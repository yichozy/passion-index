package orm_node

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// GetChildren returns the direct child rows of one node, ordered by
// page then id for stable section order.
func GetChildren(ctx context.Context, parent_id uuid.UUID) ([]models.Node, error) {
	var rows []models.Node
	err := dao.GetDB().WithContext(ctx).
		Where("parent_id = ?", parent_id).
		Order("page_start, id").
		Find(&rows).Error
	return rows, err
}
