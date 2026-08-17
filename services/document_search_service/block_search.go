package document_search_service

// BlockSearch is block-level retrieval for large trees — Go port of
// ConDB's BlockRetriever document mode (contextdb/retriever/algorithm/
// block_retriever.py:_retrieve_doc). Where BeamSearch feeds the LLM one
// frontier per turn (prompt changes every call → KV cache useless),
// BlockSearch processes the tree through pre-cut, content-fixed blocks:
//
//   - The prompt prefix (static instructions + previously processed block
//     contents + current block) grows monotonically call over call, so the
//     provider's server-side KV cache (DeepSeek auto context cache) hits
//     everything but the newest block and the short dynamic tail.
//   - Beam behavior survives as a filter: after each block the beams
//     become the top-ranked nodes, and the next block only allows their
//     descendants — the LLM ranks inside beam subtrees, not the whole tree.
//
// Mirrored ConDB defaults: max_parallel_blocks=4, cache_window_tokens =
// context_limit(100k) − reserve(4096) = 96000, first processed block
// pinned in the window, pick_limit = max(select_k, beam_size) = 3.
import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/gatlin"
	"github.com/yichozy/passion-index/models"
)

const (
	// BLOCK_SWITCH_NODE_COUNT is the auto-strategy threshold — trees with
	// more nodes than this use BlockSearch instead of BeamSearch (ConDB
	// _pick_strategy).
	BLOCK_SWITCH_NODE_COUNT = 50
	// BLOCK_MAX_PARALLEL bounds concurrency inside a horizontal group
	// (ConDB max_parallel_blocks default).
	BLOCK_MAX_PARALLEL = 4
	// BLOCK_CACHE_WINDOW_TOKENS bounds the accumulated prefix of processed
	// blocks (ConDB: context_limit − 4096 reserve).
	BLOCK_CACHE_WINDOW_TOKENS = 96000
)

// ErrBlockNoCandidates: no block ever offered an allowed candidate, so no
// result could be locked.
var ErrBlockNoCandidates = errors.New("block search: no candidates in any block")

// BLOCK_STATIC_SEGMENT is the never-changing prompt head — identical
// bytes in every call so the KV-cache prefix starts stable. Mirrors
// ConDB's DOC_CACHE_STATIC_SEGMENT.
const BLOCK_STATIC_SEGMENT = `You are ranking tree nodes to answer a user question.
Previous blocks are provided for context only — do NOT select nodes from them.`

// BLOCK_DYNAMIC_TAIL is the volatile per-call part: query, previous
// candidates, frontier, and the allowed whitelist for the current block.
// Rendered last so the prefix above stays byte-identical across calls.
// Mirrors ConDB's block.jinja with our JSON reply contract (same as the
// beam prompt: ranked_ids + relevance + done).
const BLOCK_DYNAMIC_TAIL = `

Query: {{ query }}

Top candidates in previous block:
{{ prev_top }}

Input frontier (context from previous block):
{{ frontier }}
The LAST BLOCK above is the CURRENT BLOCK.

Allowed node ids in CURRENT BLOCK (you MUST select only from these ids):
{{ allowed_ids }}

Pick up to {{ pick_limit }} candidates from the ALLOWED NODE IDS above, best first.
Set done=true only if top candidates already provide sufficient concrete evidence; otherwise done=false.

Reply strictly in the following JSON format:
{
    "ranked_ids": ["<uuid1>", "<uuid2>", ...],
    "relevance": <integer 0-10, how well the top-ranked candidate answers the query>,
    "done": <true|false>
}

Follow strictly the above JSON return format. Do not include any other text!`

// BlockTurn captures one block-processing decision for debugging/trace.
type BlockTurn struct {
	BlockID    string
	Kind       string // "vertical" or "horizontal"
	Candidates int    // allowed ids shown to the LLM
	Kept       int    // ranked ids kept after filtering
	Done       bool
}

// BlockSearchResult is the output of a block search.
type BlockSearchResult struct {
	Node            *models.Node // top-1 result
	Score           float64      // 0-10 relevance of the call that produced it
	BlocksProcessed int
	LLMCalls        int
	Trace           []BlockTurn
}

// windowEntry is one processed block inside the cache prefix window.
type windowEntry struct {
	content string // "=== BLOCK id ===\n\n{content}" — already wrapped
	tokens  int
	pinned  bool
}

// BlockSearch runs block-level retrieval on a single document tree.
// select_k=1: the first top-ranked node across all blocks wins (later
// blocks cannot displace it — same accumulation rule as ConDB).
func BlockSearch(ctx context.Context, root *models.Node, query string) (*BlockSearchResult, error) {
	if root == nil {
		return nil, ErrBeamNoCandidates
	}
	plan, err := CutTree(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("cut tree: %w", err)
	}

	beams := []*models.Node{root}
	var prev_top []string
	var top_node *models.Node
	var top_score float64
	var trace []BlockTurn

	var cache_window []windowEntry
	cache_window_tokens := 0
	blocks_processed := 0
	llm_calls := 0
	max_calls := len(plan.Ordered) // ConDB max_turns=None → all blocks

	// apply_ranking updates search state from one block's (or one merged
	// horizontal group's) ranked output: prev_top, beams, and the one-time
	// top-1 lock. relevance belongs to the call that contributed ranked[0].
	apply_ranking := func(ranked []uuid.UUID, relevance float64) {
		if len(ranked) == 0 {
			// LLM ranked nothing inside the whitelist (mirrors ConDB's
			// empty-frontier stall): beams drop to empty → every later
			// block's allowed set is empty → the loop winds down.
			beams = nil
			prev_top = nil
			return
		}
		prev_top = nil
		for _, id := range ranked {
			if len(prev_top) >= BEAM_SIZE {
				break
			}
			prev_top = append(prev_top, id.String())
		}
		beams = nil
		for _, id := range ranked {
			if len(beams) >= BEAM_SIZE {
				break
			}
			if n, ok := plan.NodeByID[id]; ok {
				beams = append(beams, n)
			}
		}
		if top_node == nil {
			top_node = plan.NodeByID[ranked[0]]
			top_score = relevance
		}
	}

	// append_window adds a processed block to the cache prefix. The first
	// entry is pinned; the oldest non-pinned entries are dropped once the
	// token budget is exceeded.
	append_window := func(block *Block) error {
		content := "=== BLOCK " + block.ID + " ===\n\n" + block.Content
		tokens, err := countTokens(ctx, content)
		if err != nil {
			return err
		}
		cache_window = append(cache_window, windowEntry{
			content: content,
			tokens:  tokens,
			pinned:  len(cache_window) == 0,
		})
		cache_window_tokens += tokens
		for cache_window_tokens > BLOCK_CACHE_WINDOW_TOKENS {
			drop_index := -1
			for i := range cache_window {
				if !cache_window[i].pinned {
					drop_index = i
					break
				}
			}
			if drop_index == -1 {
				break
			}
			cache_window_tokens -= cache_window[drop_index].tokens
			cache_window = append(cache_window[:drop_index], cache_window[drop_index+1:]...)
		}
		return nil
	}

	build_prefix := func() string {
		parts := make([]string, 0, len(cache_window)+1)
		parts = append(parts, BLOCK_STATIC_SEGMENT)
		for i := range cache_window {
			parts = append(parts, cache_window[i].content)
		}
		return strings.Join(parts, "\n\n")
	}

	// rank_block runs one LLM call for a block. Returns the ranked ids
	// filtered to the allowed whitelist; skipped=true when the block has
	// no allowed candidates (no LLM call, not cached — ConDB `continue`).
	rank_block := func(prefix string, block *Block) (ranked []uuid.UUID, relevance float64, done bool, skipped bool, err error) {
		allowed := collectAllowed(block, beams)
		if len(allowed) == 0 {
			return nil, 0, false, true, nil
		}
		prompt := buildBlockPrompt(prefix, block, query, prev_top, beams, allowed)
		ids, rel, dn, err := call_ranker(ctx, prompt)
		if err != nil {
			return nil, 0, false, false, fmt.Errorf("block %s: %w", block.ID, err)
		}
		allowed_set := make(map[uuid.UUID]bool, len(allowed))
		for _, id := range allowed {
			allowed_set[id] = true
		}
		for _, id := range ids {
			if allowed_set[id] {
				ranked = append(ranked, id)
			}
		}
		return ranked, rel, dn, false, nil
	}

	group_blocks := map[string][]*Block{}
	for _, g := range plan.Groups {
		group_blocks[g.ID] = g.Blocks
	}
	processed_groups := map[string]bool{}

	done_flag := false
loop:
	for _, block := range plan.Ordered {
		if llm_calls >= max_calls || done_flag {
			break
		}
		if llm_calls > 0 && !beamsHaveChildren(beams) {
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Horizontal groups: all blocks of the group processed in parallel
		// against the same prefix, then merged in block order.
		if group_id, ok := plan.GroupOfID[block.ID]; ok {
			if processed_groups[group_id] {
				continue
			}
			processed_groups[group_id] = true
			blocks := group_blocks[group_id]
			prefix := build_prefix()

			type block_outcome struct {
				ranked    []uuid.UUID
				relevance float64
				done      bool
				skipped   bool
			}
			outcomes := make([]block_outcome, len(blocks))
			g := gatlin.NewGroup(ctx, BLOCK_MAX_PARALLEL)
			for i, gb := range blocks {
				i, gb := i, gb
				g.Go(func() error {
					ranked, relevance, done, skipped, err := rank_block(prefix, gb)
					if err != nil {
						return err
					}
					outcomes[i] = block_outcome{ranked: ranked, relevance: relevance, done: done, skipped: skipped}
					return nil
				})
			}
			if err := g.Wait(); err != nil {
				return nil, err
			}

			// Merge in block order, dedup. merged_relevance belongs to the
			// outcome that contributed merged[0].
			var merged []uuid.UUID
			seen := map[uuid.UUID]bool{}
			merged_relevance := 0.0
			any_done := false
			candidates := 0
			for i := range outcomes {
				if outcomes[i].skipped {
					continue
				}
				llm_calls++
				blocks_processed++
				candidates += len(outcomes[i].ranked)
				for _, id := range outcomes[i].ranked {
					if !seen[id] {
						seen[id] = true
						if len(merged) == 0 {
							merged_relevance = outcomes[i].relevance
						}
						merged = append(merged, id)
					}
				}
				if outcomes[i].done {
					any_done = true
				}
			}
			apply_ranking(merged, merged_relevance)

			for i, gb := range blocks {
				if outcomes[i].skipped {
					continue
				}
				if err := append_window(gb); err != nil {
					return nil, err
				}
			}

			trace = append(trace, BlockTurn{
				BlockID:    group_id,
				Kind:       "horizontal",
				Candidates: candidates,
				Kept:       len(merged),
				Done:       any_done,
			})
			if any_done {
				done_flag = true
			}
			continue loop
		}

		// Vertical block: one sequential call.
		ranked, relevance, done, skipped, err := rank_block(build_prefix(), block)
		if err != nil {
			return nil, err
		}
		if skipped {
			continue
		}
		llm_calls++
		blocks_processed++
		if err := append_window(block); err != nil {
			return nil, err
		}
		apply_ranking(ranked, relevance)

		trace = append(trace, BlockTurn{
			BlockID:    block.ID,
			Kind:       "vertical",
			Candidates: len(ranked),
			Kept:       len(ranked),
			Done:       done,
		})
		if done {
			break
		}
	}

	if top_node == nil {
		return nil, ErrBlockNoCandidates
	}
	return &BlockSearchResult{
		Node:            top_node,
		Score:           top_score,
		BlocksProcessed: blocks_processed,
		LLMCalls:        llm_calls,
		Trace:           trace,
	}, nil
}

// collectAllowed returns the ids of nodes in block that are strict
// descendants of any beam (and not beams themselves) — the LLM may only
// rank these. ConDB implements this as a materialized-path prefix check;
// with in-memory nodes we mark descendants from each beam instead.
func collectAllowed(block *Block, beams []*models.Node) []uuid.UUID {
	if len(beams) == 0 {
		return nil
	}
	beam_ids := make(map[uuid.UUID]bool, len(beams))
	descendant := map[uuid.UUID]bool{}
	var mark func(n *models.Node)
	mark = func(n *models.Node) {
		for i := range n.Nodes {
			child := &n.Nodes[i]
			descendant[child.ID] = true
			mark(child)
		}
	}
	for _, b := range beams {
		beam_ids[b.ID] = true
		mark(b)
	}

	var allowed []uuid.UUID
	for _, n := range block.Nodes {
		if !beam_ids[n.ID] && descendant[n.ID] {
			allowed = append(allowed, n.ID)
		}
	}
	return allowed
}

// beamsHaveChildren reports whether any beam still has children to expand.
func beamsHaveChildren(beams []*models.Node) bool {
	for _, b := range beams {
		if len(b.Nodes) > 0 {
			return true
		}
	}
	return false
}

// buildBlockPrompt assembles the full prompt: prefix (static segment +
// processed blocks) + current block + dynamic tail. The prefix and
// current block are byte-stable across calls for a given tree walk —
// only the tail varies — which is what makes the provider KV cache hit.
func buildBlockPrompt(prefix string, block *Block, query string, prev_top []string, beams []*models.Node, allowed []uuid.UUID) string {
	var head strings.Builder
	head.WriteString(prefix)
	head.WriteString("\n\n=== BLOCK ")
	head.WriteString(block.ID)
	head.WriteString(" ===\n\n")
	head.WriteString(block.Content)

	prev := "(none)"
	if len(prev_top) > 0 {
		prev = strings.Join(prev_top, ", ")
	}
	var frontier strings.Builder
	if len(beams) == 0 {
		frontier.WriteString("(none)\n")
	}
	for _, b := range beams {
		fmt.Fprintf(&frontier, "- %s: %s\n", b.ID, b.Title)
	}
	var ids strings.Builder
	for _, id := range allowed {
		fmt.Fprintf(&ids, "- %s\n", id)
	}

	tail := strings.NewReplacer(
		"{{ query }}", query,
		"{{ prev_top }}", prev,
		"{{ frontier }}", frontier.String(),
		"{{ allowed_ids }}", ids.String(),
		"{{ pick_limit }}", strconv.Itoa(BEAM_SIZE),
	).Replace(BLOCK_DYNAMIC_TAIL)
	return head.String() + tail
}
