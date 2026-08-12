# Metadata Design — passion-index

## Context

Document model currently has 4 metadata fields (`DOI`, `Indication`, `Study`,
`LiteratureType`) that were added speculatively but never populated — no write
path (GraphQL doesn't expose them, pipeline doesn't fill them) and no read path
(filters exist in service/orm layers but resolver passes zero values).

Replace with a single free-form `Metadata` JSONB column. This lets clients attach
arbitrary key-value pairs at upload time and filter search results by JSONB
containment (`@>` operator).

## Decisions

| Decision | Choice |
|---|---|
| Metadata shape | Free-form JSONB (`map[string]any` in Go) |
| Filter location | `SearchDocuments` only |
| Filter semantics | Postgres `@>` containment (AND match on all key-value pairs) |
| Write path | `UploadDocument` only (optional parameter) |
| GraphQL representation | `scalar JSON` mapped to `graphql.Map` (`map[string]any`) |

## Data model

### Document model change

Remove:
```go
DOI            string         `gorm:"index" json:"doi"`
Indication     pq.StringArray `gorm:"type:text[]" json:"indication"`
Study          pq.StringArray `gorm:"type:text[]" json:"study"`
LiteratureType pq.StringArray `gorm:"type:text[]" json:"literature_type"`
```

Add:
```go
Metadata  datatypes.JSON  `gorm:"type:jsonb" json:"metadata,omitempty"`
```

`datatypes.JSON` from `gorm.io/datatypes` — a `[]byte` wrapper that handles
JSON (de)serialization for GORM. Requires `go get gorm.io/datatypes`.

Remove `"github.com/lib/pq"` import (no more `pq.StringArray`).

### Indexes

`internal/orm/create_document_indexes.go`:

Remove 3 GIN indexes on old text[] columns. Add 1 GIN index on metadata:

```sql
CREATE INDEX IF NOT EXISTS idx_documents_metadata ON documents USING GIN (metadata)
```

### Migration

GORM AutoMigrate adds the `metadata` column. Old columns (`doi`, `indication`,
`study`, `literature_type`) stay in DB but are unreferenced by code (GORM
AutoMigrate does not drop columns). Existing data is all empty, so no data
migration needed.

## GraphQL schema

### Scalar

`scalar.graphql` — add `scalar JSON`.

`gqlgen.yml` — add mapping:
```yaml
JSON:
  model:
    - github.com/99designs/gqlgen/graphql.Map
```

`graphql.Map` is `map[string]interface{}` — gqlgen handles JSON parsing and
validation at the scalar boundary.

### Document type

```graphql
type Document {
  id: UUID!
  filename: String!
  folder_id: UUID!
  folder: Folder
  status: DocStatus!
  page_count: Int
  error: String
  metadata: JSON
  created_at: Time!
  updated_at: Time!
  tree: TreeNode
}
```

### SearchDocuments

```graphql
SearchDocuments(
  query: String!,
  doc_ids: [UUID!],
  metadata: JSON,
  limit: Int = 10
): [SearchResult!]!
```

`metadata` is optional. When provided, search is scoped to documents whose
`metadata` JSONB column contains all the given key-value pairs (`@>` operator).

### UploadDocument

```graphql
UploadDocument(
  file: Upload!,
  folder_id: UUID!,
  metadata: JSON
): Document!
```

`metadata` is optional. When provided, stored as the document's `metadata`
JSONB column. No update path after upload (no separate mutation).

## Search/filter data flow

```
Client sends: metadata: {"indication": ["lung cancer"], "literature_type": ["review"]}
    ↓ gqlgen JSON scalar parses
Go: map[string]any{"indication": []any{"lung cancer"}, "literature_type": []any{"review"}}
    ↓ resolver → service → orm_node.Search
orm_node.Search: json.Marshal(metadata) → '{"indication":["lung cancer"],"literature_type":["review"]}'
    ↓ SQL
WHERE d.metadata @> '{"indication":["lung cancer"],"literature_type":["review"]}'::jsonb
    ↓ Postgres GIN index
Matching documents
```

### orm_node.Search signature change

```go
// Before
func Search(ctx, query string, doc_ids []uuid.UUID, doi string, indication, study, literature_type []string, limit int) ([]NodeWithScore, error)

// After
func Search(ctx, query string, doc_ids []uuid.UUID, metadata map[string]any, limit int) ([]NodeWithScore, error)
```

SQL condition changes from 4 separate `if` blocks (doi/indication/study/literature_type) to 1:

```go
if len(metadata) > 0 {
    metadataJSON, _ := json.Marshal(metadata)
    conditions = append(conditions, "d.metadata @> ?::jsonb")
    args = append(args, string(metadataJSON))
}
```

`pq` import removed from search.go.

## Upload data flow

```
Client sends: UploadDocument(file, folder_id, metadata: {"doi": "10.1234/abc"})
    ↓ gqlgen parses
Go: graphql.Upload, uuid.UUID, map[string]any
    ↓ resolver → document_service.UploadDocument
service: json.Marshal(metadata) → datatypes.JSON(metadataJSON)
    ↓ doc.Metadata = datatypes.JSON(metadataJSON)
    ↓ orm_document.Create(ctx, doc)
GORM → Postgres jsonb column
```

## Files to change

| File | Change |
|---|---|
| `models/document.go` | Delete 4 fields + `pq` import, add `Metadata datatypes.JSON` |
| `internal/orm/create_document_indexes.go` | Remove 3 old GIN indexes, add 1 metadata GIN |
| `internal/orm_node/search.go` | Signature: 4 filter params → 1 `metadata map[string]any`; SQL: `@>` operator |
| `services/document_service/search_documents.go` | Signature sync |
| `services/document_service/upload_document.go` | Add `metadata map[string]any` parameter |
| `graph/schema/document.graphql` | Document type: add `metadata: JSON`; SearchDocuments: add `metadata: JSON` arg; UploadDocument: add `metadata: JSON` arg |
| `graph/schema/scalar.graphql` | Add `scalar JSON` |
| `gqlgen.yml` | Add `JSON: graphql.Map` mapping |
| `graph/document.resolvers.go` | Resolver signatures follow schema changes |

## Not affected

- `ReSummarizeDocument` — no metadata involvement
- Folder domain — completely unchanged
- Pipeline (OCR / Structuring / Summary) — does not fill metadata
- `GetDocument` / `GetDocumentListByFolder` — `metadata` returns as a field on Document automatically (CopyObj handles `datatypes.JSON` → JSON scalar)

## Usage examples

### Upload with metadata

```graphql
mutation {
  UploadDocument(
    file: null,
    folder_id: "019ff5fc-...",
    metadata: {"doi": "10.1234/abc", "indication": ["lung cancer"], "author": "Smith"}
  ) {
    id filename status metadata
  }
}
```

### Search with metadata filter

```graphql
query {
  SearchDocuments(
    query: "immunotherapy",
    metadata: {"indication": ["lung cancer"], "literature_type": ["review"]}
  ) {
    doc_id filename score
  }
}
```

This matches documents where `metadata->'indication'` array contains "lung
cancer" AND `metadata->'literature_type'` array contains "review", then
full-text searches within that scope.
