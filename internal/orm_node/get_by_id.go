package orm_node

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// GetByID returns a single node row by its UUID PK.
func GetByID(ctx context.Context, id uuid.UUID) (models.Node, error) {
	var row models.Node
	err := dao.GetDB().WithContext(ctx).Where("id = ?", id).First(&row).Error
	return row, err
}
