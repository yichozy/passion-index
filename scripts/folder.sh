#!/bin/bash
# Folder CRUD + tree operations.
#
# Usage:
#   ./scripts/folder.sh create <name> [parent_id]
#   ./scripts/folder.sh get <folder_id>
#   ./scripts/folder.sh tree [folder_id] [depth]
#   ./scripts/folder.sh rename <folder_id> <new_name>
#   ./scripts/folder.sh delete <folder_id>
#   ./scripts/folder.sh docs <folder_id> [--recursive] [--limit N] [--offset M]
#
# Examples:
#   ./scripts/folder.sh create Medical
#   ./scripts/folder.sh create Oncology "$MEDICAL_ID"
#   ./scripts/folder.sh tree                # whole tree from root, depth=3
#   ./scripts/folder.sh tree "$MEDICAL_ID" 5
#   ./scripts/folder.sh docs "$MEDICAL_ID" --recursive --limit 50
set -eu
BASE="${PASSION_INDEX_URL:-http://localhost:8900}"

cmd="${1:-}"
[ -z "$cmd" ] && { echo "usage: $0 <create|get|tree|rename|delete|docs> ..." >&2; exit 1; }
shift

case "$cmd" in
  create)
    name="${1:?usage: create <name> [parent_id]}"
    parent_id="${2:-}"
    parent_arg=""
    [ -n "$parent_id" ] && parent_arg=", parent_id: \"$parent_id\""
    query='mutation { CreateFolder(name: "'"$name"'"'"$parent_arg"') { id name parent_id created_at } }'
    ;;
  get)
    id="${1:?usage: get <folder_id>}"
    query='{ GetFolder(id: "'"$id"'") { id name parent_id created_at updated_at } }'
    ;;
  tree)
    folder_id="${1:-}"
    depth="${2:-3}"
    folder_arg=""
    [ -n "$folder_id" ] && folder_arg="folder_id: \"$folder_id\", "
    # 3 levels of inlined `folders` for a useful default view; depth arg
    # controls how many levels the server expands, not how many we request.
    query='{ GetFolderTree('"$folder_arg"'depth: '"$depth"') { id name parent_id document_count folder_count folders { id name document_count folder_count folders { id name document_count folder_count } } } }'
    ;;
  rename)
    id="${1:?usage: rename <folder_id> <new_name>}"
    name="${2:?usage: rename <folder_id> <new_name>}"
    query='mutation { RenameFolder(id: "'"$id"'", name: "'"$name"'") { id name updated_at } }'
    ;;
  delete)
    id="${1:?usage: delete <folder_id>}"
    query='mutation { DeleteFolder(id: "'"$id"'") }'
    ;;
  docs)
    folder_id="${1:?usage: docs <folder_id> [--recursive] [--limit N] [--offset M]}"
    shift
    recursive="false"
    limit=20
    offset=0
    while [ $# -gt 0 ]; do
      case "$1" in
        --recursive) recursive="true"; shift ;;
        --limit) limit="$2"; shift 2 ;;
        --offset) offset="$2"; shift 2 ;;
        *) shift ;;
      esac
    done
    query='{ GetDocumentListByFolder(folder_id: "'"$folder_id"'", recursive: '"$recursive"', limit: '"$limit"', offset: '"$offset"') { total items { id filename title description status folder { id name } created_at } } }'
    ;;
  *)
    echo "unknown command: $cmd" >&2
    exit 1
    ;;
esac

req=$(jq -n --arg q "$query" '{query: $q}')
resp=$(curl -s "$BASE/query" -H 'content-type: application/json' --data-binary "$req")

# Surface GraphQL errors (otherwise they hide as null data).
if echo "$resp" | jq -e '.errors' >/dev/null 2>&1; then
  echo "=== GraphQL errors ===" >&2
  echo "$resp" | jq '.errors' >&2
  exit 1
fi

# Per-command output.
case "$cmd" in
  create)
    echo "$resp" | jq '.data.CreateFolder'
    ;;
  get)
    echo "$resp" | jq '.data.GetFolder'
    ;;
  rename)
    echo "$resp" | jq '.data.RenameFolder'
    ;;
  delete)
    echo "$resp" | jq '.data.DeleteFolder'
    ;;
  tree)
    # Pretty-print like `tree -d`, with counts.
    echo "$resp" | jq -r '
      def show($depth):
        ("  " * $depth) + .name + "  [" + .id + "]  docs=\(.document_count) subfolders=\(.folder_count)",
        (.folders[]? | show($depth + 1));
      .data.GetFolderTree[]? | show(0)
    '
    ;;
  docs)
    echo "$resp" | jq -r '
      .data.GetDocumentListByFolder as $r |
      "total: \($r.total)",
      "",
      ($r.items[]? |
        "📄 \(.filename)  [\(.status)]  [\(.id)]  folder=\(.folder.name // "-")",
        (if (.title // "") != "" then "  title:       \(.title)" else empty end),
        (if (.description // "") != "" then "  description: \(.description | .[0:120] + (if length > 120 then "..." else "" end))" else empty end))
    '
    ;;
esac
