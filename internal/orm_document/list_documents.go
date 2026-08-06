package orm_document

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// ListDocuments returns a paginated list of all documents ordered by
// created_at DESC. Returns (docs, total, err) where total is the total
// row count (ignoring limit/offset).
func ListDocuments(ctx context.Context, limit, offset int) ([]models.Document, int64, error) {
	var total int64
	if err := dao.GetDB().WithContext(ctx).Model(&models.Document{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var docs []models.Document
	if err := dao.GetDB().WithContext(ctx).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&docs).Error; err != nil {
		return nil, 0, err
	}

	return docs, total, nil
}
