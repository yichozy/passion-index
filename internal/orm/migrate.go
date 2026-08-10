package orm

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/hopebox/log"
	"github.com/yichozy/passion-index/models"
)

// DoAutoMigrate creates the documents table + indexes if they don't exist.
// Safe to call on every startup (gorm AutoMigrate is idempotent). All
// indexes are declared on the model via gorm struct tags — no raw SQL.
func DoAutoMigrate() {
	ctx := context.Background()
	db := dao.GetDB().WithContext(ctx)

	if err := db.AutoMigrate(
		&models.Document{},
		&models.Node{},
	); err != nil {
		log.Errorf(ctx, "AutoMigrate failed: %v", err)
		return
	}

	// Full-text search: generated tsvector column + GIN index.
	// gorm can't express GENERATED columns via struct tags, so raw SQL.
	// Title weight A (highest), summary B, text C (lowest).
	stmts := []string{
		`DO $$ BEGIN
			ALTER TABLE nodes ADD COLUMN IF NOT EXISTS search_vector tsvector
				GENERATED ALWAYS AS (
					setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
					setweight(to_tsvector('english', coalesce(summary, '')), 'B') ||
					setweight(to_tsvector('english', coalesce(text, '')), 'C')
				) STORED;
		EXCEPTION WHEN OTHERS THEN NULL; END $$`,
		`CREATE INDEX IF NOT EXISTS idx_nodes_search ON nodes USING GIN (search_vector)`,
	}
	for _, sql := range stmts {
		if err := db.Exec(sql).Error; err != nil {
			log.Warnf(ctx, "migration stmt failed: %v", err)
		}
	}

	log.Info(ctx, "AutoMigrate Done")
}
