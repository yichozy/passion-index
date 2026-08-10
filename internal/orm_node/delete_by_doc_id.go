package orm_node

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// DeleteByDocID soft-deletes all nodes for a document.
func DeleteByDocID(ctx context.Context, docID string) error {
	return dao.GetDB().WithContext(ctx).
		Where("doc_id = ?", docID).
		Delete(&models.Node{}).Error
}
