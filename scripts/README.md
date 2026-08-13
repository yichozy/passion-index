# passion-index scripts

Bash helpers for hitting the local GraphQL API at `$PASSION_INDEX_URL`
(default `http://localhost:8900`). Two entry points, each with subcommands:

- `folder.sh` — folder CRUD + tree view
- `docs.sh` — document metadata, tree, nodes, search, polling, upload

Requires `jq` and `curl`.

## Quick start

```bash
# Start the server (reads .env automatically)
make run

# Create a folder, upload a doc, watch it process, view the tree
FOLDER_ID=$(./scripts/folder.sh create Medical | jq -r '.data.CreateFolder.id')
DOC_ID=$(./scripts/docs.sh   upload paper.pdf "$FOLDER_ID" | jq -r '.data.UploadDocument.id')
./scripts/docs.sh   poll "$DOC_ID"
./scripts/docs.sh   tree "$DOC_ID"
```

## folder.sh

```
./scripts/folder.sh create <name> [parent_id]
./scripts/folder.sh get <folder_id>
./scripts/folder.sh tree [folder_id] [depth]
./scripts/folder.sh rename <folder_id> <new_name>
./scripts/folder.sh delete <folder_id>
./scripts/folder.sh docs <folder_id> [--recursive] [--limit N] [--offset M]
```

`tree` pretty-prints like `tree -d` with counts:

```
Medical  [019fef5f-4044-7a73-a145-8fed6ed49f70]  docs=0 subfolders=2
  Oncology  [019fef5f-4071-72d6-8576-8296412511ab]  docs=3 subfolders=1
    Lung-Cancer  [019fef5f-4089-7e3a-9c41-2b3a4c5d6e7f]  docs=5 subfolders=0
  Cardiology  [019fef5f-40a1-7011-bcce-e8d9f0a1b2c3]  docs=2 subfolders=0
```

`delete` refuses if the folder still has documents — clear them first
(`folder.sh docs <id>` to list, `docs.sh` to delete individually).

## docs.sh

```
./scripts/docs.sh get <doc_id>
./scripts/docs.sh tree <doc_id> [--raw]
./scripts/docs.sh node <node_id> [--raw]
./scripts/docs.sh pages <doc_id> <page1> [page2...]
./scripts/docs.sh search "<query>" <folder_id> [--recursive] [--metadata '{"key":"value"}']
./scripts/docs.sh search-nodes "<query>" <folder_id> [--recursive] [--metadata '{"key":"value"}']
./scripts/docs.sh poll <doc_id> [interval_seconds=5] [max_minutes=10]
./scripts/docs.sh upload <pdf_path> <folder_id> [metadata_json]
./scripts/docs.sh resummarize <doc_id> [--force]
```

`resummarize` kicks off async summary regeneration. Default (no `--force`)
only fills empty summaries (e.g., previously failed LLM calls); `--force`
regenerates every node. Returns immediately — poll with `docs.sh poll <doc_id>`
or watch status via `docs.sh get <doc_id>` (transitions `DONE → SUMMARY → DONE`).

`tree` shows a pretty outline (titles, page ranges, summaries, figures).
`--raw` dumps the raw JSON instead.

`poll` loops every N seconds, exits 0 on DONE, 1 on FAILED, 2 on timeout.

`upload` is the only subcommand using multipart form (GraphQL `Upload`
scalar). `folder_id` is **required** — every document must live in a folder.
Optional third arg `metadata_json` attaches free-form metadata (e.g.
`'{"doi":"10.1234/abc","indication":["lung cancer"]}'`).

## Common workflows

### Build a folder hierarchy

```bash
MEDICAL=$(./scripts/folder.sh create Medical | jq -r '.data.CreateFolder.id')
ONCOLOGY=$(./scripts/folder.sh create Oncology "$MEDICAL" | jq -r '.data.CreateFolder.id')
./scripts/folder.sh tree  # view the result
```

### Upload + process + view

```bash
DOC_ID=$(./scripts/docs.sh upload paper.pdf "$ONCOLOGY" | jq -r '.data.UploadDocument.id')
./scripts/docs.sh poll "$DOC_ID"
./scripts/docs.sh tree "$DOC_ID"
```

### List documents in a folder (recursive)

```bash
./scripts/folder.sh docs "$ONCOLOGY" --recursive --limit 50
```

### Search documents (doc-level: filename + title + description)

```bash
# Just the folder's direct contents
./scripts/docs.sh search "lung cancer" "$FOLDER_ID"

# Include sub-folders
./scripts/docs.sh search "lung cancer" "$FOLDER_ID" --recursive

# Filter by metadata (JSONB @> containment)
./scripts/docs.sh search "lung cancer" "$FOLDER_ID" --metadata '{"indication":["lung cancer"]}'
```

### Search inside document content (node-level: title + summary + text)

```bash
# Find which section of which doc covers a topic
./scripts/docs.sh search-nodes "nivolumab cost effectiveness" "$FOLDER_ID" --recursive
```

### Drill into specific pages of a document

```bash
./scripts/docs.sh pages "$DOC_ID" 1 5 10
./scripts/docs.sh node  "$NODE_ID" --raw   # NODE_ID is a UUID now
```
