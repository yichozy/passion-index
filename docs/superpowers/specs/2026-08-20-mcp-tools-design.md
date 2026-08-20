# passion-index MCP Tools — Design Spec

## Context

hopeclaw's agent talks to PageIndex cloud via 7 MCP-style tools
(`tool_pgidx_*.go`). passion-index replaces that backend, and hope-mcp
(the shared MCP server, official `modelcontextprotocol/go-sdk`) is where
the agent-facing tool surface lives. This spec defines that surface,
**functionally aligned with the official PageIndex MCP tools** (verified
against docs.pageindex.ai/js-sdk/mcp-tools, which mirrors the remote
server's tools 1:1, and the open-source VectifyAI/pageindex-mcp repo).

Alignment target = the official **discovery workflow**:

1. `get_folder_structure` — orientation: folder tree first
2. `browse_documents` — primary discovery: list by time or semantic relevance
3. `search_documents` — escalation: keyword search when browse missed
4. `get_document` — confirm processing status
5. `get_document_structure` + `get_page_content` — drill into content

Transport decision (settled earlier): MCP tools wrap passion-index's
GraphQL `/query` directly — no REST conversion. Per-tool fixed GraphQL
documents give free per-tool projections; `errors[]` → tool-error
translation is the only friction.

## Scope

### In scope

- 8 read-only MCP tools in hope-mcp `passion_index_tools/`
- GraphQL client + helpers (error translation, wait polling, pages-spec parser)
- passion-index side: nullable `folder_id` on 3 queries + new `GetFigureImage`

### Out of scope (deliberate)

- ❌ `process_document` / `remove_document` — MCP surface stays read-only;
  ingestion goes through passion-index's own pipeline/scripts
- ❌ `next_steps` response field — planned for the next round, not v1
- ❌ `part` pagination for very large outlines — our clinical PDFs have
  ~57-node trees; revisit if that grows
- ❌ LLM node retrieval (beam/block search) as an MCP tool — stays a
  service-layer method the agent composes later
- ❌ metadata JSONB filter param on search tools — backend supports it but
  v1 keeps the surface lean; folders scope suffices

## Tool Roster (8 tools)

All params snake_case. All `folder_id` params **optional — omit = whole
library** (virtual root, see Backend Changes). Keys are UUIDs (PageIndex
uses filename strings; UUIDs are unambiguous).

### 1. `get_folder_structure`

Orientation step. Calls `GetFolderTree(folder_id, depth)`.

| Param | Type | Default | Notes |
|---|---|---|---|
| folder_id | UUID? | whole library | subtree root |
| depth | int? | 10 | 1–10, max nesting to expand |

Returns: a **synthesized single root node** (name "root", null id)
wrapping the top-level forest — mirrors PageIndex's single-tree shape.
Each node: `{id, name, document_count, folder_count, folders[]}`.
Plus `total_folders`.

### 2. `browse_documents`

Primary document discovery, dual-mode like the official tool.

| Param | Type | Default | Notes |
|---|---|---|---|
| folder_id | UUID? | whole library | folder scope |
| recursive | bool? | false | true = flatten descendants |
| sort | "time"\|"relevance"? | "time" | relevance requires `query` |
| query | string? | — | natural language OK (semantic) |
| offset | int? | 0 | **time mode only** — semantic path has no offset |
| limit | int? | 10 | 1–50 |

Backend: time → `GetDocumentListByFolder` (ordered `created_at DESC`);
relevance → `SearchDocuments(mode: SEMANTIC)` (vector recall + DocScore).

Returns: `{folders[], documents[], total, next_offset, has_more}`.
- `folders[]` (child folders, when recursive=false) comes from a second
  call `GetFolderTree(folder_id, depth: 1)` — saves the agent a round-trip,
  mirroring the official response
- `documents[]`: `{doc_id, filename, title, description, status,
  created_at, folder_id}` (+ `score` in relevance mode; DocScore, higher =
  better, unbounded, **not comparable with keyword scores**)

### 3. `search_documents`

Keyword escalation path — call when `browse_documents(sort=relevance)`
missed a document you strongly believe exists.

| Param | Type | Default | Notes |
|---|---|---|---|
| query | string | required | exact keywords (codes, NCT IDs, acronyms) — NOT natural language |
| folder_id | UUID? | whole library | widen scope on zero hits |
| recursive | bool? | false | set true to widen |
| limit | int? | 10 | 1–50 |

Backend: `SearchDocuments(mode: KEYWORD)` — BM25 over filename + title +
description. Returns `{documents[]}` with BM25 `score`.

### 4. `get_document`

| Param | Type | Default |
|---|---|---|
| doc_id | UUID | required |
| wait_for_completion | bool? | false |

Backend: `GetDocument(id)` **without the tree field** (fixed projection).
Returns metadata: `{doc_id, filename, title, description, status,
page_count, error, created_at, updated_at, folder {id, name}}`.
Description lists the status enum: PENDING / OCR / STRUCTURING / SUMMARY /
EMBEDDING / DONE / FAILED (transient states are normal).

### 5. `get_document_structure`

| Param | Type | Default |
|---|---|---|
| doc_id | UUID | required |
| wait_for_completion | bool? | false |

Backend: `GetDocument(id) { status tree {…titles, pages, ids…} }`. The MCP
layer renders a **plain-text outline** (token-economical, like the
official tool — titles + page ranges only, NO summaries per user
decision), with node ids so the agent can jump straight to
`get_section`:

```
1. FINAL PROGRESSION-FREE SURVIVAL ANALYSIS (p.3-4) [019f…]
  1.1 Background (p.3) [019f…]
2. SAFETY (p.5-7) [019f…]
```

Returns `{doc_id, page_count, structure, node_count}`.

### 6. `get_page_content`

| Param | Type | Default | Notes |
|---|---|---|---|
| doc_id | UUID | required | |
| pages | string | required | spec: `"5"`, `"3,7,10"`, `"5-10"` (1-based) |
| wait_for_completion | bool? | false |

**Deviation from official**: returns **node-level sections** (our
differentiator) instead of page-level raw text — sections carry structure
and dedupe overlapping pages. Backend: `GetDocumentNodesByPages(doc_id,
pages: [Int!]!)`; the MCP layer parses the spec and caps expansion at 50
pages.

Returns `{doc_id, requested_pages, sections[]}` where each section:
`{node_id, title, page_start, page_end, text, figures[] {name, page,
caption}}` — figure names feed `get_figure_image`.

### 7. `get_section`

Our extension (PageIndex has no node-level read). Backend:
`GetDocumentNode(node_id)`.

Returns `{node_id, title, page_start, page_end, summary, text, figures[],
children[] {node_id, title, page_start, page_end}}` (children are shallow
titles for navigation).

### 8. `get_figure_image`

| Param | Type | Default |
|---|---|---|
| doc_id | UUID | required |
| image_name | string | required — from get_page_content figures |

Backend: **new GraphQL query** (see Backend Changes). Returns
`{name, page, caption, data}` where `data` is base64 — same delivery as
the official `get_document_image` and hopeclaw's existing tool. A URL is
useless to text-only agents; base64 lets multimodal agents inline it.

## Shared Conventions

- **Virtual root**: `folder_id` omitted = whole library, all documents.
  No root folder row (zero data migration; top-level folders stay
  `parent_id IS NULL`). If "upload to root" is ever needed, materialize
  a root row then.
- **wait_for_completion** (tools 4–6): handler polls `GetDocument.status`
  every 5s, max 120s. FAILED → tool error carrying `document.error`;
  timeout → return current metadata + status (not an error; the agent
  decides to retry).
- **Errors**: GraphQL `errors[]` → MCP tool error (first message).
  `data.X == null` without errors → "not found" tool error.
- **Descriptions carry the discipline**: the 5-step discovery workflow,
  semantic (meaning/synonyms/brand names, e.g. "Opdivo" finds
  "nivolumab") vs keyword (exact terms) guidance, "widen recursive on
  zero hits", and "for docs >20 pages read structure first".
- **Score semantics**: DocScore (semantic) and BM25 (keyword) are
  documented as mutually incomparable in tool descriptions.

## hope-mcp Implementation

```
hope-mcp/passion_index_tools/
  register.go                  — Register(server), registers all 8
  client.go                    — GraphQL POST client (env PASSION_INDEX_URL,
                                 default http://localhost:8900), errors[]
                                 translation
  wait.go                      — wait_for_done() poll helper
  pages_spec.go                — parse "5-10" → []int (pure, unit-tested)
  outline.go                   — tree → plain-text outline renderer (pure,
                                 unit-tested)
  get_folder_structure.go
  browse_documents.go
  search_documents.go
  get_document.go
  get_document_structure.go
  get_page_content.go
  get_section.go
  get_figure_image.go
```

Follows `pubmed_tools/` conventions exactly: one tool per file,
`mcp.AddTool` with name = file name, typed Input struct, JSON result.
Registered from `tools/register.go`. No name prefix — passion-index is
the only document family in hope-mcp.

## passion-index Backend Changes

### A. Nullable folder_id (nil = whole library)

Two GraphQL queries make `folder_id` optional; ORM nil-branch drops the
folder CTE / scoping (soft-delete filters stay). `recursive` is
meaningless when folder_id is nil.

- `GetDocumentListByFolder(folder_id: UUID, …)` → `ListDocumentsByFolder`
  nil branch
- `SearchDocuments(query, folder_id: UUID, …)` → both `SearchDocumentsBm25`
  and `SearchNodesByVector` (semantic path) nil branches

`SearchDocumentNodes` (node-level BM25) is not on the MCP surface and
stays folder-required.

`GetFolderTree(nil)` already returns the top-level forest — no change.

### B. New query: GetFigureImage

```graphql
GetFigureImage(doc_id: UUID!, name: String!): Figure
```

Resolver derives the OSS key `passion-index/<docID>/images/<name>`,
downloads the bytes (or presigns — whichever the hopebox OSS client
supports; verify at plan time), and returns `{name, page, caption,
data}`. Page/caption are looked up from the doc's nodes' figures by name.
This also finally implements the "populate Figure.Data on demand" idea
that models/figure.go declares but no resolver fulfills.

## Deviations from PageIndex (documented)

| # | Ours | Official | Why |
|---|---|---|---|
| 1 | UUID keys | docName strings | unambiguous |
| 2 | folder_id optional via virtual root | folderId optional via real "root" | zero data migration |
| 3 | page content = node sections | raw page text | our tree IS the index; sections dedupe and carry structure |
| 4 | figure = base64 via GraphQL | base64 | aligned; URL instead was rejected (useless to text agents) |
| 5 | scores: DocScore / BM25 raw | LLM rerank 6–10 | our pipeline; documented as incomparable |
| 6 | status enum = 7 pipeline states | completed/processing/… | our pipeline truth; transients listed in description |
| 7 | outline carries node ids | no ids | enables direct get_section without page round-trip |
| 8 | browse offset = time mode only | offset always | semantic path (DocScore over top-100 chunks) has no offset |
| 9 | get_section extra tool | — | node-level read is our differentiator |
| 10 | no next_steps / process / remove / parts | has them | read-only v1; next_steps next round |

## Verification

1. Unit (hope-mcp, no DB): pages_spec parser, outline renderer, errors[]
   translation via httptest, virtual-root nil handling in client
2. Manual e2e against local passion-index with the three seed docs:
   run the full discovery workflow (folder structure → browse time →
   browse relevance "Opdivo …" → keyword "NCT02425891" → structure →
   page content → section → figure)
3. Regression: passion-index GraphQL tests for the nil folder_id
   branches and GetFigureImage
4. `go build ./... && go vet ./...` in both repos

## Next Round (recorded, not v1)

- `next_steps` field on responses (official's agent-guidance pattern)
- LLM node retrieval (beam/block `SearchDocumentNodes`) as a composed
  agent capability — decide surface later
- metadata filter param on search tools
