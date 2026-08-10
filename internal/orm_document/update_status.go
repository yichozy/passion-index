package orm_document

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// UpdateStatus updates a document's status in DB.
func UpdateStatus(ctx context.Context, docID, status string) {
	dao.GetDB().WithContext(ctx).
		Model(&models.Document{}).
		Where("id = ?", docID).
		Update("status", status)
}
