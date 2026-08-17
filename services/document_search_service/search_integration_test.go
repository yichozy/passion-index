package document_search_service

// Integration tests for beam/block retrieval and the semantic node
// search service against the local DB's seed documents and a real LLM.
// They replace the old cmd/beam-demo + scripts/beam-benchmark.sh pair.
//
// Assumptions (per project convention, 2026-08): the local paradedb has
// the three benchmark documents ingested (DOC0/1/2 below) and .env holds
// the LLM credentials. Run with:
//
//	go test ./services/document_search_service/ -run Quality -v
//
// `go test -short` skips everything here (no DB / LLM needed).
import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/hopebox/env"
	hopelog "github.com/yichozy/hopebox/log"
	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"
)

// Seed documents (must stay ingested in the local DB — the benchmark docs).
const (
	DOC0 = "019fff60-59d8-7870-909c-b08b819fd85e" // nivolumab+ipilimumab cost-effectiveness (NSCLC)
	DOC1 = "019fff65-7444-7900-9420-cbd2853c7367" // WT1 peptide vaccine (AML)
	DOC2 = "019fff5a-6895-732c-8916-d41d32974099" // atezolizumab+nab-paclitaxel (TNBC)
)

func TestMain(m *testing.M) {
	if os.Getenv("ENV") != "prod" {
		env.LoadEnvVariable()
	}
	hopelog.Init(hopelog.DefaultConfig())
	os.Exit(m.Run())
}

var (
	db_once   sync.Once
	tree_once sync.Map // doc_id string → *models.Node (loaded once per process)
)

// require_db skips in -short mode and initializes the DB connection on
// first use.
func require_db(t *testing.T) {
	t.Helper()
	require_llm(t)
	db_once.Do(dao.InitPgDbConn)
}

// require_llm skips in -short mode only — for tests that call the LLM
// but need no DB (synthetic trees).
func require_llm(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration: needs LLM; skipped in -short")
	}
}

// load_tree fetches and assembles a doc's tree once per process.
func load_tree(t *testing.T, doc_id uuid.UUID) *models.Node {
	t.Helper()
	if cached, ok := tree_once.Load(doc_id.String()); ok {
		return cached.(*models.Node)
	}
	rows, err := orm_node.GetByDocID(context.Background(), doc_id)
	if err != nil {
		t.Fatalf("load nodes (doc=%s): %v", doc_id, err)
	}
	if len(rows) == 0 {
		t.Fatalf("doc %s has no nodes — seed documents missing from local DB", doc_id)
	}
	root := models.AssembleTree(rows)
	tree_once.Store(doc_id.String(), root)
	return root
}

func mustUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return id
}

// benchmark_case mirrors one row of the old beam-benchmark.sh CASES matrix:
// doc | query | expected top-1 node title (substring match).
type benchmark_case struct {
	doc      string
	query    string
	expected string
}

var benchmark_cases = []benchmark_case{
	// ---- doc0: cost-effectiveness of nivolumab+ipilimumab ----
	{DOC0, "nivolumab plus ipilimumab incremental cost effectiveness ratio results", "Base-Case and Subgroup Analyses"},
	{DOC0, "how was the markov model structured what health states", "Model Structure"},
	{DOC0, "utility values quality of life inputs used in the model", "Cost and Utility Model Inputs"},
	{DOC0, "did the combination remain cost effective under sensitivity analyses", "Sensitivity Analyses"},
	{DOC0, "survival curve inputs taken from the checkmate trial", "Clinical Model Inputs"},

	// ---- doc1: WT1 vaccine in AML ----
	{DOC1, "WT1 peptide vaccine induced T-cell immune responses", "Immunologic responses"},
	{DOC1, "were there any allergic or hypersensitivity reactions", "Safety and toxicity"},
	{DOC1, "how many patients stayed in remission and how many relapsed", "Patient characteristics and clinical outcomes"},
	{DOC1, "peptide sequences dose and montanide adjuvant composition", "Vaccine formulation"},
	{DOC1, "how was minimal residual disease monitored by RT-PCR", "Measurement of WT1 transcript by RT-PCR"},
	{DOC1, "eligibility criteria and IRB approval for the trial", "Trial design"},

	// ---- doc2: atezolizumab in TNBC ----
	{DOC2, "atezolizumab plus nab-paclitaxel progression-free survival benefit", "FINAL PROGRESSION-FREE SURVIVAL ANALYSIS"},
	{DOC2, "grade 3-4 adverse events and immune-related events", "SAFETY"},
	{DOC2, "overall survival interim analysis 21.3 versus 17.6 months", "INTERIM OVERALL SURVIVAL ANALYSIS"},
	{DOC2, "patient eligibility inclusion and exclusion criteria", "PATIENTS"},
	{DOC2, "tumor response rate and complete response", "RESPONSE RATE AND DURATION OUTCOMES"},
	{DOC2, "无进展生存期结果", "FINAL PROGRESSION-FREE SURVIVAL ANALYSIS"},
}

// MIN_QUALITY_HITS is the quality gate: deep beam search must top-1 at
// least this many of the 17 benchmark cases (the old PoC bar was 3/5 per
// doc; LLM output is nondeterministic, so we gate on the aggregate rate
// instead of failing per-case).
const MIN_QUALITY_HITS = 12

func TestBeamSearchDeep_QualityGate(t *testing.T) {
	require_db(t)

	// Load trees in the test goroutine — Fatalf is not goroutine-safe.
	trees := map[string]*models.Node{
		DOC0: load_tree(t, mustUUID(t, DOC0)),
		DOC1: load_tree(t, mustUUID(t, DOC1)),
		DOC2: load_tree(t, mustUUID(t, DOC2)),
	}

	var hits atomic.Int32
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6) // bound concurrent LLM calls
	for i := range benchmark_cases {
		c := benchmark_cases[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			result, err := BeamSearch(context.Background(), trees[c.doc], c.query)
			switch {
			case err != nil:
				t.Logf("case %q: miss (error): %v", c.query, err)
			case strings.Contains(result.Node.Title, c.expected):
				hits.Add(1)
				t.Logf("case %q: HIT %q (score %.1f)", c.query, result.Node.Title, result.Score)
			default:
				t.Logf("case %q: miss got %q want substring %q", c.query, result.Node.Title, c.expected)
			}
		}()
	}
	wg.Wait()

	if got := int(hits.Load()); got < MIN_QUALITY_HITS {
		t.Errorf("quality gate: beam deep top-1 hit %d/%d cases, want ≥ %d", got, len(benchmark_cases), MIN_QUALITY_HITS)
	} else {
		t.Logf("quality gate: beam deep top-1 hit %d/%d cases", got, len(benchmark_cases))
	}
}

// TestBlockSearch_SmallDoc runs the block path end-to-end on a real seed
// doc (single vertical block) — verified stable result from the PoC runs.
func TestBlockSearch_SmallDoc(t *testing.T) {
	require_db(t)

	root := load_tree(t, mustUUID(t, DOC0))
	result, err := BlockSearch(context.Background(), root, "how was the markov model structured what health states")
	if err != nil {
		t.Fatalf("block search: %v", err)
	}
	if result.Node == nil || result.Score < 0 || result.Score > 10 {
		t.Fatalf("bad result: node=%v score=%.1f", result.Node, result.Score)
	}
	if result.LLMCalls < 1 || result.BlocksProcessed < 1 {
		t.Errorf("expected at least one processed block / LLM call, got blocks=%d calls=%d",
			result.BlocksProcessed, result.LLMCalls)
	}
	if !strings.Contains(result.Node.Title, "Model Structure") {
		t.Errorf("block top-1 = %q, want substring %q", result.Node.Title, "Model Structure")
	}
}

// TestBlockSearch_WideTree_Horizontal exercises horizontal packing on a
// synthetic over-wide tree (no DB needed, real LLM): one level whose token
// estimate exceeds BLOCK_MAX_TOKENS must be cut into a parallel block
// group and still return a sane top-1.
func TestBlockSearch_WideTree_Horizontal(t *testing.T) {
	require_llm(t) // synthetic tree — no DB needed

	root := synth_wide_tree(t)
	result, err := BlockSearch(context.Background(), root, "safety and toxicity profile")
	if err != nil {
		t.Fatalf("block search: %v", err)
	}
	if result.Node == nil {
		t.Fatal("no result node")
	}
	saw_horizontal := false
	for _, turn := range result.Trace {
		if turn.Kind == "horizontal" {
			saw_horizontal = true
		}
	}
	if !saw_horizontal {
		t.Errorf("expected a horizontal block group in trace, got %+v", result.Trace)
	}
}

// synth_wide_tree builds root + 60 wide children (long summaries so the
// level exceeds BLOCK_MAX_TOKENS) where every 10th child has 2 leaves.
func synth_wide_tree(t *testing.T) *models.Node {
	t.Helper()
	long_summary := strings.Repeat("该临床试验评估了联合免疫治疗方案在晚期肿瘤患者中的安全性与疗效终点，包括无进展生存、总生存和客观缓解率，以及三级以上不良事件的发生情况。", 8)
	root := &models.Node{ID: uuid.New(), Title: "SYNTHETIC WIDE DOC", Summary: "synthetic root"}
	for i := 0; i < 60; i++ {
		child := &models.Node{
			ID:        uuid.New(),
			Title:     fmt.Sprintf("SECTION %02d", i),
			Summary:   long_summary,
			PageStart: 1, PageEnd: 2,
		}
		if i%10 == 0 {
			for j := 0; j < 2; j++ {
				child.Nodes = append(child.Nodes, models.Node{
					ID: uuid.New(), Title: fmt.Sprintf("SECTION %02d LEAF %d", i, j),
					Summary: "leaf summary " + long_summary[:200], PageStart: 2, PageEnd: 3,
				})
			}
		}
		root.Nodes = append(root.Nodes, *child)
	}
	return root
}

// TestSearchDocumentNodes_Semantic: the service entry — single-doc
// retrieval should land on the specific section (Model Structure).
func TestSearchDocumentNodes_Semantic(t *testing.T) {
	require_db(t)

	row, err := SearchDocumentNodes(context.Background(),
		mustUUID(t, DOC0),
		"how was the markov model structured what health states")
	if err != nil {
		t.Fatalf("SearchDocumentNodes semantic: %v", err)
	}
	if !strings.Contains(row.Title, "Model Structure") {
		t.Errorf("top node = %q, want substring %q", row.Title, "Model Structure")
	}
	if row.Filename == "" {
		t.Error("filename not hydrated")
	}
	if row.Score < 0 || row.Score > 10 {
		t.Errorf("score %.1f outside 0-10", row.Score)
	}
	t.Logf("top: %s / %q score=%.1f", row.Filename, row.Title, row.Score)
}

// TestSearchNodes_BM25: the folder-scoped BM25 node search (GraphQL
// path) still works — cross-doc selection stays BM25's job.
func TestSearchNodes_BM25(t *testing.T) {
	require_db(t)

	doc, err := orm_document.GetDocumentByID(context.Background(), mustUUID(t, DOC0))
	if err != nil {
		t.Fatalf("load doc: %v", err)
	}
	if doc.FolderID == nil || *doc.FolderID == uuid.Nil {
		t.Fatal("DOC0 has no folder")
	}
	rows, err := orm_node.SearchNodes(context.Background(),
		"markov model", *doc.FolderID, true, nil, 5)
	if err != nil {
		t.Fatalf("SearchNodes bm25: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no BM25 results")
	}
	t.Logf("top: %q score=%.4f (of %d)", rows[0].Title, rows[0].Score, len(rows))
}
