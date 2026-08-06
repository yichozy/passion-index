package document_service

import (
	"context"

	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/models"
)

// GetDocumentNodesByPages loads the document tree for doc_id and returns
// the covering node for each page in `pages`, deduplicated by NodeID (a
// node spanning multiple requested pages is returned once). Pages with no
// covering node are skipped. Returns (nil, nil) if the document has no
// tree yet or pages is empty.
func GetDocumentNodesByPages(ctx context.Context, doc_id string, pages []int) ([]*models.Node, error) {
	root := orm_document.GetDocumentByID(ctx, doc_id).Tree
	if root == nil || len(pages) == 0 {
		return nil, nil
	}
	seen_nodes := make(map[string]bool)
	var nodes []*models.Node
	for _, page := range pages {
		node := getNodesByPage(root, page)
		if node == nil || seen_nodes[node.NodeID] {
			continue
		}
		seen_nodes[node.NodeID] = true
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// getNodesByPage returns the deepest descendant of root (excluding root
// itself, which is the synthetic NodeID="0000") whose [PageStart, PageEnd]
// range contains page, or nil if no node covers it.
func getNodesByPage(root *models.Node, page int) *models.Node {
	for i := range root.Nodes {
		if node := getNodesByPageDFS(&root.Nodes[i], page); node != nil {
			return node
		}
	}
	return nil
}

func getNodesByPageDFS(node *models.Node, page int) *models.Node {
	if page < node.PageStart || page > node.PageEnd {
		return nil
	}
	for i := range node.Nodes {
		if deeper_node := getNodesByPageDFS(&node.Nodes[i], page); deeper_node != nil {
			return deeper_node
		}
	}
	return node
}
