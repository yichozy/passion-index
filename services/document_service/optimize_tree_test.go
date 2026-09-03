package document_service

// slicePagesText unit tests plus one integration test over a real seed
// document (local DB, per project convention the benchmark docs are
// always ingested; -short skips the DB part). The expand logic itself —
// heading validation, boundary rule, prompt rendering — is inlined in
// OptimizeTree and only exercised end-to-end by uploads.

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/hopebox/env"
	hopelog "github.com/yichozy/hopebox/log"
	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"
)

func TestSlicePagesTextJoinsWithHoles(t *testing.T) {
	pages := []string{"a", "", "c", "d"}
	if got := slicePagesText(pages, 0, 3); got != "a\n\nc\n\nd" {
		t.Errorf("slice = %q", got)
	}
	if got := slicePagesText(pages, 2, 1); got != "" {
		t.Errorf("inverted range = %q, want empty", got)
	}
}

// --- integration (local DB) ---

const DOC0 = "01a013ac-5507-7a39-a346-74fd69d4f8ae" // CheckMate227.pdf

var db_once sync.Once

func require_db(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration: needs local DB; skipped in -short")
	}
	db_once.Do(dao.InitPgDbConn)
}

// subtreeEnd is the deepest page covered by n's subtree (test-only:
// OptimizeTree works on leaves, so the main file needs no subtree logic).
func subtreeEnd(n *models.Node) int {
	end := n.PageEnd
	for i := range n.Nodes {
		if child_end := subtreeEnd(&n.Nodes[i]); child_end > end {
			end = child_end
		}
	}
	return end
}

// The seeds predate the pages store, so OptimizeTree without pages must
// leave the real tree untouched (expand is the only pass).
func TestOptimizeTreeWithoutPagesIsNoOp(t *testing.T) {
	require_db(t)

	doc_id := uuid.MustParse(DOC0)
	rows, err := orm_node.GetByDocID(context.Background(), doc_id)
	if err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("doc %s has no nodes — seed documents missing from local DB", DOC0)
	}
	root := models.AssembleTree(rows)

	before := countNodes(root)
	OptimizeTree(context.Background(), root, nil) // no pages → skipped

	if after := countNodes(root); after != before {
		t.Errorf("node count %d → %d — without page text the tree must be untouched", before, after)
	}

	// Structural invariant regardless: every child span inside its parent's.
	var walk func(n *models.Node)
	walk = func(n *models.Node) {
		for i := range n.Nodes {
			child := &n.Nodes[i]
			if child.PageStart < n.PageStart || subtreeEnd(child) > subtreeEnd(n) {
				t.Errorf("child %q (%d-%d) escapes parent %q (%d-%d)",
					child.Title, child.PageStart, subtreeEnd(child), n.Title, n.PageStart, subtreeEnd(n))
			}
			walk(child)
		}
	}
	walk(root)
}

func TestMain(m *testing.M) {
	if os.Getenv("ENV") != "prod" {
		env.LoadEnvVariable()
	}
	hopelog.Init(hopelog.DefaultConfig())
	os.Exit(m.Run())
}
