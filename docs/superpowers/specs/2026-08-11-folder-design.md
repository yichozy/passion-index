# Folder Design — passion-index

## Context

passion-index replaces PageIndex which organizes documents in a folder hierarchy (browse_documents with folder_id, recursive, sort). This spec adds folder support so hopeclaw's agent can organize and browse documents the same way.

## Data model

### New table: folders

```go
type Folder struct {
    BaseUUIDModel
    Name     string     `gorm:"not null" json:"name"`
    ParentID *uuid.UUID `gorm:"index;type:uuid" json:"parent_id"` // nil = root
}
```

Self-referential hierarchy via ParentID. Root is implicit — folders with nil ParentID are top-level.

### Update: documents

```go
type Document struct {
    ...
    FolderID *uuid.UUID `gorm:"index;type:uuid" json:"folder_id"` // nil = root
}
```

Soft delete on both tables (BaseUUIDModel already has DeletedAt).

## GraphQL API

```graphql
type Folder {
  id: ID!
  name: String!
  parent_id: ID
  created_at: Time!
  updated_at: Time!
}

type FolderNode {
  id: ID!
  name: String!
  document_count: Int!
  folders: [FolderNode!]!
}

extend type Query {
  GetFolderTree: FolderNode!    # full hierarchy from root
}

extend type Mutation {
  CreateFolder(name: String!, parent_id: ID): Folder!
  DeleteFolder(id: ID!): Boolean!
  RenameFolder(id: ID!, name: String!): Folder!
}
```

### Updated existing APIs

```graphql
# UploadDocument: add optional folder_id
UploadDocument(file: Upload!, folder_id: ID): Document!

# GetDocumentList: add optional folder_id + recursive
GetDocumentList(folder_id: ID, recursive: Boolean, limit: Int!, offset: Int!): DocumentList!
```

## Behavior

### Upload to folder
- `folder_id` optional, nil = root
- If folder_id doesn't exist → error

### Browse documents
- `folder_id` nil → root-level documents only (folder_id IS NULL)
- `folder_id` set + `recursive=false` → documents in that folder only
- `folder_id` set + `recursive=true` → documents in that folder + all descendant folders

### Folder tree
- `GetFolderTree` returns entire hierarchy as nested `FolderNode`
- Each node has `document_count` (direct children documents, not recursive)
- Root is always present (synthetic, id="root")

### Delete folder
- Soft-deletes folder
- Documents in it are NOT deleted (their folder_id becomes orphaned — or we move them to root)
- **Decision**: set orphaned documents' folder_id to NULL (move to root), don't delete

### Rename folder
- Updates name only, keeps hierarchy

## Implementation files

| File | Action |
|---|---|
| `models/folder.go` | New: Folder model |
| `models/document.go` | Add FolderID field |
| `internal/orm/migrate.go` | Add Folder to AutoMigrate |
| `internal/orm_folder/` | New package: CRUD + tree assembly |
| `services/document_service/upload_document.go` | Accept folder_id |
| `internal/orm_document/list_documents.go` | Accept folder_id + recursive |
| `graph/schema/document.graphql` | Add Folder types + mutations + query |
| `graph/document.resolvers.go` | Add folder resolvers + update upload/list |
| `scripts/folder.sh` | Test script |

## Verification

1. `CreateFolder(name: "Medical")` → returns folder
2. `CreateFolder(name: "Oncology", parent_id: <medical_id>)` → sub-folder
3. `UploadDocument(file: null, folder_id: <oncology_id>)` → doc in folder
4. `GetFolderTree` → shows hierarchy
5. `GetDocumentList(folder_id: <medical_id>, recursive: true)` → docs from Medical + Oncology
6. `GetDocumentList(folder_id: <medical_id>, recursive: false)` → only Medical direct docs
7. `DeleteFolder(id: <oncology_id>)` → folder soft-deleted, docs moved to root
8. `RenameFolder(id: ..., name: "Cancer")` → name updated
