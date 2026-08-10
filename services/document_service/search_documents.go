package document_service

import (
	"context"

	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/internal/orm_node"
)

type SearchMatch struct {
	NodeID int     `json:"node_id"`
	Score  float64 `json:"score"`
}

type SearchResult struct {
	DocID    string        `json:"doc_id"`
	Filename string        `json:"filename"`
	Score    float64       `json:"score"`
	Matches  []SearchMatch `json:"matches"`
}

// SearchDocuments performs full-text search across documents.
// Returns document-level results (grouped by doc_id, best score) with
// node-level matches (node_id + score only). Client controls what to
// display via GraphQL field selection.
func SearchDocuments(ctx context.Context, query string, docIDs []string, limit int) ([]SearchResult, error) {
	matched_nodes, err := orm_node.Search(ctx, query, docIDs, limit*5)
	if err != nil {
		return nil, err
	}
	if len(matched_nodes) == 0 {
		return []SearchResult{}, nil
	}

	// Group matched nodes by document, tracking insertion order for stable output.
	doc_by_id := map[string]*SearchResult{}
	var doc_first_seen_order []string
	for i := range matched_nodes {
		doc_id := matched_nodes[i].DocID.String()
		result, exists := doc_by_id[doc_id]
		if !exists {
			result = &SearchResult{DocID: doc_id}
			doc_by_id[doc_id] = result
			doc_first_seen_order = append(doc_first_seen_order, doc_id)
		}

		node_score := matched_nodes[i].Score
		if node_score > result.Score {
			result.Score = node_score
		}
		result.Matches = append(result.Matches, SearchMatch{
			NodeID: matched_nodes[i].ID,
			Score:  node_score,
		})
	}

	// Collect results in first-seen order, truncate to limit documents.
	results := make([]SearchResult, 0, len(doc_first_seen_order))
	var truncated_doc_ids []string
	for _, doc_id := range doc_first_seen_order {
		results = append(results, *doc_by_id[doc_id])
		truncated_doc_ids = append(truncated_doc_ids, doc_id)
		if len(results) >= limit {
			break
		}
	}

	// Batch-load filenames (single query, not N+1).
	filenames, err := orm_document.GetFilenamesByIDs(ctx, truncated_doc_ids)
	if err != nil {
		return nil, err
	}
	for i := range results {
		results[i].Filename = filenames[results[i].DocID]
	}

	return results, nil
}
