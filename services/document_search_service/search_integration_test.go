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
	"github.com/yichozy/hopebox/fastembed"
	hopelog "github.com/yichozy/hopebox/log"
	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"
	"github.com/yichozy/passion-index/services/document_service"
)

// Seed documents (must stay ingested in the local DB — the benchmark docs).
const (
	DOC0 = "01a013ac-5507-7a39-a346-74fd69d4f8ae" // CheckMate227.pdf: nivolumab+ipilimumab cost-effectiveness (NSCLC)
	DOC1 = "01a013b2-a725-7065-ab2c-5f3788d3a358" // NCT00398138.pdf: WT1 peptide vaccine (AML)
	DOC2 = "01a01396-df0d-7b4e-a151-b68e0fd5e4d2" // NCT02425891.pdf: atezolizumab+nab-paclitaxel (TNBC)
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
	rows, err := orm_node.SearchNodesByBm25(context.Background(),
		"markov model", *doc.FolderID, true, nil, 5)
	if err != nil {
		t.Fatalf("SearchNodes bm25: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no BM25 results")
	}
	t.Logf("top: %q score=%.4f (of %d)", rows[0].Title, rows[0].Score, len(rows))
}

// require_fastembed skips when the embedding service is not configured or
// unreachable (it's a locally-deployed service, not always running).
func require_fastembed(t *testing.T) string {
	t.Helper()
	url := os.Getenv("FASTEMBED_URL")
	if url == "" {
		t.Skip("FASTEMBED_URL not configured")
	}
	if _, err := fastembed.NewClient(url).TextEmbedding(context.Background(), []string{"ping"}); err != nil {
		t.Skipf("fastembed unreachable: %v", err)
	}
	return url
}

// doc0_folder resolves DOC0's folder (searches are folder-scoped).
func doc0_folder(t *testing.T) uuid.UUID {
	t.Helper()
	doc, err := orm_document.GetDocumentByID(context.Background(), mustUUID(t, DOC0))
	if err != nil {
		t.Fatalf("load doc: %v", err)
	}
	if doc.FolderID == nil || *doc.FolderID == uuid.Nil {
		t.Fatal("DOC0 has no folder")
	}
	return *doc.FolderID
}

// TestSearchDocumentsSemantic_Synonym — the feature's raison d'être:
// "Opdivo" never appears in DOC0 (it says nivolumab / immunotherapy /
// checkpoint inhibitors), so BM25 cannot match it; the vector path must.
// Fixture: embeds DOC0 first (idempotent; existing docs carry no
// embeddings since the pipeline only embeds on upload).
func TestSearchDocumentsSemantic_Synonym(t *testing.T) {
	require_db(t)
	require_fastembed(t)

	if err := document_service.EmbedDocumentTree(context.Background(), mustUUID(t, DOC0)); err != nil {
		t.Fatalf("embed DOC0: %v", err)
	}

	folder_id := doc0_folder(t)
	rows, err := SearchDocumentsSemantic(context.Background(),
		"Opdivo combination immunotherapy adverse events",
		&folder_id, true, nil, 5)
	if err != nil {
		t.Fatalf("SearchDocumentsSemantic: %v", err)
	}
	found := false
	for i := range rows {
		t.Logf("#%d %s score=%.4f", i+1, rows[i].Filename, rows[i].Score)
		if rows[i].ID == mustUUID(t, DOC0) && rows[i].Score > 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("DOC0 not recalled for the synonym query — semantic search failed its purpose")
	}
}

// TestSearchDocumentsSemantic_ChineseQuery: the embedding model is
// English-only (bge-small-en-v1.5) yet must still recall the right doc
// for a Chinese query — verified empirically 2026-08-17 (out-of-distribution
// but working); this test guards that against model/service drift. If it
// starts failing, switching to a multilingual model (bge-m3) is the fix.
func TestSearchDocumentsSemantic_ChineseQuery(t *testing.T) {
	require_db(t)
	require_fastembed(t)

	// Fixture: DOC2 (atezolizumab TNBC — contains the PFS analysis).
	if err := document_service.EmbedDocumentTree(context.Background(), mustUUID(t, DOC2)); err != nil {
		t.Fatalf("embed DOC2: %v", err)
	}
	doc, err := orm_document.GetDocumentByID(context.Background(), mustUUID(t, DOC2))
	if err != nil || doc.FolderID == nil {
		t.Fatalf("load DOC2: %v", err)
	}

	for _, query := range []string{"无进展生存期结果", "progression-free survival results"} {
		rows, err := SearchDocumentsSemantic(context.Background(), query, doc.FolderID, true, nil, 5)
		if err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		if len(rows) == 0 || rows[0].ID != mustUUID(t, DOC2) {
			t.Errorf("query %q: top-1 is not DOC2 (got %d results)", query, len(rows))
			continue
		}
		t.Logf("query %q → top-1 %s score=%.4f", query, rows[0].Filename, rows[0].Score)
	}
}

// TestSearchDocumentsKeyword: the KEYWORD path (BM25 over doc-level
// text) keeps its original behavior.
func TestSearchDocumentsKeyword(t *testing.T) {
	require_db(t)

	folder_id := doc0_folder(t)
	rows, err := orm_document.SearchDocumentsBm25(context.Background(),
		"nivolumab cost effectiveness", &folder_id, true, nil, 5)
	if err != nil {
		t.Fatalf("SearchDocuments keyword: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no BM25 doc results")
	}
	if rows[0].ID != mustUUID(t, DOC0) {
		t.Errorf("top doc = %s (%s), want DOC0", rows[0].ID, rows[0].Filename)
	}
	t.Logf("top: %s score=%.4f (of %d)", rows[0].Filename, rows[0].Score, len(rows))
}
