package orm_document

import (
	"context"
	"errors"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
	"gorm.io/gorm"
)

// GetDocumentByID fetches a document row by doc_id. Returns a zero-value
// Document with nil error if the row is not found. Returns a non-nil error
// for actual DB failures (connection lost, timeout, etc.) so callers can
// distinguish "not found" from "DB down".
func GetDocumentByID(ctx context.Context, docID string) (models.Document, error) {
	var doc models.Document
	result := dao.GetDB().WithContext(ctx).Where("doc_id = ?", docID).First(&doc)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return doc, result.Error
	}
	return doc, nil
}
