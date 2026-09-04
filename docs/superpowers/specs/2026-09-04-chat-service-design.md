# passion-index Chat Service — Design Spec

## Context

`pageindex_prompt.txt` (repo root) is PageIndex cloud's chat system
prompt, obtained from their live service. It encodes a polished
document-QA workflow: a three-step discovery funnel (folder structure →
browse → keyword escalation), a persistence protocol before concluding
"not found", reading discipline (structure first for >20 pages), and
citation rules. The OSS repo confirms the architecture: chat is a thin
tool-loop orchestrator layered over the same 7 agent tools the MCP
surface exposes (`local_chat.py` + `agent_tools.py`).

This spec builds the equivalent **into passion-index itself**: a
document-QA chat that runs an LLM tool loop over in-process service
calls, so the server-side retrieval that PageIndex lacks (beam/block
tree search) becomes a first-class tool.

## Decisions (2026-09-04)

- **Home**: `services/chat_service/` in passion-index. Not hope-mcp
  (one-shot tool results, no chat semantics), not hopebox client (would
  loop back over HTTP and can't reach beam search).
- **Architecture**: tool loop via hopebox `llm.Chat` — mirrors PageIndex
  chat; the prompt's workflow discipline only lives meaningfully in a
  tool loop.
- **Naming**: fully aligned with PageIndex, EXCEPT `browse_documents`
  has no `sort` dual-mode (plain time listing; semantic search stays a
  standalone tool). This supersedes the earlier "two search tools"
  split: the pair becomes `browse_documents` (list) +
  `search_documents_semantic` (semantic) + `search_documents` (keyword,
  renamed from `search_documents_keyword`).
- **Exposure**: GraphQL `Chat` mutation, non-streaming. v1 consumers
  are agents that want one blocking call with the final answer.
  Streaming (gin SSE route or gqlgen subscription) deferred until a UI
  needs it; SSE is the cheaper v2 path.

## Tool Surface (10 chat tools: 9 shared with MCP + 1 chat-only)

| Tool | Change vs today | Backing |
|---|---|---|
| `get_folder_structure` | — | GetFolderTree service |
| `browse_documents(folder_id?, recursive?, offset?, limit?)` | NEW — plain list, no sort | GetDocumentListByFolder |
| `search_documents_semantic(query, folder_id?, recursive?, limit?)` | — | SearchDocuments SEMANTIC |
| `search_documents(query, folder_id?, recursive?, limit?)` | RENAMED from search_documents_keyword | SearchDocuments KEYWORD |
| `get_document(doc_id)` | — | GetDocument |
| `get_document_structure(doc_id)` | — | GetDocument + outline render |
| `get_page_content(doc_id, pages)` | — | GetDocumentNodesByPages |
| `get_section(node_id)` | — (ours alone) | GetDocumentNode |
| `get_document_image(doc_id, image_name)` | RENAMED from get_figure_image | GetFigureImage |
| `search_sections(doc_id, query)` | NEW — chat-only: beam/block tree retrieval, the server-side retrieval PageIndex doesn't have | document_search_service.SearchDocumentNodes |

Renames ripple to hope-mcp tool names and hopebox client methods
(`GetFigureImage→GetDocumentImage`, `SearchDocumentsKeyword→SearchDocuments`,
new `BrowseDocuments`). That hope-mcp pass should also pick up the
still-pending thinning task (#33) in the same sweep.

## Structure

```
services/chat_service/
  chat.go     Chat(ctx, messages []ChatMessage, folder_id?) (*ChatResult, error)
              — llm.Chat tool loop; reason/output channels discarded in v1
                (non-streaming); event channel drained for logging
  tools.go    tool_base.ToolDefinition registry over in-process calls;
              every document-derived string passes llm_safety.Sanitize
              before entering tool results
  prompt.go   the adapted system prompt (below)
graph/schema/chat.graphql + resolver — Chat mutation
```

## Prompt Adaptation (per section of pageindex_prompt.txt)

Keep as-is: READING WORKFLOW (>20 pages → structure first), the
three-step funnel shape, PERSISTENCE protocol (all five steps before
"not found"), DECISION tree, STYLE (concise, no emojis, lead with the
answer, reply in user's language).

Adapt:
- `browse_documents(sort="relevance", query)` → `search_documents_semantic(query)`
- keyword escalation target → `search_documents` (BM25; scores not
  comparable with DocScore)
- CITATIONS: no block_id in our data — cite as `[filename p.X-Y]`, at
  most one tag per supporting section; facts from images cite the
  section carrying the figure
- add short paragraphs: `get_section` (drill by node id from outlines /
  page results) and `search_sections` (meaning-based section search
  inside one document — strongest retrieval, prefer it when the target
  document is known)

Remove: read-only folders, `web_search`, `next_steps` on errors, the
`block_id` machinery, `page_images` full-page rendering.

## GraphQL

```graphql
input ChatMessage { role: String!  content: String! }
type ChatCitation { filename: String!  page_start: Int!  page_end: Int!  node_id: UUID  snippet: String }
type ChatResult  { answer: String!  citations: [ChatCitation!]! }
extend type Mutation {
  # Blocking document-QA call; runs the tool loop server-side (tens of
  # seconds with retrieval). folder_id scopes the whole conversation.
  Chat(messages: [ChatMessage!]!, folder_id: UUID): ChatResult!
}
```

Citations are parsed from the answer's cite tags (`[filename p.X-Y]`) —
the tool loop's used-tools trace (docs/sections actually read) is the
authoritative source; tags referencing unread sections are dropped.

## v1 Boundaries (not doing)

- No streaming (mutation only); no server-side session store (client
  passes full history); no doc-targeting allowlists (local_chat's
  scoped doc_id); no cross-document comparison orchestration; no
  figure/image answering beyond captions (multimodal answer model out
  of scope).

## Risks

- Long-blocking mutation (tool loop + several LLM rounds): client
  timeouts must be generous; future ingress/gateway timeout config must
  allow ≥ 300s
- LLM nondeterminism: acceptance tests assert workflow behaviors
  (citations present, escalation happened), not exact answers
- Prompt cost: every chat turn re-sends the system prompt; acceptable
  at current scale, revisit with KV-cache-friendly tool ordering later

## Verification

1. Unit: citation tag parser; tool registry names/arity vs MCP surface
2. Integration (local DB + real LLM, `search_integration_test.go`
   conventions): three scripted conversations over seed docs —
   (a) "Opdivo 组合免疫疗法的成本效果结论" → discovers CheckMate227 via
   semantic, reads structure, answers with citations;
   (b) "列出我的文档" → browse path, no retrieval;
   (c) a keyword-escalation case (NCT id)
3. GraphQL e2e via playground/scripts; hope-mcp rename sweep + its
   passion_index_tools tests stay green
4. `go build ./... && go vet ./...` in passion-index and hope-mcp;
   hopebox client tests green
