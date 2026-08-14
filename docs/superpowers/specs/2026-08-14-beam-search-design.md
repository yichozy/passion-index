# Beam Search PoC — Design Spec

## Context

passion-index replaces PageIndex cloud for the hopeclaw agent. PageIndex's
`browseDocuments(sort=relevance)` does LLM-based semantic retrieval — its core
algorithm is **Beam Search over document trees**, which vectifyai open-sourced
as `ConDB`. passion-index currently uses BM25 (paradedb `pg_search`) as the
recall + ranking mechanism. BM25 misses queries with synonyms, implicit
relationships, or semantic intent ("nivolumab 副作用" should match sections
mentioning "Opdivo" or "anti-PD-1 adverse events" — BM25 cannot).

This spec covers **only the algorithm** (`beam_search.go`): a Go port of
ConDB's `beam_retriever.py`, scoped to single-document retrieval. Multi-doc
orchestration, GraphQL endpoint integration, and SearchDocuments wrapping are
**out of scope** and will be covered by follow-up specs.

ConDB defaults: `beam_size=3`, `max_turns=5`, `select_k=1`. We mirror these.

## Scope

### In scope

- `services/document_service/beam_search.go` — Beam Search algorithm
- `services/document_service/beam_prompt.go` — prompt template + builder
- `services/document_service/beam_search_test.go` — unit tests with mock LLM
- `cmd/beam-demo/main.go` — CLI for manual smoke testing

### Out of scope (YAGNI for PoC)

- ❌ GraphQL endpoint changes (SearchDocuments integration is a follow-up spec)
- ❌ Multi-doc orchestration (folder-level candidate selection, parallelism)
- ❌ KV-cache prefix reuse (deferred alongside Block retrieval)
- ❌ Block retrieval for large docs (passion-index papers are 10-30 nodes)
- ❌ Filesystem mode (passion-index doesn't index directory trees)
- ❌ Caching layer (per-request only, no cross-request cache)
- ❌ Tool use / function calling (JSON output is simpler)
- ❌ Jinja2 templates (use `strings.NewReplacer` to match summary prompts)

## Architecture

### Module layout

```
services/document_service/
├── beam_search.go          ← types + BeamSearch func + llmBeamRanker
├── beam_prompt.go          ← BEAM_RANK_PROMPT const + buildBeamPrompt
└── beam_search_test.go     ← 8 unit tests with mock beamRanker

cmd/beam-demo/
└── main.go                 ← one-shot CLI: load doc → run Beam → print trace
```

### Boundary

`beam_search.go` is a **pure algorithm**: input is `*models.Node` tree +
query + LLM client interface, output is `*BeamResult`. No DB access, no
GraphQL awareness. This makes it trivial to unit test.

The LLM call is abstracted behind a `beamRanker` interface so tests can inject
mocks without touching real LLM endpoints.

`maxTurns` is derived from the tree itself (max depth) — not a function
parameter. This avoids the caller needing to know tree structure.

## Data flow

```
BeamSearch(ctx, root, query, ranker)
  ↓
1. Init frontier = [{Node: root, ParentSummary: ""}]
  ↓
2. Loop (up to maxTurns):
   a. Expand frontier → candidates
      - Each frame's children become candidates
      - Leaf frames (no children) become candidates with IsLeaf=true
   b. If all frontier are leaves → pick first, stop (early termination)
   c. If no candidates → stop (defensive)
   d. LLM call: ranker.Rank(query, candidates, pickLimit=3)
      → returns (rankedIDs, done)
   e. Walk rankedIDs:
      - First leaf encountered → becomes topCandidate (locked)
      - Non-leaf top-beamSize → next frontier
   f. If done=true OR topCandidate != nil → break
   g. frontier = nextFrontier
  ↓
3. Return BeamResult{Node: topCandidate, Trace: turns}
   - If topCandidate still nil → return ErrBeamNoConverge
```

## Types

```go
type BeamCandidate struct {
    Node          *models.Node
    IsLeaf        bool     // len(Nodes) == 0
    Path          string   // "Chapter 1 > Section 1.2 > ..." titles joined
    ParentSummary string   // parent's summary for LLM context
}

type BeamTurn struct {
    Depth      int
    Candidates []BeamCandidate
    RankedIDs  []uuid.UUID  // full LLM ranking
    PickedIDs  []uuid.UUID  // top-pickLimit
    Done       bool         // LLM's done flag
}

type BeamResult struct {
    Node  *models.Node   // top-1 leaf (select_k=1)
    Trace []BeamTurn     // per-turn decisions, debugging
}

type BeamFrame struct {
    Node          *models.Node
    ParentSummary string
}

// beamRanker abstracts LLM ranking so the algorithm is unit-testable.
type beamRanker interface {
    Rank(ctx context.Context, query string, candidates []BeamCandidate, pickLimit int) (rankedIDs []uuid.UUID, done bool, err error)
}
```

## Prompt design

Translated from ConDB's `contextdb/prompts/beam.jinja`. Uses
`strings.NewReplacer` (same pattern as existing summary prompts in
`summarize_document_tree.go`).

```go
const BEAM_RANK_PROMPT = `You are ranking document tree nodes to find the most relevant section for a user question.

Query: {{ query }}

Candidates (rank up to {{ pick_limit }} ids, best first):
{{ candidates_block }}

Reply strictly in the following JSON format:
{
    "ranked_ids": ["<uuid1>", "<uuid2>", ...],
    "done": <true if reached leaf nodes or content specific enough to answer; false if high-level sections needing deeper exploration>
}

Follow strictly the above JSON return format. Do not include any other text!`
```

`candidates_block` is built per call, one block per candidate:

```
- id: 426848b4-3180-44fe-9070-ac330ff23768
  title: Nivolumab Safety Profile
  summary: 3-4 级不良事件发生率约 30%...
  text: The most common adverse events were fatigue...   (first 200 chars)
  range: p5-6
  path: RESULTS > Safety Analyses > Nivolumab Safety Profile
  parent_context: <parent summary first 300 chars, "(root)" if no parent>
```

### LLM client

Production implementation uses `hopebox/llm.LLMChatWithStructOutput[reply]`
(matching the summary-pipeline pattern):

```go
type llmBeamRanker struct {
    model    string         // from PASSION_INDEX_SUMMARY_MODEL env
    fallback string         // from PASSION_INDEX_SUMMARY_FALLBACK_MODEL env
    timeout  time.Duration  // BEAM_TIMEOUT = 60s
}

type beamReply struct {
    RankedIDs []string `json:"ranked_ids" jsonschema:"schema_description=IDs of candidate nodes in best-to-worst order, max pick_limit"`
    Done      bool     `json:"done" jsonschema:"schema_description=true if reached leaf nodes or content specific enough to answer; false if high-level sections needing deeper exploration"`
}
```

UUIDs are passed as `[]string` (JSON schema's UUID support is uneven); the
ranker parses them to `uuid.UUID` and silently skips any that fail to parse
(LLMs occasionally return malformed IDs — non-fatal as long as at least one
valid ID survives).

## Constants

```go
const (
    BEAM_SIZE          = 3       // ConDB default
    BEAM_SELECT_K      = 1       // top-1 result
    BEAM_TIMEOUT       = 60 * time.Second
    BEAM_TEXT_TRUNCATE = 200     // chars of node.Text included in prompt
    BEAM_PARENT_TRUNCATE = 300   // chars of parent summary in prompt
)
```

`maxTurns` is **not a constant** — it's computed at runtime as the tree's max
depth (`models.Node` doesn't carry depth explicitly; we walk once to find max
depth). This is consistent with ConDB's `max_turns: None` default that
resolves to tree depth. A safety cap (e.g., hard limit of 10) can be added
later if pathological trees show up.

## Error handling

Sentinel errors let callers distinguish failure modes when integrating
(future spec's SearchDocuments wrapper will degrade to BM25 on beam failure):

```go
var (
    ErrBeamNoConverge   = errors.New("beam search did not converge")
    ErrBeamNoCandidates = errors.New("beam search: no candidates at frontier")
)
```

| Scenario | Behavior |
|---|---|
| LLM call fails (network / rate limit / timeout) | Return error |
| LLM returns empty `ranked_ids` | Return error (LLM should always rank) |
| LLM returns invalid UUID | Silently skip that ID |
| All UUIDs invalid | Return error (no usable ranking) |
| Tree has only root (no children) | Return root as topCandidate |
| `maxTurns` exhausted without finding leaf | Return `ErrBeamNoConverge` |
| LLM says `done=true` mid-tree | Stop immediately, pick `ranked[0]` if no leaf yet |
| `ctx.Done()` | Return `ctx.Err()` |

## Testing

### Unit tests (`beam_search_test.go`)

All tests use a mock `beamRanker` — no real LLM calls. 8 cases:

```
TestBeamSearch_HappyPath_3LevelTree
  Tree:  root → [A, B, C] → each has leaves
  Mock:  turn 0 rank [B] done=false; turn 1 rank [leaf_B1] done=true
  Expect: result = leaf_B1, trace has 2 turns

TestBeamSearch_SingleLevelTree
  Tree:  root → [leaf1, leaf2] (no deeper)
  Mock:  turn 0 rank [leaf1, leaf2] done=true
  Expect: result = leaf1, trace has 1 turn

TestBeamSearch_LLMFails
  Mock:  returns error
  Expect: BeamSearch returns same error

TestBeamSearch_LLMReturnsInvalidUUID
  Mock:  returns ["valid-uuid", "garbage"]
  Expect: "garbage" skipped, valid-uuid kept

TestBeamSearch_DidNotConverge
  Mock:  returns rankedIDs of all-invalid UUIDs at the leaf level
         (real candidates are leaves, but LLM hallucinates IDs)
  Expect: ErrBeamNoConverge

TestBeamSearch_RootOnly
  Tree:  just root (no children)
  Expect: result = root (allLeaves frontier triggers early pick)

TestBeamSearch_LLMSaysDoneEarly
  Mock:  turn 0 returns done=true, ranked has non-leaf only
  Expect: result = ranked[0] (best non-leaf so far; we trust LLM that
          the section content is already specific enough)

TestBuildBeamPrompt
  Input: query + 2 candidates
  Expect: prompt string contains both candidate titles, the query,
          and the literal "{{ pick_limit }}" replaced
```

### Prompt builder unit test (`beam_prompt_test.go`)

`TestBuildBeamPrompt` — verify the `strings.NewReplacer` substitution works
and the candidates block is formatted correctly.

### Manual smoke test (`cmd/beam-demo/main.go`)

CLI to validate end-to-end with a real doc + real LLM:

```bash
go run ./cmd/beam-demo \
    -doc-id 019ffaa6-9ba8-77cd-abb4-4c14eb7b2a31 \
    -query "atezolizumab 副作用数据"
```

Loads doc via `orm_node.GetByDocID` + `models.AssembleTree`, runs
`BeamSearch` with the production `llmBeamRanker`, prints result + trace +
latency + token usage. Not gated by tests — manual verification only.

### Quality spot check (manual)

Pick one doc (e.g., `NCT02425891.pdf` already uploaded). Run 5 queries that
have an "obviously correct" answer a human can identify. Compare Beam top-1
to BM25 top-1. PoC is "successful" if Beam wins on at least 3 of 5.

Example query matrix:

| Query | Expected section |
|---|---|
| "atezolizumab adverse events" | Safety Analyses |
| "trial design" | METHODS / Trial Design |
| "PD-L1 positive subgroup results" | RESULTS / Subgroup |
| "overall survival data" | RESULTS / Overall Survival |
| "patient demographics" | METHODS / Patient Characteristics |

## Verification checklist

- [ ] `go build ./...` passes
- [ ] `go vet ./...` passes
- [ ] 8 unit tests pass
- [ ] `cmd/beam-demo` loads a real doc without error
- [ ] 5 manual queries return reasonable top-1 (matches human expectation ≥ 3/5)
- [ ] Latency per single-doc Beam Search < 15s on typical 10-30 node paper

If quality is poor → revisit Section "Constants" (beam_size, select_k, prompt).
If quality is good → follow-up spec will integrate into `SearchDocuments`.

## What this spec deliberately does NOT do

To keep PoC scope minimal and validate the algorithm before building
infrastructure around it, this spec explicitly defers:

1. **No GraphQL endpoint changes** — `SearchDocuments` stays BM25-only until
   the algorithm proves itself
2. **No multi-doc orchestration** — caller of `BeamSearch` is responsible for
   picking which doc(s) to run it on; `cmd/beam-demo` hardcodes single doc
3. **No production caller** — only `cmd/beam-demo` calls `BeamSearch`. Service
   layer doesn't depend on it yet
4. **No benchmarks** — algorithm is O(tree-node-count), not hot path; LLM
   latency dominates anyway
5. **No real-LLM tests** — integration tests requiring LLM API keys are
   manual-only via `cmd/beam-demo`

## References

- ConDB source (algorithm reference):
  `/Users/binhuchen/workspace/vectifyai/ConDB/contextdb/retriever/algorithm/beam_retriever.py`
- ConDB prompt reference:
  `/Users/binhuchen/workspace/vectifyai/ConDB/contextdb/prompts/beam.jinja`
- ConDB defaults:
  `/Users/binhuchen/workspace/vectifyai/ConDB/contextdb/config/retriever/beam.yaml`
  (`beam_size: 3`, `max_turns: 5`, `select_k: 1`)
- passion-index summary prompts (pattern reference for `strings.NewReplacer`):
  `services/document_service/summarize_document_tree.go`
