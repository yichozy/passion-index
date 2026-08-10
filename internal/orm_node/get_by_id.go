package orm_node

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// GetByID returns a single node row by doc_id + node_id.
func GetByID(ctx context.Context, doc_id, node_id string) (models.Node, error) {
	var row models.Node
	err := dao.GetDB().WithContext(ctx).
		Where("doc_id = ? AND node_id = ?", doc_id, node_id).
		First(&row).Error
	return row, err
}
