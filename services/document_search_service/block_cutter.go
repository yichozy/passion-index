package document_search_service

// Block cutting for block-level retrieval — Go port of ConDB's BlockCutter
// (contextdb/retriever/algorithm/block_cutter.py). Pre-plans a document
// tree into blocks whose content depends only on the tree, never on the
// query, so the block sequence doubles as a stable prompt prefix across
// LLM calls (DeepSeek server-side KV cache hits it).
//
// Cutting strategy (ConDB defaults):
//   - Vertical blocks: greedy-merge contiguous depth levels while the
//     accumulated token estimate stays within BLOCK_MAX_TOKENS.
//   - Horizontal groups: when a single level alone exceeds the budget,
//     its nodes are packed (grouped by parent, greedy) into multiple
//     parallel blocks.
import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/llm"
	"github.com/yichozy/passion-index/models"
)

const (
	// BLOCK_MAX_TOKENS is the token budget per block (ConDB default
	// max_tokens_per_block=16000).
	BLOCK_MAX_TOKENS = 16000
	// BLOCK_NODE_TEXT_TRUNCATE is the text prefix counted in a node's
	// token estimate and the floor for its text share in block content
	// (mirrors ConDB's text[:200]).
	BLOCK_NODE_TEXT_TRUNCATE = 200
	// BLOCK_CHARS_PER_TOKEN converts metadata chars to a token estimate
	// when rendering block content (ConDB uses 4).
	BLOCK_CHARS_PER_TOKEN = 4
	// BLOCK_NODE_TOKEN_OVERHEAD pads a node's token estimate for the
	// metadata formatting lines around it.
	BLOCK_NODE_TOKEN_OVERHEAD = 30
)

// Block is one fixed-content slice of the tree. Content is
// query-independent and stable for a given tree.
type Block struct {
	ID          string // "v_0_2" (vertical, depth range) / "h_3_3_b1" (horizontal pack)
	DepthStart  int
	DepthEnd    int
	Nodes       []*models.Node
	TotalTokens int    // sum of node token estimates
	Content     string // rendered cacheable content (metadata + adaptive text)
}

// BlockGroup is a set of parallel blocks covering one over-wide depth
// level (single level exceeds BLOCK_MAX_TOKENS).
type BlockGroup struct {
	ID     string
	Blocks []*Block
}

// BlockPlan is the complete cutting plan plus lookup indexes.
type BlockPlan struct {
	Ordered   []*Block // depth order; horizontal-group blocks contiguous
	Groups    []*BlockGroup
	GroupOfID map[string]string // block ID -> group ID (horizontal blocks only)
	NodeByID  map[uuid.UUID]*models.Node
}

// CutTree builds the block plan for an in-memory tree. Token estimates
// use tiktoken (o200k) over title + summary + text[:200] plus a fixed
// overhead per node.
func CutTree(ctx context.Context, root *models.Node) (*BlockPlan, error) {
	// 1. Index nodes by depth (root = depth 0; synthetic root included).
	nodes_by_depth := map[int][]*models.Node{}
	depth_by_id := map[uuid.UUID]int{}
	tokens_by_id := map[uuid.UUID]int{}
	plan := &BlockPlan{
		GroupOfID: map[string]string{},
		NodeByID:  map[uuid.UUID]*models.Node{},
	}
	max_depth := 0

	var walk func(n *models.Node, depth int) error
	walk = func(n *models.Node, depth int) error {
		nodes_by_depth[depth] = append(nodes_by_depth[depth], n)
		depth_by_id[n.ID] = depth
		plan.NodeByID[n.ID] = n
		tokens, err := countNodeTokens(ctx, n)
		if err != nil {
			return err
		}
		tokens_by_id[n.ID] = tokens
		if depth > max_depth {
			max_depth = depth
		}
		for i := range n.Nodes {
			if err := walk(&n.Nodes[i], depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root, 0); err != nil {
		return nil, err
	}

	// 2. Greedy vertical merging; horizontal packing for over-wide levels.
	depth_tokens := func(d int) int {
		total := 0
		for _, n := range nodes_by_depth[d] {
			total += tokens_by_id[n.ID]
		}
		return total
	}
	nodes_in_range := func(start, end int) []*models.Node {
		var nodes []*models.Node
		for d := start; d <= end; d++ {
			nodes = append(nodes, nodes_by_depth[d]...)
		}
		return nodes
	}

	for depth := 0; depth <= max_depth; {
		// Greedy: merge as many deeper levels as fit in one vertical block.
		depth_end := depth
		accumulated := depth_tokens(depth)
		for depth_end+1 <= max_depth {
			next := depth_tokens(depth_end + 1)
			if accumulated+next > BLOCK_MAX_TOKENS {
				break
			}
			depth_end++
			accumulated += next
		}

		range_nodes := nodes_in_range(depth, depth_end)
		if accumulated <= BLOCK_MAX_TOKENS {
			block := makeBlock(fmt.Sprintf("v_%d_%d", depth, depth_end), depth, depth_end, range_nodes, tokens_by_id, depth_by_id)
			plan.Ordered = append(plan.Ordered, block)
		} else {
			// Single level exceeds the budget → horizontal packing.
			group := &BlockGroup{ID: fmt.Sprintf("h_%d_%d", depth, depth_end)}
			for pack_index, pack := range packNodes(range_nodes, tokens_by_id) {
				block := makeBlock(fmt.Sprintf("%s_b%d", group.ID, pack_index), depth, depth_end, pack, tokens_by_id, depth_by_id)
				group.Blocks = append(group.Blocks, block)
				plan.Ordered = append(plan.Ordered, block)
				plan.GroupOfID[block.ID] = group.ID
			}
			plan.Groups = append(plan.Groups, group)
		}
		depth = depth_end + 1
	}
	return plan, nil
}

// packNodes greedily packs nodes into groups within BLOCK_MAX_TOKENS,
// keeping siblings adjacent (nodes grouped by parent first, mirroring
// ConDB's parent_groups ordering). A single node larger than the budget
// still gets its own pack.
func packNodes(nodes []*models.Node, tokens_by_id map[uuid.UUID]int) [][]*models.Node {
	groups := map[uuid.UUID][]*models.Node{}
	var parent_order []uuid.UUID
	for _, n := range nodes {
		var key uuid.UUID
		if n.ParentID != nil {
			key = *n.ParentID
		}
		if _, seen := groups[key]; !seen {
			parent_order = append(parent_order, key)
		}
		groups[key] = append(groups[key], n)
	}
	var ordered []*models.Node
	for _, key := range parent_order {
		ordered = append(ordered, groups[key]...)
	}

	var packs [][]*models.Node
	var current []*models.Node
	current_tokens := 0
	for _, n := range ordered {
		tokens := tokens_by_id[n.ID]
		if len(current) > 0 && current_tokens+tokens > BLOCK_MAX_TOKENS {
			packs = append(packs, current)
			current, current_tokens = nil, 0
			continue // re-try this node against a fresh pack
		}
		current = append(current, n)
		current_tokens += tokens
	}
	if len(current) > 0 {
		packs = append(packs, current)
	}
	return packs
}

// makeBlock assembles a Block with its total token estimate and rendered
// cacheable content.
func makeBlock(id string, depth_start, depth_end int, nodes []*models.Node, tokens_by_id, depth_by_id map[uuid.UUID]int) *Block {
	total := 0
	for _, n := range nodes {
		total += tokens_by_id[n.ID]
	}
	return &Block{
		ID:          id,
		DepthStart:  depth_start,
		DepthEnd:    depth_end,
		Nodes:       nodes,
		TotalTokens: total,
		Content:     renderBlockContent(nodes, depth_by_id),
	}
}

// renderBlockContent renders a block's cacheable content: per-node
// metadata lines (id/title/parent/summary/depth/range) followed by text
// with an adaptive per-node char budget — the remaining block budget
// split across nodes that have text, floor BLOCK_NODE_TEXT_TRUNCATE.
// Port of ConDB's BlockCutter._generate_block_content.
func renderBlockContent(nodes []*models.Node, depth_by_id map[uuid.UUID]int) string {
	title_by_id := map[uuid.UUID]string{}
	for _, n := range nodes {
		if n.Title != "" {
			title_by_id[n.ID] = n.Title
		}
	}

	type node_meta struct {
		lines []string
		text  string
	}
	metas := make([]node_meta, 0, len(nodes))
	metadata_chars := 0
	for _, n := range nodes {
		var lines []string
		lines = append(lines, fmt.Sprintf("- id: %s", n.ID))
		if n.Title != "" {
			lines = append(lines, fmt.Sprintf("  title: %s", n.Title))
		}
		// Parent title only when the parent is in this block (ConDB builds
		// its title map from block nodes).
		if n.ParentID != nil {
			if parent_title, ok := title_by_id[*n.ParentID]; ok {
				lines = append(lines, fmt.Sprintf("  parent: %s", parent_title))
			}
		}
		if n.Summary != "" {
			lines = append(lines, fmt.Sprintf("  summary: %s", n.Summary))
		}
		lines = append(lines, fmt.Sprintf("  depth: %d", depth_by_id[n.ID]))
		if n.PageStart != 0 || n.PageEnd != 0 {
			lines = append(lines, fmt.Sprintf("  range: %d-%d", n.PageStart, n.PageEnd))
		}
		for _, line := range lines {
			metadata_chars += len(line)
		}
		metas = append(metas, node_meta{lines: lines, text: n.Text})
	}

	metadata_tokens_est := metadata_chars / BLOCK_CHARS_PER_TOKEN
	remaining_tokens := BLOCK_MAX_TOKENS - metadata_tokens_est
	if remaining_tokens < 0 {
		remaining_tokens = 0
	}
	nodes_with_text := 0
	for i := range metas {
		if metas[i].text != "" {
			nodes_with_text++
		}
	}
	chars_per_node := BLOCK_NODE_TEXT_TRUNCATE
	if nodes_with_text > 0 {
		if c := (remaining_tokens * BLOCK_CHARS_PER_TOKEN) / nodes_with_text; c > chars_per_node {
			chars_per_node = c
		}
	}

	var out []string
	for _, meta := range metas {
		out = append(out, meta.lines...)
		if meta.text != "" {
			text := meta.text
			if len(text) > chars_per_node {
				text = text[:chars_per_node]
			}
			out = append(out, fmt.Sprintf("  text: %s", text))
		}
	}
	return strings.Join(out, "\n")
}

// countNodeTokens estimates a node's prompt footprint the way ConDB's
// TokenCounter does: title + summary + first 200 chars of text, plus a
// fixed overhead for metadata formatting.
func countNodeTokens(ctx context.Context, n *models.Node) (int, error) {
	text := n.Text
	if len(text) > BLOCK_NODE_TEXT_TRUNCATE {
		text = text[:BLOCK_NODE_TEXT_TRUNCATE]
	}
	tokens, err := countTokens(ctx, n.Title+"\n"+n.Summary+"\n"+text)
	if err != nil {
		return 0, err
	}
	return tokens + BLOCK_NODE_TOKEN_OVERHEAD, nil
}

// countTokens counts tokens via hopebox's tiktoken wrapper (o200k_base).
func countTokens(ctx context.Context, s string) (int, error) {
	ids, err := llm.EncodeToken(ctx, s)
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}
