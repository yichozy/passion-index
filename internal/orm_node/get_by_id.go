package orm_node

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// GetByID returns a single node row by doc_id + node_id.
func GetByID(ctx context.Context, docID uuid.UUID, nodeID int) (models.Node, error) {
	var row models.Node
	err := dao.GetDB().WithContext(ctx).
		Where("doc_id = ? AND id = ?", docID, nodeID).
		First(&row).Error
	return row, err
}
