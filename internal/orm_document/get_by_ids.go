package orm_document

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// GetByIDs fetches documents by primary key, projecting only the
// search-relevant columns (explicit select keeps internal columns —
// file_key, status, error, ... — out of the result). Used by the
// semantic node search to hydrate doc rows (filename) for candidates
// after BM25 recall.
func GetByIDs(ctx context.Context, ids []uuid.UUID) ([]models.Document, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []models.Document
	err := dao.GetDB().WithContext(ctx).
		Table("documents").
		Select("id, filename, title, description, metadata").
		Where("id IN ? AND deleted_at IS NULL", ids).
		Scan(&rows).Error
	return rows, err
}
