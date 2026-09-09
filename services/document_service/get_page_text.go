package document_service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/internal/orm_page"
	"github.com/yichozy/passion-index/models"
)

// GetPageText returns per-page markdown text for a pages spec ("5",
// "3,7,10", "5-10", 1-based; expansion capped). Missing pages (blank or
// predating the pages store) carry empty text. ErrDocumentNotFound /
// ErrDocumentNotReady / ErrBadPageSpec gate the read.
func GetPageText(ctx context.Context, doc_id uuid.UUID, spec string) ([]models.PageText, error) {
	pages, err := ParsePagesSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadPageSpec, err)
	}
	doc, err := orm_document.GetDocumentByID(ctx, doc_id)
	if err != nil {
		return nil, err
	}
	if doc.ID == uuid.Nil {
		return nil, fmt.Errorf("%w: %s", ErrDocumentNotFound, doc_id)
	}
	if doc.Status != models.StatusDone {
		return nil, fmt.Errorf("%w: status=%s", ErrDocumentNotReady, doc.Status)
	}
	rows, err := orm_page.GetByDocID(ctx, doc_id)
	if err != nil {
		return nil, err
	}
	by_idx := make(map[int]models.Page, len(rows))
	for i := range rows {
		by_idx[rows[i].PageIdx] = rows[i]
	}
	out := make([]models.PageText, len(pages))
	for i, page := range pages {
		out[i] = models.PageText{Page: page}
		if row, ok := by_idx[page-1]; ok { // spec 1-based, PageIdx 0-based
			out[i].Text = row.Text
		}
	}
	return out, nil
}
