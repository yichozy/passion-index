package orm_document

import (
	"context"

	"github.com/yichozy/passion-index/models"
	"gorm.io/gorm"
)

// Update persists all fields of an existing document by primary key
// (gorm Save: full update). Caller should have loaded the doc first via
// GetDocument, mutated the desired fields, then call Update to save. db
// is the handle to run on — dao.GetDB() standalone, or the tx inside a
// service-layer transaction.
func Update(ctx context.Context, db *gorm.DB, doc *models.Document) error {
	return db.WithContext(ctx).Save(doc).Error
}
