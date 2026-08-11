package orm_folder

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
	"gorm.io/gorm"
)

// GetByID returns a single folder by ID.
// Returns (nil, nil) when the folder does not exist.
func GetByID(ctx context.Context, id uuid.UUID) (*models.Folder, error) {
	var folder models.Folder
	err := dao.GetDB().WithContext(ctx).Where("id = ?", id).First(&folder).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &folder, nil
}
