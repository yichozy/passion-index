package orm_document

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// GetFilenamesByIDs batch-loads filenames for a set of document IDs.
// Returns a map of docID → filename.
func GetFilenamesByIDs(ctx context.Context, doc_ids []string) (map[string]string, error) {
	if len(doc_ids) == 0 {
		return map[string]string{}, nil
	}
	var docs []models.Document
	err := dao.GetDB().WithContext(ctx).
		Select("id, filename").
		Where("id IN ?", doc_ids).
		Find(&docs).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(docs))
	for _, doc := range docs {
		result[doc.ID.String()] = doc.Filename
	}
	return result, nil
}
