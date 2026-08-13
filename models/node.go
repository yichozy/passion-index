package models

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Node is both the gorm model for the nodes table and the in-memory tree
// structure. UUID PK (ID) is globally unique. ParentID nil = child of
// synthetic root (ID=uuid.Nil, not stored). The Nodes field is gorm:"-"
// — assembled in memory.
type Node struct {
	ID       uuid.UUID  `gorm:"primaryKey;type:uuid" json:"id"`
	DocID    uuid.UUID  `gorm:"index;type:uuid" json:"doc_id"`
	ParentID *uuid.UUID `gorm:"index;type:uuid" json:"parent_id"`

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

// GroupByLevelBottomUp returns non-synthetic nodes grouped by depth, in bottom-up
// order (deepest level first, root level last). The synthetic root (uuid.Nil)
// is excluded — every returned node is a real document section. Within each
// level, nodes are in DFS order for stable processing.
//
// Used for bottom-up passes like summary generation: process each level in
// parallel, wait, move up one level so parents can see their children's
// results.
//
// Filter is by ID (== uuid.Nil means synthetic), not by depth, so the
// function works whether the input root is the synthetic wrapper or an
// unwrapped real top-level node (AssembleTree unwraps when there is one).
func (root *Node) GroupByLevelBottomUp() [][]*Node {
	by_depth := map[int][]*Node{}
	max_depth := 0
	var walk func(n *Node, depth int)
	walk = func(n *Node, depth int) {
		if n.ID != uuid.Nil { // skip synthetic root; real nodes always included
			by_depth[depth] = append(by_depth[depth], n)
		}
		if depth > max_depth {
			max_depth = depth
		}
		for i := range n.Nodes {
			walk(&n.Nodes[i], depth+1)
		}
	}
	walk(root, 0)

	levels := make([][]*Node, 0, max_depth)
	for d := max_depth; d >= 0; d-- {
		if nodes, ok := by_depth[d]; ok {
			levels = append(levels, nodes)
		}
	}
	return levels
}

// FlattenTree converts an in-memory Node tree into flat rows for DB.
// The synthetic root (uuid.Nil) is NOT included — only its descendants.
func (root *Node) FlattenTree(docID uuid.UUID) []Node {
	var rows []Node
	for i := range root.Nodes {
		root.Nodes[i].flattenNode(docID, nil, &rows)
	}
	return rows
}

func (n *Node) flattenNode(docID uuid.UUID, parentID *uuid.UUID, rows *[]Node) {
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
// Top-down recursion: index children by parent_id, then walk the top-level
// nodes and recursively attach their subtrees. This avoids the
// copy-before-children-are-attached bug that plagues bottom-up approaches
// with []Node (value type).
//
// Returns the single top-level node directly when there is one. When there
// are multiple top-level nodes, wraps them in a synthetic root (uuid.Nil)
// so the return type stays *Node.
func AssembleTree(rows []Node) *Node {
	children_by_parent := map[uuid.UUID][]Node{}
	var top_level []Node
	for i := range rows {
		node := rows[i]
		if node.ParentID == nil {
			top_level = append(top_level, node)
		} else {
			parent_id := *node.ParentID
			children_by_parent[parent_id] = append(children_by_parent[parent_id], node)
		}
	}

	var attach_subtree func(nodes []Node) []Node
	attach_subtree = func(nodes []Node) []Node {
		for i := range nodes {
			nodes[i].Nodes = attach_subtree(children_by_parent[nodes[i].ID])
		}
		return nodes
	}

	tree := attach_subtree(top_level)
	if len(tree) == 1 {
		return &tree[0]
	}
	return &Node{ID: uuid.Nil, Nodes: tree}
}
