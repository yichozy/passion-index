package orm_node

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// UpdateSummary updates a single node's summary by UUID.
func UpdateSummary(ctx context.Context, node_id uuid.UUID, summary string) error {
	return dao.GetDB().WithContext(ctx).
		Model(&models.Node{}).
		Where("id = ?", node_id).
		Update("summary", summary).Error
}
