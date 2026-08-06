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

	if err := db.AutoMigrate(&models.Document{}); err != nil {
		log.Errorf(ctx, "AutoMigrate failed: %v", err)
		return
	}

	log.Info(ctx, "AutoMigrate Done")
}
