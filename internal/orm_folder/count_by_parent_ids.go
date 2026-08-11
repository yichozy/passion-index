package orm_folder

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// CountByParentIDs returns map[parent_id]direct_child_count for the given
// parent IDs. One GROUP BY query, regardless of input size.
func CountByParentIDs(ctx context.Context, parent_ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	var rows []struct {
		ParentID uuid.UUID
		Count    int64
	}
	if err := dao.GetDB().WithContext(ctx).
		Model(&models.Folder{}).
		Where("parent_id IN ?", parent_ids).
		Select("parent_id, COUNT(*) AS count").
		Group("parent_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	count := make(map[uuid.UUID]int64, len(rows))
	for _, r := range rows {
		count[r.ParentID] = r.Count
	}
	return count, nil
}
