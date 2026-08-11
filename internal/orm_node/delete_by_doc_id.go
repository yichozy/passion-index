package orm_node

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// DeleteByDocID soft-deletes all nodes for a document.
func DeleteByDocID(ctx context.Context, docID uuid.UUID) error {
	return dao.GetDB().WithContext(ctx).
		Where("doc_id = ?", docID).
		Delete(&models.Node{}).Error
}
