// Package document_search_service implements LLM-guided retrieval over
// document trees — a Go port of ConDB's beam_retriever / block_retriever
// (document mode), plus the single-doc semantic node search entry
// (SearchDocumentNodes). This file: beam search — the LLM ranks sibling
// nodes level by level; traversal keeps the top BEAM_SIZE branches and
// descends until a leaf is locked. The LLM's done signal only ends the
// walk early once a leaf is locked or nothing is left to expand.
package document_search_service

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/yichozy/passion-index/models"
)

// Beam search constants — mirror ConDB defaults
// (contextdb/config/retriever/beam.yaml: beam_size=3, select_k=1).
const (
	BEAM_SIZE            = 3
	BEAM_SELECT_K        = 1
	BEAM_TEXT_TRUNCATE   = 200 // chars of node.Text included in prompt
	BEAM_PARENT_TRUNCATE = 300 // chars of parent summary in prompt
)

// Sentinel errors — callers distinguish failure modes via errors.Is.
var (
	ErrBeamNoConverge   = errors.New("beam search did not converge")
	ErrBeamNoCandidates = errors.New("beam search: no candidates at frontier")
)

// BeamCandidate is one node the LLM is asked to rank. It doubles as the
// frontier entry: candidates promote to the frontier as-is, since
// SectionPath and ParentSummary already describe the node's own position
// and parent. SectionPath is the "Chapter > Section > Subsection" title
// chain giving the LLM positional context.
type BeamCandidate struct {
	Node          *models.Node
	IsLeaf        bool
	SectionPath   string
	ParentSummary string
}

// BeamTurn captures one LLM ranking decision for debugging/trace.
type BeamTurn struct {
	Depth        int
	RankedTitles []string // LLM's ranking resolved to titles, best first
	Done         bool
}

// BeamResult is the output of a beam search.
type BeamResult struct {
	Node  *models.Node // top-1 result (select_k=1)
	Score float64      // 0-10 LLM relevance of the final result (0 when no LLM was consulted)
	Trace []BeamTurn
}

// BeamSearch runs beam=3, select_k=1 retrieval on a single document tree,
// drilling to a leaf (the specific section answering the query).
//
// Returns ErrBeamNoConverge if the loop exhausts without locking a result
// (e.g., LLM hallucinated IDs at every level). Returns ErrBeamNoCandidates
// if root is nil.
func BeamSearch(ctx context.Context, root *models.Node, query string) (*BeamResult, error) {
	if root == nil {
		return nil, ErrBeamNoCandidates
	}

	max_turns := root.MaxDepth()
	if max_turns == 0 {
		// Tree has no children — root is the only node. Return it directly.
		return &BeamResult{Node: root}, nil
	}

	frontier := []BeamCandidate{{Node: root}}
	var top_candidate *models.Node
	var top_score float64
	var trace []BeamTurn

	for turn := 0; turn < max_turns; turn++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// 1. Expand frontier → candidate list.
		candidates, all_leaves := collectCandidates(frontier)
		if len(candidates) == 0 {
			break
		}

		// 2. Frontier is all leaves → pick the first as result. This happens
		// only when root itself is a leaf (single-node tree), since leaves
		// never enter the next frontier in the main path.
		if all_leaves {
			top_candidate = frontier[0].Node
			break
		}

		// 3. Ask the LLM to rank the candidates.
		ranked_ids, relevance, done, err := RankCandidates(ctx, query, candidates, BEAM_SIZE)
		if err != nil {
			return nil, err
		}

		// 4. Consider top-(select_k) ranked for the result lock. Only leaves
		//    in that prefix lock the result; non-leaves go to the next
		//    frontier. DELIBERATE divergence from ConDB: its document mode
		//    appends ranked[:result_limit] to the final results regardless
		//    of leaf status, so with select_k=1 it force-stops after one
		//    turn (a single LLM call over the root's children). We descend
		//    until a leaf is locked or the LLM signals done — see the PoC
		//    spec. Walking past select_k to fill the next frontier is
		//    correct: beam_size can exceed select_k (ConDB defaults:
		//    beam=3, select_k=1).
		cand_by_id := make(map[uuid.UUID]*BeamCandidate, len(candidates))
		for i := range candidates {
			cand_by_id[candidates[i].Node.ID] = &candidates[i]
		}

		for i := 0; i < BEAM_SELECT_K && i < len(ranked_ids); i++ {
			c, ok := cand_by_id[ranked_ids[i]]
			if !ok {
				continue
			}
			if c.IsLeaf {
				top_candidate = c.Node
				top_score = relevance // set here: the done-branch below breaks before the later assignment
				break
			}
		}

		// 5. Build next frontier from non-leaves in ranked, up to BEAM_SIZE.
		var next_frontier []BeamCandidate
		for _, id := range ranked_ids {
			if len(next_frontier) >= BEAM_SIZE {
				break
			}
			c, ok := cand_by_id[id]
			if !ok {
				continue // LLM hallucinated an ID; skip
			}
			if c.IsLeaf {
				continue // leaves don't expand further
			}
			next_frontier = append(next_frontier, *c)
		}

		// 6. Trace: resolve ranked IDs to titles for debugging.
		var ranked_titles []string
		for _, id := range ranked_ids {
			if c, ok := cand_by_id[id]; ok {
				ranked_titles = append(ranked_titles, c.Node.Title)
			}
		}
		trace = append(trace, BeamTurn{
			Depth:        turn,
			RankedTitles: ranked_titles,
			Done:         done,
		})

		// 7. LLM said stop — honor it once a leaf is locked or the
		//    frontier is exhausted; otherwise keep drilling toward a
		//    leaf. If we stop with nothing locked, fall back to
		//    top-ranked (could be non-leaf) — trusts the LLM's "this
		//    section is specific enough" signal.
		if done && (top_candidate != nil || len(next_frontier) == 0) {
			if top_candidate == nil {
				for _, id := range ranked_ids {
					if c, ok := cand_by_id[id]; ok {
						top_candidate = c.Node
						top_score = relevance
						break
					}
				}
			}
			break
		}
		if top_candidate != nil {
			break // top_score was set at lock time in step 4
		}
		if len(next_frontier) == 0 {
			break // ranked had only leaves but none in top-select_k? defensive
		}
		frontier = next_frontier
	}

	if top_candidate == nil {
		return nil, ErrBeamNoConverge
	}
	return &BeamResult{Node: top_candidate, Score: top_score, Trace: trace}, nil
}

// collectCandidates flattens the current frontier into the LLM-visible
// candidate list: each frontier entry contributes either itself (if leaf)
// or its children. all_leaves=true means every frontier entry is a leaf —
// the caller should short-circuit.
func collectCandidates(frontier []BeamCandidate) (candidates []BeamCandidate, all_leaves bool) {
	all_leaves = true
	for _, f := range frontier {
		if len(f.Node.Nodes) == 0 {
			leaf := f // copy; IsLeaf gets set below
			leaf.IsLeaf = true
			candidates = append(candidates, leaf)
			continue
		}
		all_leaves = false
		for i := range f.Node.Nodes {
			child := &f.Node.Nodes[i]
			// Section path: the parent's accumulated title chain extended
			// with the child's title, "Chapter > Section > Subsection"
			// style. Empty titles are skipped (e.g. synthetic root).
			var path_parts []string
			if f.SectionPath != "" {
				path_parts = append(path_parts, f.SectionPath)
			} else if f.Node.Title != "" {
				path_parts = append(path_parts, f.Node.Title)
			}
			if child.Title != "" {
				path_parts = append(path_parts, child.Title)
			}
			candidates = append(candidates, BeamCandidate{
				Node:          child,
				IsLeaf:        len(child.Nodes) == 0,
				SectionPath:   strings.Join(path_parts, " > "),
				ParentSummary: f.Node.Summary,
			})
		}
	}
	return candidates, all_leaves
}
