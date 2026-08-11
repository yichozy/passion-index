package orm_folder

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// Update persists all fields of an existing folder by primary key
// (gorm Save: full update). Caller should have loaded the folder first
// (e.g., via GetByID), mutated the desired fields, then call Update.
func Update(ctx context.Context, folder *models.Folder) error {
	return dao.GetDB().WithContext(ctx).Save(folder).Error
}
