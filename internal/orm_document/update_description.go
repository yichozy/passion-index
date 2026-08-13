package orm_document

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// UpdateDescription sets the doc-level description (root node's summary)
// without touching other fields. Used by ReSummarizeDocumentTree after
// summaries regenerate.
func UpdateDescription(ctx context.Context, doc_id uuid.UUID, description string) error {
	return dao.GetDB().WithContext(ctx).
		Model(&models.Document{}).
		Where("id = ?", doc_id).
		Update("description", description).Error
}
