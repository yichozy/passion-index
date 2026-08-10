package document_service

import (
	"context"
	"errors"

	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"
	"gorm.io/gorm"
)

func GetDocumentNode(ctx context.Context, doc_id, node_id string) (*models.Node, error) {
	node, err := orm_node.GetByID(ctx, doc_id, node_id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &node, nil
}
