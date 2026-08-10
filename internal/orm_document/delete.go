package orm_document

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// DeleteDocument soft-deletes a document by id. Callers should also call
// orm_node.DeleteByDocID to clean up associated nodes.
func DeleteDocument(ctx context.Context, docID string) bool {
	result := dao.GetDB().WithContext(ctx).Where("id = ?", docID).Delete(&models.Document{})
	return result.RowsAffected > 0
}
