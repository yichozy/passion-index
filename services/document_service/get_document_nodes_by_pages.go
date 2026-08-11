package document_service

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"
)

func GetDocumentNodesByPages(ctx context.Context, doc_id uuid.UUID, pages []int) ([]*models.Node, error) {
	rows, err := orm_node.GetByPages(ctx, doc_id, pages)
	if err != nil {
		return nil, err
	}
	nodes := make([]*models.Node, len(rows))
	for i := range rows {
		nodes[i] = &rows[i]
	}
	return nodes, nil
}
