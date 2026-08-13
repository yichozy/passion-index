package orm

import (
	"context"

	"github.com/yichozy/hopebox/log"
	"gorm.io/gorm"
)

// createDocumentIndexes runs SQL that gorm struct tags can't express:
// - BM25 index on nodes via pg_search (node-level content search)
// - BM25 index on documents via pg_search (document-level meta search:
//   filename + title + description)
// - GIN index on documents.metadata for JSONB @> containment filters.
//
// The pg_search extension must be enabled first (see migrate.go).
func createDocumentIndexes(ctx context.Context, db *gorm.DB) {
	stmts := []string{
		// Node-level BM25 — searches inside parsed sections (title/summary/text).
		//
		// Syntax note: paradedb 0.25.x requires ((table.*)) as the index input
		// (single parentheses + bare table name errors with "column does not
		// exist"). Empty field configs ({}) use the default tokenizer.
		`CREATE INDEX IF NOT EXISTS idx_nodes_search ON nodes
			USING bm25 ((nodes.*)) WITH (
				key_field = 'id',
				text_fields = '{"title": {}, "summary": {}, "text": {}}'
			)`,

		// Document-level BM25 — searches doc-level text (filename + title +
		// description). Powers the doc-scope search endpoint. Mirrors
		// PageIndex's `searchDocuments` tool's matching scope, minus the
		// LLM re-rank (BM25 alone here).
		`CREATE INDEX IF NOT EXISTS idx_documents_search ON documents
			USING bm25 ((documents.*)) WITH (
				key_field = 'id',
				text_fields = '{"filename": {}, "title": {}, "description": {}}'
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
