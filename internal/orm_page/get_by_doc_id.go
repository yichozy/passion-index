package orm_page

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// GetByDocID returns a document's page rows ordered by page_idx.
func GetByDocID(ctx context.Context, doc_id uuid.UUID) ([]models.Page, error) {
	var rows []models.Page
	err := dao.GetDB().WithContext(ctx).
		Where("doc_id = ?", doc_id).
		Order("page_idx").
		Find(&rows).Error
	return rows, err
}
