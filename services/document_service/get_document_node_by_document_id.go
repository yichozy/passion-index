package document_service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"
	"gorm.io/gorm"
)

func GetDocumentNodeByDocumentID(ctx context.Context, doc_id uuid.UUID, node_id int) (*models.Node, error) {
	node, err := orm_node.GetByID(ctx, doc_id, node_id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &node, nil
}
