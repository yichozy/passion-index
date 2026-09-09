package document_service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"
)

// GetDocumentStructure returns the document's assembled tree — every
// section with title, node id, page range, summary, and children
// (PageIndex cloud get_document_structure shape). ErrDocumentNotFound /
// ErrDocumentNotReady gate the read; a DONE document with no nodes
// yields an empty synthetic root.
func GetDocumentStructure(ctx context.Context, doc_id uuid.UUID) (*models.Node, error) {
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
	rows, err := orm_node.GetByDocID(ctx, doc_id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return &models.Node{ID: uuid.Nil, DocID: doc_id}, nil
	}
	return models.AssembleTree(rows), nil
}
