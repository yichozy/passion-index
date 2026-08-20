package document_search_service

// Semantic document search — PageIndex semantics tutorial, steps 1-3:
// embed query → vector-recall top chunks → aggregate per-doc DocScore.
// Step 4 (in-tree node retrieval) is the caller's job via
// SearchDocumentNodes.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/fastembed"
	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"
)

// SEMANTIC_RECALL_K is how many top chunks feed the DocScore aggregation
// (the tutorial's top-K vector search step).
const SEMANTIC_RECALL_K = 100

// SearchDocumentsSemantic ranks documents by semantic relevance to the
// query: vector-recall the top-K node chunks (folder-scoped, metadata
// containment), then per doc
//
//	DocScore = Σ ChunkScore / √(N+1)
//
// where N is the number of the doc's recalled chunks — more relevant
// chunks raise the score with diminishing returns; fewer strong matches
// beat many weak ones. Score is cosine-based, roughly 0-1. Any failure
// fails loudly.
//
// folder_id nil = whole library (virtual root).
func SearchDocumentsSemantic(ctx context.Context, query string, folder_id *uuid.UUID, recursive bool, metadata map[string]any, limit int) ([]orm_document.DocumentWithScore, error) {
	if limit <= 0 {
		limit = 10
	}
	fastembed_url := os.Getenv("FASTEMBED_URL")
	if fastembed_url == "" {
		return nil, fmt.Errorf("semantic search: FASTEMBED_URL not configured")
	}

	vectors, err := fastembed.NewClient(fastembed_url).TextEmbedding(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("embed query: got %d vectors for 1 query", len(vectors))
	}

	// []float64 marshals to exactly pgvector's text literal format.
	query_vec, err := json.Marshal(vectors[0])
	if err != nil {
		return nil, fmt.Errorf("encode query vector: %w", err)
	}
	recalled, err := orm_node.SearchNodesByVector(ctx, string(query_vec), folder_id, recursive, metadata, SEMANTIC_RECALL_K)
	if err != nil {
		return nil, err
	}

	score_sum_by_doc := map[uuid.UUID]float64{}
	count_by_doc := map[uuid.UUID]int{}
	var doc_order []uuid.UUID // first-seen (best-chunk) order
	for i := range recalled {
		doc_id := recalled[i].DocID
		if _, seen := score_sum_by_doc[doc_id]; !seen {
			doc_order = append(doc_order, doc_id)
		}
		score_sum_by_doc[doc_id] += recalled[i].Score
		count_by_doc[doc_id]++
	}

	type doc_score struct {
		id    uuid.UUID
		score float64
	}
	scored := make([]doc_score, 0, len(doc_order))
	for _, doc_id := range doc_order {
		scored = append(scored, doc_score{
			id:    doc_id,
			score: score_sum_by_doc[doc_id] / math.Sqrt(float64(count_by_doc[doc_id]+1)),
		})
	}
	sort.Slice(scored, func(a, b int) bool { return scored[a].score > scored[b].score })
	if len(scored) > limit {
		scored = scored[:limit]
	}

	ids := make([]uuid.UUID, len(scored))
	for i := range scored {
		ids[i] = scored[i].id
	}
	docs, err := orm_document.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	doc_by_id := map[uuid.UUID]models.Document{}
	for i := range docs {
		doc_by_id[docs[i].ID] = docs[i]
	}
	out := make([]orm_document.DocumentWithScore, 0, len(scored))
	for _, s := range scored {
		doc, ok := doc_by_id[s.id]
		if !ok {
			continue // doc deleted between recall and fetch; skip
		}
		out = append(out, orm_document.DocumentWithScore{
			ID:          doc.ID,
			Filename:    doc.Filename,
			Title:       doc.Title,
			Description: doc.Description,
			Metadata:    doc.Metadata,
			Score:       s.score,
		})
	}
	return out, nil
}
