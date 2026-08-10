package models

// Node is a section in the document tree (chapter / section / subsection).
//
// Summary is generated only for leaf nodes by document_service's
// SummarizeDocumentTree, using a vision LLM (title + text + this node's
// figures). Non-leaf nodes have Summary="".
type Node struct {
	NodeID    string   `json:"node_id"` // 4-digit zero-padded, "0001".."9999"
	Title     string   `json:"title"`
	PageStart int      `json:"page_start"` // 0-based physical page
	PageEnd   int      `json:"page_end"`   // 0-based, inclusive
	Summary   string   `json:"summary"`    // LLM-generated, bottom-up
	Figures   []Figure `json:"figures,omitempty"`
	Text      string   `json:"text,omitempty"`  // raw text
	Nodes     []Node   `json:"nodes,omitempty"` // recursive children
}

// WalkLeaves visits every leaf node (a node without children) under n in
// depth-first order, calling visit with a pointer to each. n itself is
// visited only if it has no children.
func (n *Node) WalkLeaves(visit func(*Node)) {
	if len(n.Nodes) == 0 {
		visit(n)
		return
	}
	for i := range n.Nodes {
		n.Nodes[i].WalkLeaves(visit)
	}
}

// FindByNodeID returns the descendant node (including n itself) whose NodeID
// matches, or nil if not found.
func (n *Node) FindByNodeID(nodeID string) *Node {
	if n.NodeID == nodeID {
		return n
	}
	for i := range n.Nodes {
		if found := n.Nodes[i].FindByNodeID(nodeID); found != nil {
			return found
		}
	}
	return nil
}
