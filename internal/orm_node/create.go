package orm_node

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// Create batch-inserts node rows for a document.
func Create(ctx context.Context, rows []models.Node) error {
	if len(rows) == 0 {
		return nil
	}
	return dao.GetDB().WithContext(ctx).Create(&rows).Error
}
