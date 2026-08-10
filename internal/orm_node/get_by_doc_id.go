package orm_node

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// GetByDocID returns all node rows for a document, ordered by node_id.
func GetByDocID(ctx context.Context, doc_id string) ([]models.Node, error) {
	var rows []models.Node
	err := dao.GetDB().WithContext(ctx).
		Where("doc_id = ?", doc_id).
		Order("node_id").
		Find(&rows).Error
	return rows, err
}
