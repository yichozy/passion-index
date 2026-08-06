package orm_document

import (
	"context"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// Create inserts a new document row. Returns an error so callers can
// surface DB failures (e.g., connection lost, primary key conflict) to
// the user — used by UploadDocument, where silent failure would leave
// the API client with a doc_id that doesn't exist.
func Create(ctx context.Context, doc *models.Document) error {
	return dao.GetDB().WithContext(ctx).Create(doc).Error
}
