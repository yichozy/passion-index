package orm_document

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// ListDocumentsByFolder returns documents under a folder with pagination.
//
//	recursive=false → documents directly in that folder
//	recursive=true  → documents in folder + all descendants (single recursive
//	                  CTE collects descendant folder IDs in one round-trip)
func ListDocumentsByFolder(ctx context.Context, folder_id uuid.UUID, recursive bool, limit, offset int) ([]models.Document, int64, error) {
	db := dao.GetDB().WithContext(ctx).Model(&models.Document{})
	if recursive {
		db = db.Where(`folder_id IN (
			WITH RECURSIVE subtree AS (
				SELECT id FROM folders WHERE id = ? AND deleted_at IS NULL
				UNION ALL
				SELECT f.id FROM folders f JOIN subtree s ON f.parent_id = s.id
				WHERE f.deleted_at IS NULL
			)
			SELECT id FROM subtree
		)`, folder_id)
	} else {
		db = db.Where("folder_id = ?", folder_id)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var docs []models.Document
	if err := db.Preload("Folder").Order("created_at DESC").Limit(limit).Offset(offset).Find(&docs).Error; err != nil {
		return nil, 0, err
	}
	return docs, total, nil
}
