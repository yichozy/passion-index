# Chat Service — Implementation Plan

Implements `docs/superpowers/specs/2026-09-04-chat-service-design.md`.
Three repos: **passion-index** (chat service + GraphQL), **hopebox**
(client renames/additions), **hope-mcp** (tool renames + the pending
thin-down #33, same sweep).

Grounded facts (verified 2026-09-04):

- hopebox tool loop: `llm.Chat(ctx, ChatKwargs{SystemPrompt, HistoryMsgs,
  UserPrompt, ModelName, FallbackModelName, Tools}, reason_chan,
  output_chan, event_chan) (*ChatResponse, error)` (hopebox/llm/chat.go:83)
- `tool_base.ToolDefinition{Definition FunctionDefinition, Function
  func(ctx, map[string]any) (*QueryResult, error), MaxCalls}` — the
  in-process tool contract
- passion-index internal calls available for tools: folder_service
  GetFolderTree, orm_document ListDocumentsByFolder/SearchDocumentsBm25,
  document_search_service SearchDocuments/SearchDocumentNodes,
  orm_node GetByDocID/GetByID, document_service GetDocumentNodesByPages/
  GetFigureImage, hopebox-style outline rendering (see passion_index_client
  outline.go as the reference shape to port into chat tools)
- gin already hosts GraphQL; no REST needed this phase

## Phase 1 — naming alignment + hope-mcp thin-down (hopebox, hope-mcp)

Precondition: push hopebox cbh-dev (fb676f4+), tag a release (e.g.
v0.0.1060), so hope-mcp can depend on it.

hopebox `passion_index_client/`:
1. `get_figure_image.go` → rename `GetFigureImage` to `GetDocumentImage`
   (file + tests)
2. `search_documents_keyword.go` → rename `SearchDocumentsKeyword` to
   `SearchDocuments` (file + tests)
3. NEW `browse_documents.go`: `BrowseDocuments(ctx, folderID *string,
   recursive bool, offset, limit int) (*BrowseResult, error)` over
   GetDocumentListByFolder (time-ordered) + child-folder hint via
   GetFolderTree(depth=1) when !recursive; types in types.go
4. README + tests updated; go test green; commit + push + tag

hope-mcp `passion_index_tools/` (the #33 thin-down, now justified):
1. Delete local client.go/json.go/outline.go(+tests)/types.go/status
   gating — delegate to hopebox passion_index_client
2. Tool files become thin: Input struct + mcp.AddTool + client call +
   json_result; names: `browse_documents` (new), `search_documents`
   (renamed), `get_document_image` (renamed); the rest unchanged
3. go.mod bumps hopebox to the new tag; register.go wiring unchanged
4. Manual MCP e2e re-run against local passion-index (tools/list shows
   the renamed surface; a semantic + keyword + browse + image round-trip)

Commit checkpoint per repo.

## Phase 2 — chat_service (passion-index)

`services/chat_service/` three files:

1. `prompt.go` — the adapted system prompt as a const (per spec §Prompt
   Adaptation; source reference `pageindex_prompt.txt` stays in repo)
2. `tools.go` — `chatTools() map[string]tool_base.ToolDefinition`:
   10 tools, MCP-aligned names, in-process backing calls; every
   document-derived string in tool results passes
   `llm_safety.Sanitize`; `search_sections` wraps
   `document_search_service.SearchDocumentNodes` (chat-only). Also
   maintains a per-call trace (docs/sections actually read) used for
   citation validation.
3. `chat.go` — `Chat(ctx, messages []models.ChatMessage, folder_id
   *uuid.UUID) (*models.ChatResult, error)`:
   - maps messages to `llm_types.Message` history, last user message as
     UserPrompt
   - model env `PASSION_INDEX_CHAT_MODEL` / `_FALLBACK_MODEL` (defaults
     DeepSeekV4Flash / DoubleSeedEvolving — same pattern as summary/
     optimize), timeout 300s
   - runs `llm.Chat` with Tools; channels: output/reason drained (v1
     non-streaming), events logged
   - parses `[filename p.X-Y]` cite tags from the answer, drops tags
     whose section was not in the tool trace, returns ChatResult
   - style: one entry function + pure helpers only where multiply
     called (repo convention)

## Phase 3 — GraphQL exposure

1. `graph/schema/chat.graphql`: ChatMessage input, ChatCitation,
   ChatResult, `Chat` mutation (per spec §GraphQL); `make gql`
2. Thin resolver calling chat_service.Chat; schema comments carry the
   blocking-call semantics (tens of seconds)

## Phase 4 — tests & verification

1. Unit: citation tag parser (valid/dangling/multi tags); tools registry
   names == the 9 MCP tool names + search_sections
2. Integration (local DB + real LLM, search_integration_test.go
   conventions, skip in -short): three scripted conversations per spec —
   (a) Opdivo semantic discovery → structure → cited answer;
   (b) "what documents do I have" → browse path;
   (c) NCT-id keyword escalation
3. Playground e2e: `mutation { Chat(messages: [...]) { answer citations
   { filename page_start page_end } } }` on seed docs
4. `go build ./... && go vet ./... && gofmt` in all three repos; hopebox
   and hope-mcp test suites green

## Risks / notes

- Phase 1 blocks on hopebox release; if tagging is deferred, hope-mcp
  can temporarily pin the pseudo-version of the pushed commit
- Tool-loop latency: integration tests cap at one conversation each to
  bound LLM spend
- `FunctionDefinition` schema details (parameters JSON schema) read from
  hopebox tool_base at implementation time; hopeclaw tool_pgidx_*.go has
  working examples of the Definition shape

## Reminders

- Task #33 (hope-mcp thin-down) closes inside Phase 1
- pageindex_prompt.txt stays in repo root as the reference source
