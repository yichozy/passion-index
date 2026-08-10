package document_service

import (
	"context"

	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/models"
)

// GetDocumentNode loads the document tree and returns the node with the given
// node_id, or (nil, nil) if the document has no tree or the node_id is not
// found.
func GetDocumentNode(ctx context.Context, doc_id, node_id string) (*models.Node, error) {
	root := orm_document.GetDocumentByID(ctx, doc_id).Tree
	if root == nil {
		return nil, nil
	}
	return root.FindByNodeID(node_id), nil
}
