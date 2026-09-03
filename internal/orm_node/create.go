package orm_node

import (
	"context"

	"github.com/yichozy/passion-index/models"
	"gorm.io/gorm"
)

// Create batch-inserts node rows for a document. db is the handle to
// run on — dao.GetDB() standalone, or the tx inside a service-layer
// transaction. No-op on empty input.
func Create(ctx context.Context, db *gorm.DB, rows []models.Node) error {
	if len(rows) == 0 {
		return nil
	}
	return db.WithContext(ctx).Create(&rows).Error
}
