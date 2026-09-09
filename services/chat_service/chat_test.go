package chat_service

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/hopebox/env"
	hopelog "github.com/yichozy/hopebox/log"
	"github.com/yichozy/passion-index/models"
)

func TestValidateCitations(t *testing.T) {
	node_a := uuid.New()
	node_b := uuid.New()
	trace := []trace_entry{
		{filename: "CheckMate227.pdf", node_id: node_a, page_start: 5, page_end: 7, snippet: "safety"},
		{filename: "NCT02425891.pdf", node_id: node_b, page_start: 3, page_end: 4, snippet: "pfs"},
	}

	cases := []struct {
		name      string
		answer    string
		want_node []uuid.UUID
	}{
		{"exact tag", "Grade 3-4 events were common [CheckMate227.pdf p.6].", []uuid.UUID{node_a}},
		{"range tag", "x [CheckMate227 p.6-7] y", []uuid.UUID{node_a}},
		{"shortened filename", "x [CheckMate227 p.6] y", []uuid.UUID{node_a}},
		{"dangling tag dropped", "fabricated [Ghost.pdf p.6]", nil},
		{"page mismatch dropped", "wrong page [CheckMate227.pdf p.20]", nil},
		{"second doc", "both [CheckMate227.pdf p.6] and [NCT02425891 p.4]", []uuid.UUID{node_a, node_b}},
		{"dedup same node", "twice [CheckMate227.pdf p.6] [CheckMate227.pdf p.7]", []uuid.UUID{node_a}},
		{"no tags", "plain answer", nil},
	}
	for _, c := range cases {
		got := validate_citations(c.answer, trace)
		if len(got) != len(c.want_node) {
			t.Errorf("%s: got %d citations, want %d (%+v)", c.name, len(got), len(c.want_node), got)
			continue
		}
		for i := range got {
			if got[i].NodeID == nil || *got[i].NodeID != c.want_node[i] {
				t.Errorf("%s: citation %d node mismatch", c.name, i)
			}
			if got[i].PageStart != trace_pages(t, trace, c.want_node[i]).start+1 {
				t.Errorf("%s: citation pages must be 1-based", c.name)
			}
		}
	}
}

func TestValidateCitationsPageLevelTrace(t *testing.T) {
	trace := []trace_entry{
		{filename: "Old.pdf", page_start: 4, page_end: 4, snippet: "page five"}, // page read, no node
		{filename: "Old.pdf", page_start: 5, page_end: 5, snippet: "page six"},  // distinct page
		{filename: "New.pdf", node_id: uuid.New(), page_start: 4, page_end: 6},  // section read
	}
	got := validate_citations("a [Old.pdf p.5] b [Old.pdf p.6] c [New.pdf p.5]", trace)
	if len(got) != 3 {
		t.Fatalf("got %d citations, want 3 (%+v)", len(got), got)
	}
	if got[0].NodeID != nil || got[1].NodeID != nil {
		t.Errorf("page-level citations must have nil node_id: %+v %+v", got[0], got[1])
	}
	if got[2].NodeID == nil {
		t.Errorf("section citation must carry node_id")
	}
	if got[0].PageStart != 5 || got[1].PageStart != 6 {
		t.Errorf("page citations: %+v %+v", got[0], got[1])
	}
}

func trace_pages(t *testing.T, trace []trace_entry, id uuid.UUID) struct{ start, end int } {
	t.Helper()
	for i := range trace {
		if trace[i].node_id == id {
			return struct{ start, end int }{trace[i].page_start, trace[i].page_end}
		}
	}
	t.Fatal("node not in trace")
	return struct{ start, end int }{}
}

// --- integration (local DB + real LLM; -short skips) ---

var db_once sync.Once

func require_db_llm(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration: needs local DB + LLM; skipped in -short")
	}
	db_once.Do(dao.InitPgDbConn)
}

func TestChatSemanticDiscoveryWithCitations(t *testing.T) {
	require_db_llm(t)
	result, err := Chat(context.Background(), []models.ChatMessage{
		{Role: "user", Content: "What did the CheckMate227 cost-effectiveness analysis conclude about nivolumab plus ipilimumab? Cite the supporting sections."},
	}, nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	t.Logf("answer: %.300s", result.Answer)
	if len(result.Answer) < 50 {
		t.Fatalf("answer too short: %q", result.Answer)
	}
	found := false
	for i := range result.Citations {
		t.Logf("citation: %s p.%d-%d", result.Citations[i].Filename, result.Citations[i].PageStart, result.Citations[i].PageEnd)
		if strings.Contains(result.Citations[i].Filename, "CheckMate227") {
			found = true
		}
	}
	if !found {
		t.Errorf("no CheckMate227 citation — discovery or citation validation failed")
	}
}

func TestChatBrowseListing(t *testing.T) {
	require_db_llm(t)
	result, err := Chat(context.Background(), []models.ChatMessage{
		{Role: "user", Content: "我库里现在都有哪些文档？"},
	}, nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	t.Logf("answer: %.300s", result.Answer)
	if len(result.Answer) < 20 {
		t.Fatalf("answer too short: %q", result.Answer)
	}
}

func TestChatKeywordEscalation(t *testing.T) {
	require_db_llm(t)
	result, err := Chat(context.Background(), []models.ChatMessage{
		{Role: "user", Content: "Find the document for trial NCT02425891 and tell me its trial phase in one sentence."},
	}, nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	t.Logf("answer: %.300s", result.Answer)
	if !strings.Contains(result.Answer, "NCT02425891") && !strings.Contains(strings.ToUpper(result.Answer), "PHASE 3") {
		t.Errorf("answer does not reference the trial: %q", result.Answer)
	}
}

func TestMain(m *testing.M) {
	if os.Getenv("ENV") != "prod" {
		env.LoadEnvVariable()
	}
	hopelog.Init(hopelog.DefaultConfig())
	os.Exit(m.Run())
}
