package models

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Node is both the gorm model for the nodes table and the in-memory tree
// structure. Composite PK (DocID, ID) — ID is a DFS counter (1, 2, 3...).
// ParentID nil = child of synthetic root (ID=0, not stored).
// The Nodes field is gorm:"-" — assembled in memory.
type Node struct {
	ID       int       `gorm:"primaryKey" json:"node_id"`
	DocID    uuid.UUID `gorm:"primaryKey;type:uuid" json:"doc_id"`
	ParentID *int      `gorm:"index" json:"parent_id"`

	Title     string         `json:"title"`
	PageStart int            `json:"page_start"`
	PageEnd   int            `json:"page_end"`
	Summary   string         `json:"summary"`
	Text      string         `json:"text"`
	Figures   []Figure       `gorm:"type:jsonb;serializer:json" json:"figures,omitempty"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Nodes []Node `gorm:"-" json:"nodes,omitempty"`
}

func (Node) TableName() string { return "nodes" }

// WalkLeaves visits every leaf node under n in depth-first order.
func (n *Node) WalkLeaves(visit func(*Node)) {
	if len(n.Nodes) == 0 {
		visit(n)
		return
	}
	for i := range n.Nodes {
		n.Nodes[i].WalkLeaves(visit)
	}
}

// FindByID returns the descendant node whose ID matches, or nil.
func (n *Node) FindByID(id int) *Node {
	if n.ID == id {
		return n
	}
	for i := range n.Nodes {
		if found := n.Nodes[i].FindByID(id); found != nil {
			return found
		}
	}
	return nil
}

// FlattenTree converts an in-memory Node tree into flat rows for DB.
// The synthetic root (ID=0) is NOT included — only its descendants.
func (root *Node) FlattenTree(docID uuid.UUID) []Node {
	var rows []Node
	for i := range root.Nodes {
		root.Nodes[i].flattenNode(docID, nil, &rows)
	}
	return rows
}

func (n *Node) flattenNode(docID uuid.UUID, parentID *int, rows *[]Node) {
	row := Node{
		DocID:     docID,
		ID:        n.ID,
		ParentID:  parentID,
		Title:     n.Title,
		PageStart: n.PageStart,
		PageEnd:   n.PageEnd,
		Summary:   n.Summary,
		Text:      n.Text,
		Figures:   n.Figures,
	}
	*rows = append(*rows, row)
	pid := n.ID
	for i := range n.Nodes {
		n.Nodes[i].flattenNode(docID, &pid, rows)
	}
}

// AssembleTree rebuilds an in-memory Node tree from flat rows.
// Uses top-down recursion: for each node, look up its children, recursively
// populate their subtrees, then assign. This avoids the copy-before-children-
// are-attached bug that plagues bottom-up approaches with []Node (value type).
func AssembleTree(rows []Node) *Node {
	// Index: parent_id → direct children. Root children have nil ParentID.
	children_by_parent := make(map[int][]Node)
	var top_level []Node

	for i := range rows {
		node := rows[i]
		node.Nodes = nil
		if rows[i].ParentID == nil {
			top_level = append(top_level, node)
		} else {
			parent_id := *rows[i].ParentID
			children_by_parent[parent_id] = append(children_by_parent[parent_id], node)
		}
	}

	// Top-down: for each node, recursively attach its children by index.
	var attach_subtree func(nodes []Node) []Node
	attach_subtree = func(nodes []Node) []Node {
		for i := range nodes {
			children := children_by_parent[nodes[i].ID]
			if len(children) > 0 {
				nodes[i].Nodes = attach_subtree(children)
			}
		}
		return nodes
	}

	return &Node{ID: 0, Nodes: attach_subtree(top_level)}
}
