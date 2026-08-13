// Package document_service contains document-related business logic:
// PDF upload + async processing pipeline, Popo→Node tree mapping,
// page-based node lookup, tree loading, and leaf-node LLM summarization.
package document_service

import (
	"github.com/google/uuid"
	"github.com/yichozy/hopebox/mineru_popo"
	"github.com/yichozy/passion-index/models"
)

// ---- Tree mapping (Popo → Node) ----

// ConvertPopoResultToTree converts a Popo build_tree root into a root
// models.Node (ID=uuid.Nil, synthetic) plus the document's total page
// count. Visual-typed PopoNodes (image/table/chart/seal) are folded into
// the parent node's Figures[].
func ConvertPopoResultToTree(popo_doc *mineru_popo.PopoNode) (*models.Node, int) {
	if popo_doc == nil {
		return &models.Node{ID: uuid.Nil}, 0
	}
	page_count := countPages(popo_doc)
	var nodes []models.Node
	for i := range popo_doc.Children {
		child := &popo_doc.Children[i]
		if isVisualType(child.Type) {
			continue
		}
		nodes = append(nodes, convertNode(child))
	}
	return &models.Node{ID: uuid.Nil, Nodes: nodes}, page_count
}

func convertNode(popoNode *mineru_popo.PopoNode) models.Node {
	n := models.Node{
		ID:    uuid.New(),
		Title: popoNode.Title,
		Text:  popoNode.Content,
	}
	if len(popoNode.Location) > 0 {
		minP, maxP := popoNode.Location[0].Page, popoNode.Location[0].Page
		for _, loc := range popoNode.Location[1:] {
			minP, maxP = min(minP, loc.Page), max(maxP, loc.Page)
		}
		n.PageStart, n.PageEnd = minP-1, maxP-1
	}
	for i := range popoNode.Children {
		child := &popoNode.Children[i]
		if isVisualType(child.Type) {
			// Visual child → fold into Figures.
			page := 0
			if len(child.Location) > 0 {
				page = child.Location[0].Page - 1
			}
			n.Figures = append(n.Figures, models.Figure{
				Name:    child.ImgPath,
				Page:    page,
				Caption: child.Caption,
			})
			continue
		}
		n.Nodes = append(n.Nodes, convertNode(child))
	}
	return n
}

func isVisualType(t string) bool {
	switch t {
	case "image", "table", "chart", "seal", "image_block":
		return true
	}
	return false
}

func countPages(popoNode *mineru_popo.PopoNode) int {
	maxP := 0
	for _, loc := range popoNode.Location {
		maxP = max(maxP, loc.Page)
	}
	for i := range popoNode.Children {
		maxP = max(maxP, countPages(&popoNode.Children[i]))
	}
	return maxP
}
