package orm

import (
	"context"

	"github.com/yichozy/hopebox/log"
	"gorm.io/gorm"
)

// createDocumentIndexes runs SQL that gorm struct tags can't express:
// - GENERATED tsvector column on nodes (full-text search)
// - GIN indexes on tsvector + text[] array columns
func createDocumentIndexes(ctx context.Context, db *gorm.DB) {
	stmts := []string{
		// Full-text search: generated tsvector column.
		// Title weight A (highest), summary B, text C (lowest).
		`DO $$ BEGIN
			ALTER TABLE nodes ADD COLUMN IF NOT EXISTS search_vector tsvector
				GENERATED ALWAYS AS (
					setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
					setweight(to_tsvector('english', coalesce(summary, '')), 'B') ||
					setweight(to_tsvector('english', coalesce(text, '')), 'C')
				) STORED;
		EXCEPTION WHEN OTHERS THEN NULL; END $$`,

		// tsvector — for @@ full-text search operator
		`CREATE INDEX IF NOT EXISTS idx_nodes_search ON nodes USING GIN (search_vector)`,

		// text[] arrays — for && overlap operator
		`CREATE INDEX IF NOT EXISTS idx_documents_indication ON documents USING GIN (indication)`,
		`CREATE INDEX IF NOT EXISTS idx_documents_study ON documents USING GIN (study)`,
		`CREATE INDEX IF NOT EXISTS idx_documents_literature_type ON documents USING GIN (literature_type)`,
	}

	for _, sql := range stmts {
		if err := db.Exec(sql).Error; err != nil {
			log.Warnf(ctx, "migration stmt failed: %v", err)
		}
	}
}
