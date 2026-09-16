package chat_service

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/hopebox/env"
	hopelog "github.com/yichozy/hopebox/log"
	"github.com/yichozy/passion-index/models"
)

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
