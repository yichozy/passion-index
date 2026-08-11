package orm

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/hopebox/log"
	"github.com/yichozy/passion-index/models"
)

// DoAutoMigrate creates tables (gorm AutoMigrate) + raw SQL for objects
// gorm can't express (generated columns, GIN indexes on arrays).
func DoAutoMigrate() {
	ctx := context.Background()
	db := dao.GetDB().WithContext(ctx)

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
