package orm_folder

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// Create inserts a new folder row. Caller passes a *models.Folder with at
// least Name (and optionally ParentID) set; ID/CreatedAt/UpdatedAt are
// populated by GORM hooks on return.
func Create(ctx context.Context, folder *models.Folder) error {
	return dao.GetDB().WithContext(ctx).Create(folder).Error
}
