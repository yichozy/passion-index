# passion-index API Reference

Base URL: `http://<host>:<port>` (default port `8900`)

## GraphQL

**Endpoint**: `POST /query`
**Content-Type**: `application/json` (or `multipart/form-data` for file uploads)
**Playground**: `GET /` (GraphiQL, dev only)

---

## Queries

### GetDocumentByID

Fetch a single document by ID, including metadata and full tree.

```graphql
query {
  GetDocumentByID(doc_id: "019fe999-16a7-71a9-8729-8b781253e35d") {
    doc_id
    filename
    status
    page_count
    error
    created_at
    updated_at
    tree {
      node_id
      title
      page_start
      page_end
      summary
      text
      figures { name page caption }
      nodes {
        node_id title summary
        nodes { node_id title summary }
      }
    }
  }
}
```

**Arguments**

| Name | Type | Required | Description |
|---|---|---|---|
| `doc_id` | `ID!` | Yes | Document UUID |

**Returns**: `Document` or `null` (not found)

---

### GetDocumentList

Paginated list of all documents, ordered by `created_at DESC`.

```graphql
query {
  GetDocumentList(limit: 10, offset: 0) {
    total
    items {
      doc_id
      filename
      status
      page_count
      created_at
    }
  }
}
```

**Arguments**

| Name | Type | Required | Default | Description |
|---|---|---|---|---|
| `limit` | `Int!` | Yes | — | Max items to return |
| `offset` | `Int!` | Yes | — | Pagination offset |

**Returns**: `DocumentList!` (always non-null)

---

### GetDocumentNodeByNodeID

Fetch a single tree node by document ID + node ID. Loads the entire tree internally but returns only the matching node.

```graphql
query {
  GetDocumentNodeByNodeID(
    doc_id: "019fe999-16a7-71a9-8729-8b781253e35d"
    node_id: "0005"
  ) {
    node_id
    title
    page_start
    page_end
    summary
    text
    figures { name caption }
    nodes { node_id title summary }
  }
}
```

**Arguments**

| Name | Type | Required | Description |
|---|---|---|---|
| `doc_id` | `ID!` | Yes | Document UUID |
| `node_id` | `ID!` | Yes | Node ID (4-digit zero-padded, e.g. `"0005"`) |

**Returns**: `TreeNode` or `null` (document or node not found)

---

### GetDocumentNodesByPages

Find the deepest tree nodes covering the given pages, deduplicated by node ID. Useful for "which section is on page N?" lookups.

```graphql
query {
  GetDocumentNodesByPages(
    doc_id: "019fe999-16a7-71a9-8729-8b781253e35d"
    pages: [1, 5, 10]
  ) {
    node_id
    title
    page_start
    page_end
    summary
  }
}
```

**Arguments**

| Name | Type | Required | Description |
|---|---|---|---|
| `doc_id` | `ID!` | Yes | Document UUID |
| `pages` | `[Int!]!` | Yes | Array of 0-based page numbers |

**Returns**: `[TreeNode!]!` (list of covering nodes, deduplicated)

**Behavior**: A node spanning pages 3-7 will be returned once for any of pages 3, 4, 5, 6, 7. Pages with no covering node are skipped.

---

## Mutations

### UploadDocument

Upload a PDF and start the processing pipeline (OCR → structuring → summarization). Returns immediately with `status: PENDING`; processing runs asynchronously.

**Requires multipart form data** (not standard GraphQL JSON):

```bash
curl http://localhost:8900/query \
  -F operations='{"query":"mutation($file: Upload!) { UploadDocument(file: $file) { doc_id filename status } }","variables":{"file":null}}' \
  -F map='{"0":["variables.file"]}' \
  -F 0=@paper.pdf
```

**Arguments**

| Name | Type | Required | Description |
|---|---|---|---|
| `file` | `Upload!` | Yes | PDF file (max 100MB) |

**Returns**: `Document!`

**Pipeline states**: `PENDING → OCR → STRUCTURING → SUMMARY → DONE` (or `FAILED` at any step)

---

### DeleteDocument

Delete a document by ID. Idempotent — deleting a non-existent doc returns `false`.

```graphql
mutation {
  DeleteDocument(doc_id: "019fe999-16a7-71a9-8729-8b781253e35d")
}
```

**Arguments**

| Name | Type | Required | Description |
|---|---|---|---|
| `doc_id` | `ID!` | Yes | Document UUID |

**Returns**: `Boolean!` (`true` if deleted, `false` if not found)

> Note: Does not delete the PDF or images from OSS.

---

## REST Endpoints

### GET /healthz

Health check.

```bash
curl http://localhost:8900/healthz
# {"status":"ok","time":"2026-08-10T12:00:00Z"}
```

### GET /documents/:docID/images/:name

Fetch a document figure image. Returns a **302 redirect** to an OSS signed URL (24h expiry). The client follows the redirect to download directly from OSS.

```bash
curl -L -o figure.jpg \
  http://localhost:8900/documents/019fe999-16a7-71a9-8729-8b781253e35d/images/4db4a245.jpg
```

**Path Parameters**

| Name | Description |
|---|---|
| `docID` | Document UUID |
| `name` | Figure filename (from `TreeNode.figures[].name`) |

**Responses**

| Status | Description |
|---|---|
| `302` | Redirect to OSS signed URL |
| `400` | Invalid image name (path traversal attempt) |
| `404` | Image not found in OSS |
| `500` | OSS initialization failure |

---

## Types

### Document

| Field | Type | Description |
|---|---|---|
| `doc_id` | `ID!` | UUID v7 |
| `filename` | `String!` | Original upload filename |
| `status` | `DocStatus!` | Pipeline state (see below) |
| `page_count` | `Int` | Total pages (null until structuring completes) |
| `error` | `String` | Error message if status is FAILED |
| `tree` | `TreeNode` | Root tree node (null until structuring completes) |
| `created_at` | `Time!` | Upload timestamp |
| `updated_at` | `Time!` | Last update timestamp |

### TreeNode

| Field | Type | Description |
|---|---|---|
| `node_id` | `ID!` | 4-digit zero-padded (e.g. `"0005"`) |
| `title` | `String!` | Section title |
| `page_start` | `Int!` | First page (0-based) |
| `page_end` | `Int!` | Last page (0-based, inclusive) |
| `summary` | `String!` | LLM-generated summary (leaf nodes only) |
| `text` | `String` | Raw text content from OCR |
| `figures` | `[Figure!]!` | Embedded figures in this section |
| `nodes` | `[TreeNode!]!` | Child sections (recursive) |

### Figure

| Field | Type | Description |
|---|---|---|
| `name` | `String!` | Image filename (OSS object basename) |
| `page` | `Int!` | Page number (0-based) |
| `data` | `String` | Base64 image data (not populated; use REST endpoint) |
| `caption` | `String` | Figure caption from OCR |

### DocStatus

```
enum DocStatus {
  PENDING       # Uploaded, waiting for pipeline
  OCR           # MinerU OCR in progress
  STRUCTURING   # MinerU-Popo tree building in progress
  SUMMARY       # LLM summarization in progress
  DONE          # Processing complete
  FAILED        # Pipeline failed (check error field)
}
```

### DocumentList

| Field | Type | Description |
|---|---|---|
| `items` | `[Document!]!` | Document array |
| `total` | `Int!` | Total document count (ignoring limit/offset) |

---

## Field selection

GraphQL lets clients choose which fields to include. Omit heavy fields like `text` when not needed:

```graphql
# Lightweight: only structure + summaries (feed to downstream LLM)
query {
  GetDocumentByID(doc_id: "xxx") {
    tree {
      node_id title summary
      nodes { node_id title summary }
    }
  }
}

# Full: include raw text (heavier response)
query {
  GetDocumentByID(doc_id: "xxx") {
    tree {
      node_id title summary text
      figures { name caption }
    }
  }
}
```
