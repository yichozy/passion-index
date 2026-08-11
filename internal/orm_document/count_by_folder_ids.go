package orm_document

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// CountByFolderIDs returns map[folder_id]direct_doc_count for the given
// folder IDs. One GROUP BY query, regardless of input size.
func CountByFolderIDs(ctx context.Context, folder_ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	var rows []struct {
		FolderID uuid.UUID
		Count    int64
	}
	if err := dao.GetDB().WithContext(ctx).
		Model(&models.Document{}).
		Where("folder_id IN ?", folder_ids).
		Select("folder_id, COUNT(*) AS count").
		Group("folder_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	count := make(map[uuid.UUID]int64, len(rows))
	for _, r := range rows {
		count[r.FolderID] = r.Count
	}
	return count, nil
}
