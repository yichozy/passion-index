package orm_document

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// GetDocumentByID fetches a document row by doc_id. Returns a zero-value
// Document if not found (subsequent OSS/pipeline calls will fail loudly
// on an empty FileKey).
func GetDocumentByID(ctx context.Context, docID string) models.Document {
	var doc models.Document
	dao.GetDB().WithContext(ctx).Where("doc_id = ?", docID).First(&doc)
	return doc
}
