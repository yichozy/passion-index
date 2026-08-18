package document_service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/fastembed"
	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"
)

// EMBED_TEXT_TRUNCATE caps the node text fed to the embedding model.
// Chunk = title + "\n" + text[:1000] — summary is deliberately NOT
// vectorized: it restates the children's content and would double-count
// in DocScore; pure grouping sections (empty text) stay un-embedded and
// are covered by their children.
const EMBED_TEXT_TRUNCATE = 1000

// EmbedDocumentTree vectorizes a document's nodes (the pipeline's
// EMBEDDING step). One batched fastembed call, one transactional write.
// Fails loudly on missing config, service error, or dimension mismatch —
// the pipeline records FAILED.
//
// Note: embeddings never go stale — text/title are immutable after
// structuring (ReSummarizeDocument only regenerates summaries), so
// re-summarizing does not need to re-embed.
func EmbedDocumentTree(ctx context.Context, doc_id uuid.UUID) error {
	fastembed_url := os.Getenv("FASTEMBED_URL")
	if fastembed_url == "" {
		return fmt.Errorf("embedding: FASTEMBED_URL not configured")
	}

	rows, err := orm_node.GetByDocID(ctx, doc_id)
	if err != nil {
		return fmt.Errorf("load nodes: %w", err)
	}

	var node_ids []uuid.UUID
	var chunks []string
	for i := range rows {
		node := &rows[i]
		if node.Text == "" {
			continue
		}
		text := node.Text
		if len(text) > EMBED_TEXT_TRUNCATE {
			text = text[:EMBED_TEXT_TRUNCATE]
		}
		node_ids = append(node_ids, node.ID)
		chunks = append(chunks, node.Title+"\n"+text)
	}
	if len(chunks) == 0 {
		return nil
	}

	vectors, err := fastembed.NewClient(fastembed_url).TextEmbedding(ctx, chunks)
	if err != nil {
		return fmt.Errorf("fastembed: %w", err)
	}
	if len(vectors) != len(chunks) {
		return fmt.Errorf("fastembed returned %d vectors for %d chunks", len(vectors), len(chunks))
	}

	// Dimension guard: the serving model's output must match the column
	// dim (models.EmbeddingDim) or every write fails with a pgvector
	// dimension error. Checked unconditionally so a changed model fails
	// loudly here, not deep in SQL.
	if len(vectors[0]) != models.EmbeddingDim {
		return fmt.Errorf("embedding dim mismatch: model=%d, models.EmbeddingDim=%d", len(vectors[0]), models.EmbeddingDim)
	}

	// []float64 marshals to exactly pgvector's text literal format
	// ("[0.1,0.2,...]"); the only marshal error for floats is NaN/Inf,
	// which we want to fail loudly on anyway.
	vecs := make(map[uuid.UUID]string, len(vectors))
	for i := range vectors {
		b, err := json.Marshal(vectors[i])
		if err != nil {
			return fmt.Errorf("encode embedding: %w", err)
		}
		vecs[node_ids[i]] = string(b)
	}
	if err := orm_node.UpdateEmbedding(ctx, doc_id, vecs); err != nil {
		return fmt.Errorf("persist embeddings: %w", err)
	}
	return nil
}
