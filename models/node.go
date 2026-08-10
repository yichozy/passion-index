package models

import "github.com/google/uuid"

// Node is both the gorm model for the nodes table and the in-memory tree
// structure. The Nodes field (children) is gorm:"-" — assembled in memory.
//
// The synthetic root (NodeID="0000") is NOT stored in the nodes table;
// AssembleTree creates it on the fly.
type Node struct {
	BaseUUIDModel
	DocID    uuid.UUID `gorm:"index;type:uuid" json:"doc_id"`
	NodeID   string    `gorm:"index" json:"node_id"`   // "0001" tree position
	ParentID string    `gorm:"index" json:"parent_id"` // parent's NodeID, empty = root child

	Title     string   `json:"title"`
	PageStart int      `json:"page_start"`
	PageEnd   int      `json:"page_end"`
	Summary   string   `json:"summary"`
	Text      string   `json:"text"`
	Figures   []Figure `gorm:"type:jsonb;serializer:json" json:"figures,omitempty"`

	Nodes []Node `gorm:"-" json:"nodes,omitempty"`
}

func (Node) TableName() string { return "nodes" }

// WalkLeaves visits every leaf node (a node without children) under n in
// depth-first order, calling visit with a pointer to each.
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

// FlattenTree converts an in-memory Node tree (rooted at NodeID="0000")
// into a flat slice of Node rows for DB insertion. The synthetic root itself
// is NOT included — only its descendants.
func (root *Node) FlattenTree(docID uuid.UUID) []Node {
	var rows []Node
	for i := range root.Nodes {
		root.Nodes[i].flattenNode(docID, "", &rows)
	}
	return rows
}

func (n *Node) flattenNode(docID uuid.UUID, parentID string, rows *[]Node) {
	row := Node{
		DocID:     docID,
		NodeID:    n.NodeID,
		ParentID:  parentID,
		Title:     n.Title,
		PageStart: n.PageStart,
		PageEnd:   n.PageEnd,
		Summary:   n.Summary,
		Text:      n.Text,
		Figures:   n.Figures,
	}
	*rows = append(*rows, row)
	for i := range n.Nodes {
		n.Nodes[i].flattenNode(docID, n.NodeID, rows)
	}
}

// AssembleTree rebuilds an in-memory Node tree from flat Node rows.
// Creates a synthetic root (NodeID="0000").
func AssembleTree(rows []Node) *Node {
	root := &Node{NodeID: "0000"}
	nodeMap := map[string]*Node{}

	for i := range rows {
		n := &Node{
			BaseUUIDModel: rows[i].BaseUUIDModel,
			DocID:         rows[i].DocID,
			NodeID:        rows[i].NodeID,
			ParentID:      rows[i].ParentID,
			Title:         rows[i].Title,
			PageStart:     rows[i].PageStart,
			PageEnd:       rows[i].PageEnd,
			Summary:       rows[i].Summary,
			Text:          rows[i].Text,
			Figures:       rows[i].Figures,
		}
		nodeMap[n.NodeID] = n
	}

	for i := range rows {
		n := nodeMap[rows[i].NodeID]
		if rows[i].ParentID == "" {
			root.Nodes = append(root.Nodes, *n)
		} else if parent, ok := nodeMap[rows[i].ParentID]; ok {
			parent.Nodes = append(parent.Nodes, *n)
		}
	}

	return root
}
