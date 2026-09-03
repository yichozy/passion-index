package orm_page

import (
	"context"

	"github.com/yichozy/passion-index/models"
	"gorm.io/gorm"
)

// Create batch-inserts page rows for one document. db is the handle to
// run on — dao.GetDB() standalone, or the tx inside a service-layer
// transaction. No-op on empty input.
func Create(ctx context.Context, db *gorm.DB, pages []models.Page) error {
	if len(pages) == 0 {
		return nil
	}
	return db.WithContext(ctx).Create(&pages).Error
}
