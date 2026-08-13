package orm

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/hopebox/log"
	"github.com/yichozy/passion-index/models"
)

// DoAutoMigrate creates tables (gorm AutoMigrate) + raw SQL for objects
// gorm can't express (BM25 index via pg_search, GIN indexes on jsonb).
func DoAutoMigrate() {
	ctx := context.Background()
	db := dao.GetDB().WithContext(ctx)

	// pg_search must exist before we can CREATE INDEX ... USING bm25.
	// ParadeDB image preinstalls it; just enable per-database.
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS pg_search").Error; err != nil {
		log.Errorf(ctx, "create pg_search extension failed: %v", err)
		return
	}

	if err := db.AutoMigrate(
		&models.Document{},
		&models.Node{},
		&models.Folder{},
	); err != nil {
		log.Errorf(ctx, "AutoMigrate failed: %v", err)
		return
	}

	createDocumentIndexes(ctx, db)

	log.Info(ctx, "AutoMigrate Done")
}
