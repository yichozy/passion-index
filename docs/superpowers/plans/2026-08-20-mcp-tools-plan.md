# passion-index MCP Tools — Implementation Plan

Implements `docs/superpowers/specs/2026-08-20-mcp-tools-design.md`.
Two repos: **passion-index** (2 backend changes) and **hope-mcp**
(new `passion_index_tools/` family, 8 tools).

Grounded facts (verified 2026-08-20):

- gqlgen regen: `make gql` (repo root)
- OSS: services construct `aliyun.NewOss()` locally; `Oss.Bucket` is
  exported → `Bucket.GetObject(key)` streams bytes (no hopebox change
  needed); key format `passion-index/<docID>/images/<name>`
- hope-mcp registration point: `internal/app/server.go` — families
  registered directly (`pubmed_tools.Register(server, nil)`); convention:
  one tool per file, `mcp.AddTool` with name = file name, typed Input
  struct, `json_result` helper (see `pubmed_tools/json.go`)
- hope-mcp env loading: `internal/app.LoadEnv()` in main.go

## Phase 1 — passion-index backend

### 1a. Nullable folder_id (nil = whole library, query-side only)

Signature ripple: `uuid.UUID` → `*uuid.UUID` end-to-end.

1. `graph/schema/document.graphql`
   - `GetDocumentListByFolder(folder_id: UUID, …)` (drop `!`)
   - `SearchDocuments(query: String!, folder_id: UUID, …)` (drop `!`)
2. `make gql` (big `graph/generated.go` diff expected)
3. `graph/document.resolvers.go` — resolvers take `folderID *uuid.UUID`,
   pass through
4. `services/document_search_service/`
   - `search_documents.go`: `SearchDocuments(ctx, query, folder_id *uuid.UUID, …)`
   - `search_documents_semantic.go`: pass pointer through
5. ORM nil branches (folder CTE / scoping dropped, soft-delete filters
   stay; `recursive` ignored when nil):
   - `internal/orm_document/list_by_folder.go`
   - `internal/orm_document/search_by_bm25.go`
   - `internal/orm_node/search_by_vector.go`
6. Call sites: `services/document_search_service/search_integration_test.go`
   (`doc0_folder(t)` → `&folder_id`), scripts unaffected
7. Verify: `go build ./... && go vet ./...`, restart, curl —
   `GetDocumentListByFolder` without folder_id lists all docs;
   `SearchDocuments` without folder_id searches whole library;
   with folder_id behaves exactly as before

### 1b. New query GetFigureImage

1. `graph/schema/document.graphql`:
   `GetFigureImage(doc_id: UUID!, name: String!): Figure`
2. `make gql`
3. New `services/document_service/get_figure_image.go`:
   `GetFigureImage(ctx, doc_id, name) (*models.Figure, error)`
   - `orm_document.GetDocumentByID` (exists check)
   - `orm_node.GetByDocID` → scan rows' `Figures` for name match
     (page + caption source)
   - `aliyun.NewOss()` → `Bucket.GetObject("passion-index/<docID>/images/<name>")`
     → base64 into `Figure.Data`
   - not found (doc / figure / object) → typed error, resolver maps to
     null or error per gqlgen conventions
4. Thin resolver in `graph/document.resolvers.go`
5. Verify: `docs.sh tree <DOC_ID>` shows a figure name → curl
   `GetFigureImage` returns base64 (decode → valid JPEG bytes)

Commit checkpoint (user-triggered): Phase 1 complete.

## Phase 2 — hope-mcp scaffolding

New package `passion_index_tools/`:

| File | Contents |
|---|---|
| `register.go` | `Register(server *mcp.Server)` — builds client from env, registers all 8 |
| `client.go` | `Client{base_url}`, `NewClientFromEnv()` (`PASSION_INDEX_URL`, default `http://localhost:8900`); `Query(ctx, doc, vars, &out)` — POST `/query`, `errors[]` → error, data-null → `ErrNotFound` |
| `json.go` | `json_result` helper (mirror `pubmed_tools/json.go`) |
| `wait.go` | `wait_for_done(ctx, client, doc_id)` — poll `GetDocument{status}` every 5s, ≤120s; FAILED → error w/ `document.error`; timeout → return last status |
| `pages_spec.go` | `parse_pages_spec("5-10") ([]int, error)` — single/comma/range, 1-based, expansion cap 50 + unit test |
| `outline.go` | `render_outline(tree) (string, int)` — plain-text outline, titles + `p.X-Y` + `[node_id]`, numbered hierarchy + unit test on a synthetic tree |
| `types.go` | shared Input structs + response payloads |

Wire into `internal/app/server.go`: `passion_index_tools.Register(server, nil)`.

Verify: `go build ./... && go vet ./...`; `go test ./passion_index_tools/`
(pure units only).

## Phase 3 — the 8 tools (one file each)

Order = dependency comfort (simple → composite). Every file: Input
struct, `register_xxx`, description carrying the discipline (5-step
workflow, semantic vs keyword guidance, "widen recursive on zero hits",
">20 pages → structure first", status enum list, score incomparability).

1. `get_document.go` — fixed projection sans tree; `wait_for_completion?`
2. `get_folder_structure.go` — `GetFolderTree(folder_id?, depth=10)`;
   synthesize `{name:"root", folders:[…forest]}`; `total_folders`
3. `browse_documents.go` — time → `GetDocumentListByFolder`;
   relevance+query → `SearchDocuments(mode: SEMANTIC)` (score included);
   `folders[]` from `GetFolderTree(folder_id, 1)` when recursive=false;
   offset honored in time mode only (documented)
4. `search_documents.go` — `SearchDocuments(mode: KEYWORD)`, BM25 score
5. `get_document_structure.go` — tree → `render_outline`; `wait_for_completion?`
6. `get_page_content.go` — `parse_pages_spec` → `GetDocumentNodesByPages`;
   sections `{node_id, title, page_start, page_end, text, figures[]}`;
   `wait_for_completion?`
7. `get_section.go` — `GetDocumentNode` + shallow children
8. `get_figure_image.go` — `GetFigureImage` → `{name, page, caption, data}`

Commit checkpoint.

## Phase 4 — end-to-end verification

1. `make run` passion-index (seed docs in local paradedb);
   `go run .` hope-mcp (:8080, startup token per existing app flow)
2. Drive all 8 tools over MCP streamable HTTP (curl or small Go client):
   - folder structure (omit folder_id → synthesized root over whole library)
   - browse time / browse relevance **"Opdivo combination immunotherapy"**
     (synonym case: DOC0 recalled)
   - keyword **"NCT02425891"** (exact-code case)
   - structure → page_content "5-10" → section drill → figure base64
   - error paths: bad UUID (GraphQL error → tool error), unknown doc
     (not-found error), bad pages spec
3. Regression: passion-index GraphQL paths with folder_id set behave
   unchanged (scripts/docs.sh search still works)
4. `go build ./... && go vet ./...` both repos

## Notes / risks

- gqlgen regen noise in `graph/generated.go` — expected, review the
  resolver diff only
- nil-folder BM25/vector SQL: confirm paradedb still uses the score index
  without the folder CTE (EXPLAIN if results look wrong)
- base64 inflates images ~33%; page-crop figures are ~100-500KB — fine
  for MCP responses
- `passion-index` and `hope-mcp` are separate go modules (no go.work
  coupling) — hope-mcp talks HTTP only, no import of passion-index
