package orm_folder

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// Delete soft-deletes a single folder by primary key. No cascade — callers
// must ensure preconditions (e.g., no documents, no sub-folders) themselves.
func Delete(ctx context.Context, id uuid.UUID) error {
	return dao.GetDB().WithContext(ctx).Delete(&models.Folder{}, id).Error
}
