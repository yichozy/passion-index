package orm

import (
	"context"

	"github.com/yichozy/hopebox/log"
	"gorm.io/gorm"
)

// createDocumentIndexes runs SQL that gorm struct tags can't express:
// - BM25 index on nodes via pg_search (full-text search with proper BM25
//   ranking: saturation, length normalization, IDF — none of which ts_rank
//   provides).
// - GIN index on documents.metadata for JSONB @> containment filters.
//
// The pg_search extension must be enabled first (see migrate.go).
func createDocumentIndexes(ctx context.Context, db *gorm.DB) {
	stmts := []string{
		// BM25 index over the searchable text fields. key_field='id' (UUID PK)
		// gives paradedb.score(id) a stable row identifier.
		// Empty field configs ({}) use the default tokenizer (English-friendly,
		// whitespace split + lowercase).
		`CREATE INDEX IF NOT EXISTS idx_nodes_search ON nodes
			USING bm25 (nodes) WITH (
				key_field = 'id',
				text_fields = '{"title": {}, "summary": {}, "text": {}}'
			)`,

		// jsonb metadata — for @> containment operator on documents
		`CREATE INDEX IF NOT EXISTS idx_documents_metadata ON documents USING GIN (metadata)`,
	}

	for _, sql := range stmts {
		if err := db.Exec(sql).Error; err != nil {
			log.Warnf(ctx, "migration stmt failed: %v", err)
		}
	}
}
