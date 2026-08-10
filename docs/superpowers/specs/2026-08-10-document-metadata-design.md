# Document Metadata Design — passion-index

## Context

Documents need key-value metadata (e.g., `{"category": "medical", "source": "pubmed"}`). Metadata is set at upload time and used as a filter in SearchDocuments.

## Design

### Data model

Add `metadata jsonb` column to `documents` table + GIN index:

```go
// models/document.go
type Document struct {
    BaseUUIDModel
    Filename  string         `gorm:"not null" json:"filename"`
    FileKey   string         `gorm:"not null" json:"file_key"`
    Status    string         `gorm:"index;not null" json:"status"`
    PageCount int            `json:"page_count"`
    Error     string         `json:"error"`
    Metadata  datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"metadata"` // ← new
}
```

GIN index via migrate.go raw SQL (same pattern as search_vector):
```sql
CREATE INDEX IF NOT EXISTS idx_documents_metadata ON documents USING GIN (metadata);
```

AutoMigrate handles adding the column; raw SQL adds the index.

### GraphQL

Define a `JSON` scalar (gqlgen custom scalar → `map[string]any` in Go).

```graphql
scalar JSON

type Document {
  ...
  metadata: JSON!
}

mutation {
  UploadDocument(file: Upload!, metadata: JSON): Document!
}

query {
  SearchDocuments(
    query: String!
    doc_ids: [ID!]
    metadata_filter: JSON    # e.g., {"category": "medical"}
    limit: Int = 20
  ): [SearchResult!]!
}
```

### SearchDocuments with metadata_filter

Two-query approach (simple, uses GIN index on both):

1. If `metadata_filter` is non-empty: `SELECT id FROM documents WHERE metadata @> ?` → get matching doc_ids
2. Intersect with explicit `doc_ids` parameter if also provided
3. Pass combined doc_ids to `orm_node.Search`

Both filters are AND-combined. Either can be omitted independently.

### Files to modify

| File | Change |
|---|---|
| `models/document.go` | Add `Metadata` field |
| `internal/orm/migrate.go` | Add GIN index on metadata |
| `graph/schema/document.graphql` | Add `scalar JSON`, Document.metadata, UploadDocument.metadata, SearchDocuments.metadata_filter |
| `graph/json_scalar.go` | New: gqlgen JSON scalar implementation (MarshalJSON/UnmarshalJSON) |
| `services/document_service/upload_document.go` | Accept metadata parameter, write to Document |
| `internal/orm_document/get_by_metadata.go` | New: `GetDocIDsByMetadata(ctx, filter) → []string` |
| `services/document_service/search_documents.go` | Accept metadata_filter, intersect with doc_ids |
| `graph/document.resolvers.go` | Update UploadDocument + SearchDocuments resolvers |
| `scripts/upload.sh` | Support `--metadata '{"k":"v"}'` |
| `scripts/search.sh` | Support `--metadata '{"k":"v"}'` |

### Verification

1. Upload with metadata: `./scripts/upload.sh paper.pdf --metadata '{"category":"medical"}'`
2. GetDocumentByID returns metadata field
3. Search with filter: `./scripts/search.sh "lung" --metadata '{"category":"medical"}'`
4. Search with no filter: still works (returns all docs)
5. Search with doc_ids + metadata: both filters apply (AND)
