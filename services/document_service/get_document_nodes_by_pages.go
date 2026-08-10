package document_service

import (
	"context"

	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/models"
)

// GetDocumentNodesByPages loads the document tree for doc_id and returns
// all nodes whose [PageStart, PageEnd] range covers each page in `pages`,
// deduplicated by NodeID. Pages with no covering node are skipped.
// Returns (nil, nil) if the document has no tree yet or pages is empty.
func GetDocumentNodesByPages(ctx context.Context, doc_id string, pages []int) ([]*models.Node, error) {
	doc, err := orm_document.GetDocumentByID(ctx, doc_id)
	if err != nil {
		return nil, err
	}
	if doc.Tree == nil || len(pages) == 0 {
		return nil, nil
	}

	seen_nodes := make(map[string]bool)
	var nodes []*models.Node
	for _, page := range pages {
		for _, node := range getNodesByPage(doc.Tree, page) {
			if seen_nodes[node.NodeID] {
				continue
			}
			seen_nodes[node.NodeID] = true
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}

// getNodesByPage returns all nodes (excluding the synthetic root 0000)
// whose [PageStart, PageEnd] range contains the given page.
func getNodesByPage(root *models.Node, page int) []*models.Node {
	var nodes []*models.Node
	for i := range root.Nodes {
		collectNodesByPage(&root.Nodes[i], page, &nodes)
	}
	return nodes
}

func collectNodesByPage(node *models.Node, page int, out *[]*models.Node) {
	if page >= node.PageStart && page <= node.PageEnd {
		*out = append(*out, node)
	}
	for i := range node.Nodes {
		collectNodesByPage(&node.Nodes[i], page, out)
	}
}
