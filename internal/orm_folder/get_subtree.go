package orm_folder

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// GetSubtree pulls folders via a single recursive CTE.
//
//	root_id nil → all top-level folders (parent_id IS NULL) + descendants
//	root_id set → the anchor folder + its descendants
//	depth       → max nesting level to expand (0 = anchor only)
func GetSubtree(ctx context.Context, root_id *uuid.UUID, depth int) ([]models.Folder, error) {
	db := dao.GetDB().WithContext(ctx)

	var anchor string
	var args []any
	if root_id == nil {
		anchor = "parent_id IS NULL"
	} else {
		anchor = "id = ?"
		args = append(args, *root_id)
	}
	args = append(args, depth)

	query := `
		WITH RECURSIVE subtree AS (
			SELECT id, name, parent_id, created_at, updated_at, deleted_at, 0 AS lvl
			FROM folders
			WHERE ` + anchor + ` AND deleted_at IS NULL
			UNION ALL
			SELECT f.id, f.name, f.parent_id, f.created_at, f.updated_at, f.deleted_at, s.lvl + 1
			FROM folders f
			JOIN subtree s ON f.parent_id = s.id
			WHERE f.deleted_at IS NULL AND s.lvl < ?
		)
		SELECT id, name, parent_id, created_at, updated_at, deleted_at FROM subtree
		ORDER BY name`

	var folders []models.Folder
	if err := db.Raw(query, args...).Scan(&folders).Error; err != nil {
		return nil, err
	}
	return folders, nil
}
