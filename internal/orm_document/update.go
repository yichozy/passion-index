package orm_document

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// Update persists all fields of an existing document by primary key
// (gorm Save: full update). Caller should have loaded the doc first via
// GetDocument, mutated the desired fields, then call Update to save.
// Fire-and-forget — pipeline callers don't act on update failures.
func Update(ctx context.Context, doc *models.Document) {
	dao.GetDB().WithContext(ctx).Save(doc)
}
