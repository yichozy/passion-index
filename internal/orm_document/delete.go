package orm_document

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
	"gorm.io/gorm"
)

// DeleteDocument soft-deletes a document and all its nodes atomically.
// Returns deleted=true if a document row was actually removed; false if no
// matching row was found.
func DeleteDocument(ctx context.Context, docID uuid.UUID) (bool, error) {
	var deleted bool
	err := dao.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Where("doc_id = ?", docID).Delete(&models.Node{}).Error; e != nil {
			return e
		}
		result := tx.Where("id = ?", docID).Delete(&models.Document{})
		deleted = result.RowsAffected > 0
		return result.Error
	})
	return deleted, err
}
