package models

import (
	"testing"

	"github.com/google/uuid"
)

func TestAssembleTreeSyntheticRootCarriesDocID(t *testing.T) {
	docID := uuid.New()
	leftID := uuid.New()
	rightID := uuid.New()

	root := AssembleTree([]Node{
		{ID: leftID, DocID: docID, Title: "Left"},
		{ID: rightID, DocID: docID, Title: "Right"},
	})

	if root == nil {
		t.Fatal("expected root, got nil")
	}
	if root.ID != uuid.Nil {
		t.Fatalf("expected synthetic root ID to be nil UUID, got %s", root.ID)
	}
	if root.DocID != docID {
		t.Fatalf("expected synthetic root doc_id %s, got %s", docID, root.DocID)
	}
	if len(root.Nodes) != 2 {
		t.Fatalf("expected 2 top-level nodes, got %d", len(root.Nodes))
	}
}
