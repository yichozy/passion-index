# SearchDocuments Design — passion-index

## Context

passion-index is replacing PageIndex as the document indexing backend for hopeclaw's agentic_agent_service. PageIndex provides 7 MCP tools; passion-index needs equivalent GraphQL/REST APIs. The only missing capability is **search_documents** — keyword search with relevance ranking. This spec covers implementing SearchDocuments on passion-index, plus a capability gap analysis against PageIndex.

## PageIndex → passion-index capability mapping

| PageIndex MCP tool | passion-index equivalent | Status |
|---|---|---|
| `browse_documents` | `GetDocumentList(limit, offset)` | ✅ Has (no folder/sort, acceptable for MVP) |
| `search_documents` | — | ❌ **This spec implements it** |
| `get_document` | `GetDocumentByID(doc_id)` | ✅ Has |
| `get_document_structure` | `GetDocumentByID.tree` (full tree in one call) | ✅ Has |
| `get_page_content` | `GetDocumentNodesByPages(doc_id, pages)` | ✅ Has |
| `get_document_image` | `GET /documents/:docID/images/:name` | ✅ Has (302 to OSS) |
| `remove_document` | `DeleteDocument(doc_id)` | ✅ Has (soft delete) |
| `process_document` | `UploadDocument(file)` | ✅ Has (async pipeline) |

After implementing SearchDocuments, passion-index covers all PageIndex capabilities.

## Search implementation: PG tsvector

### Why tsvector (not BM25/pg_search)

- PG built-in, zero dependencies
- Aliyun RDS PG supports it natively
- `ts_rank` provides relevance scoring (TF-based, close enough for MVP scale)
- API designed to be implementation-agnostic — can swap to pg_search later by changing SQL only

### Data model change

Add a generated tsvector column + GIN index to the `nodes` table:

```sql
ALTER TABLE nodes ADD COLUMN search_vector tsvector
  GENERATED ALWAYS AS (
    setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
    setweight(to_tsvector('english', coalesce(summary, '')), 'B') ||
    setweight(to_tsvector('english', coalesce(text, '')), 'C')
  ) STORED;

CREATE INDEX idx_nodes_search ON nodes USING GIN (search_vector);
```

**Weighting**: title matches rank highest (A), summary second (B), text lowest (C). This mirrors how a human would judge relevance — a keyword in the title is more relevant than one buried in body text.

**Pipeline impact**: Zero. `GENERATED ALWAYS AS ... STORED` means PG auto-computes on insert/update. The pipeline code doesn't change.

**Migration**: Raw SQL in `internal/orm/migrate.go`, executed after AutoMigrate. gorm can't express generated columns via struct tags.

## GraphQL API

```graphql
type SearchMatch {
  node_id: Int!
  title: String!
  summary: String!
  score: Float!
}

type SearchResult {
  doc_id: ID!
  filename: String!
  score: Float!
  matches: [SearchMatch!]!
}

extend type Query {
  SearchDocuments(
    query: String!
    doc_ids: [ID!]
    limit: Int = 10
    include_matches: Boolean = false
  ): [SearchResult!]!
}
```

### Behavior

- **Default** (`include_matches=false`): Returns document-level results. SQL groups by `doc_id`, takes `MAX(score)`. Matches PageIndex's `search_documents` return shape (document list + score).
- **Expanded** (`include_matches=true`): Each result includes `matches[]` — the individual nodes that matched, with their own scores. Lets the agent jump directly to the matching section.

### Parameters

| Param | Type | Required | Default | Description |
|---|---|---|---|---|
| `query` | `String!` | Yes | — | Keywords (AND-matched via `plainto_tsquery`) |
| `doc_ids` | `[ID!]` | No | null (all docs) | Limit search to specific documents |
| `limit` | `Int` | No | 10 | Max results |
| `include_matches` | `Boolean` | No | false | Include node-level match details |

## Implementation files

| File | Action | Description |
|---|---|---|
| `internal/orm/migrate.go` | Modify | Add tsvector column + GIN index SQL after AutoMigrate |
| `internal/orm_node/search.go` | Create | `Search(ctx, query, docIDs, limit) → []Node with score` |
| `services/document_service/search_documents.go` | Create | Aggregate nodes → SearchResult (group by doc_id) |
| `graph/schema/document.graphql` | Modify | Add SearchResult, SearchMatch types + SearchDocuments query |
| `graph/document.resolvers.go` | Modify | Add SearchDocuments resolver |
| `scripts/search.sh` | Create | Test script: `./scripts/search.sh "lung cancer treatment"` |

### SQL queries

**Default mode (document-level)**:
```sql
SELECT doc_id, MAX(ts_rank(search_vector, plainto_tsquery('english', $1))) as score
FROM nodes
WHERE search_vector @@ plainto_tsquery('english', $1)
  AND ($2::uuid[] IS NULL OR doc_id = ANY($2::uuid[]))
GROUP BY doc_id
ORDER BY score DESC
LIMIT $3
```

**Expanded mode (node-level, then aggregate in Go)**:
```sql
SELECT doc_id, id, title, summary, text,
       ts_rank(search_vector, plainto_tsquery('english', $1)) as score
FROM nodes
WHERE search_vector @@ plainto_tsquery('english', $1)
  AND ($2::uuid[] IS NULL OR doc_id = ANY($2::uuid[]))
ORDER BY score DESC
LIMIT $3
```

Go layer groups results by `doc_id`, assigns `MAX(score)` to the document, and populates `matches[]`.

## hopeclaw adaptation guide (reference only — not implemented in this spec)

When hopeclaw is ready to switch from PageIndex to passion-index:

1. Create `services/passion_index_client/client.go` — HTTP GraphQL client
2. Create `tool_pidx_search_documents.go` — calls passion-index `SearchDocuments`
3. Create `tool_pidx_browse_documents.go` — calls passion-index `GetDocumentList`
4. Create `tool_pidx_get_document.go` — calls passion-index `GetDocumentByID`
5. Create `tool_pidx_get_page_content.go` — calls passion-index `GetDocumentNodesByPages`
6. Create `tool_pidx_get_document_image.go` — calls passion-index REST image endpoint
7. Create `tool_pidx_remove_document.go` — calls passion-index `DeleteDocument`
8. Create `tool_pidx_collection.go` — factory that returns all tools

Config: `PASSION_INDEX_URL` replaces `PAGE_INDEX_MCP_URL` + `PAGE_INDEX_API_KEY`.

## Verification

1. Upload a PDF, wait for DONE
2. `./scripts/search.sh "keyword from the paper"`
3. Verify: returns the document with score > 0
4. `./scripts/search.sh "keyword" --matches` (or via GraphiQL with `include_matches: true`)
5. Verify: `matches[]` contains the specific node that has the keyword
6. Search with `doc_ids` filter — verify only searches within specified docs
7. Search for non-existent keyword — returns empty array (not error)
