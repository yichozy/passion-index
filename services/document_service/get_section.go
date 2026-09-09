package document_service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"
	"gorm.io/gorm"
)

// GetSection returns one section (tree node) with its direct children
// attached — the row's Nodes field is gorm:"-", so children are a
// second query. ErrSectionNotFound when the id matches nothing.
func GetSection(ctx context.Context, node_id uuid.UUID) (*models.Node, error) {
	node, err := orm_node.GetByID(ctx, node_id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrSectionNotFound, node_id)
		}
		return nil, err
	}
	children, err := orm_node.GetChildren(ctx, node_id)
	if err != nil {
		return nil, err
	}
	if children != nil {
		node.Nodes = children
	}
	return &node, nil
}
