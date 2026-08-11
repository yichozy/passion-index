package orm_document

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// Update persists all fields of an existing document by primary key
// (gorm Save: full update). Caller should have loaded the doc first via
// GetDocument, mutated the desired fields, then call Update to save.
func Update(ctx context.Context, doc *models.Document) error {
	return dao.GetDB().WithContext(ctx).Save(doc).Error
}
