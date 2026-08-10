package orm_node

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// UpdateSummary updates a single node's summary.
func UpdateSummary(ctx context.Context, doc_id string, node_id int, summary string) error {
	return dao.GetDB().WithContext(ctx).
		Model(&models.Node{}).
		Where("doc_id = ? AND id = ?", doc_id, node_id).
		Update("summary", summary).Error
}
