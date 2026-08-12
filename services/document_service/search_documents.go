package document_service

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/passion-index/internal/orm_node"
)

type SearchMatch struct {
	NodeID int     `json:"node_id"`
	Score  float64 `json:"score"`
}

type SearchResult struct {
	DocID    uuid.UUID     `json:"doc_id"`
	Filename string        `json:"filename"`
	Score    float64       `json:"score"`
	Matches  []SearchMatch `json:"matches"`
}

// SearchDocuments performs full-text search across documents.
// metadata filter is optional — when non-empty, scoped via JSONB @> containment.
func SearchDocuments(ctx context.Context, query string, doc_ids []uuid.UUID, metadata map[string]any, limit int) ([]SearchResult, error) {
	matched_nodes, err := orm_node.Search(ctx, query, doc_ids, metadata, limit*5)
	if err != nil {
		return nil, err
	}
	if len(matched_nodes) == 0 {
		return []SearchResult{}, nil
	}

	// Group by doc_id, track best score and first-seen order.
	doc_by_id := map[uuid.UUID]*SearchResult{}
	var doc_first_seen_order []uuid.UUID
	for i := range matched_nodes {
		doc_id := matched_nodes[i].DocID
		result, exists := doc_by_id[doc_id]
		if !exists {
			result = &SearchResult{DocID: doc_id, Filename: matched_nodes[i].Filename}
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

	// Collect in first-seen order, truncate to limit.
	results := make([]SearchResult, 0, len(doc_first_seen_order))
	for _, doc_id := range doc_first_seen_order {
		results = append(results, *doc_by_id[doc_id])
		if len(results) >= limit {
			break
		}
	}

	return results, nil
}
