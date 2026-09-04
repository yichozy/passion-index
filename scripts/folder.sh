#!/bin/bash
# Folder CRUD + tree operations over the passion-index REST API.
#
# Usage:
#   ./scripts/folder.sh create <name> [parent_id]
#   ./scripts/folder.sh get <folder_id>
#   ./scripts/folder.sh tree [folder_id] [depth]
#   ./scripts/folder.sh rename <folder_id> <new_name>
#   ./scripts/folder.sh delete <folder_id>
#   ./scripts/folder.sh docs [folder_id] [--recursive] [--limit N] [--offset M]
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

# GET a JSON endpoint; surface HTTP errors and exit 1 on non-200.
rest_get() {
	local path="$1"; shift
	local resp code body
	resp=$(curl -s -w "\n%{http_code}" --get "$BASE$path" "$@")
	code=$(echo "$resp" | tail -1)
	body=$(echo "$resp" | sed '$d')
	if [ "$code" != "200" ]; then
		echo "error: HTTP $code $(echo "$body" | jq -r '.error // empty')" >&2
		exit 1
	fi
	echo "$body"
}

case "$cmd" in
  create)
    name="${1:?usage: create <name> [parent_id]}"
    parent_id="${2:-}"
    body=$(jq -n --arg name "$name" --arg parent_id "$parent_id" '{name: $name} + (if $parent_id == "" then {} else {parent_id: $parent_id} end)')
    curl -s -w "\nHTTP %{http_code}\n" -X POST "$BASE/createFolder" -H 'content-type: application/json' -d "$body" | jq . 2>/dev/null || true
    ;;

  get)
    rest_get "/getFolderById" --data-urlencode "id=${1:?usage: get <folder_id>}" | jq '.'
    ;;

  tree)
    folder_id="${1:-}"
    depth="${2:-3}"
    args=(--data-urlencode "depth=$depth")
    [ -n "$folder_id" ] && args+=(--data-urlencode "folder_id=$folder_id")
    # Pretty-print like `tree -d`, with counts.
    rest_get "/getFolderTree" "${args[@]}" | jq -r '
      def show($depth):
        ("  " * $depth) + .name + "  [" + .id + "]  docs=\(.document_count) subfolders=\(.folder_count)",
        (.folders[]? | show($depth + 1));
      .[]? | show(0)
    '
    ;;

  rename)
    id="${1:?usage: rename <folder_id> <new_name>}"
    name="${2:?usage: rename <folder_id> <new_name>}"
    curl -s -X PATCH "$BASE/renameFolder" -H "content-type: application/json" \
      -d "$(jq -n --arg id "$id" --arg name "$name" '{id: $id, name: $name}')" | jq '.'
    ;;

  delete)
    id="${1:?usage: delete <folder_id>}"
    code=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "$BASE/deleteFolder" --data-urlencode "id=$id")
    case "$code" in
      204) echo "deleted" ;;
      409) echo "error: folder still contains documents" >&2; exit 1 ;;
      *) echo "error: HTTP $code" >&2; exit 1 ;;
    esac
    ;;

  docs)
    folder_id="${1:-}"
    shift 2>/dev/null || true
    recursive="false"; limit=20; offset=0
    while [ $# -gt 0 ]; do
      case "$1" in
        --recursive) recursive="true"; shift ;;
        --limit) limit="$2"; shift 2 ;;
        --offset) offset="$2"; shift 2 ;;
        *) shift ;;
      esac
    done
    args=(--data-urlencode "recursive=$recursive" --data-urlencode "limit=$limit" --data-urlencode "offset=$offset")
    [ -n "$folder_id" ] && args+=(--data-urlencode "folder_id=$folder_id")
    rest_get "/getDocumentList" "${args[@]}" | jq -r '
      "total: \(.total)",
      "",
      (.items[]? |
        "📄 \(.filename)  [\(.status)]  [\(.id)]  folder=\(.folder.name // "-")",
        (if (.title // "") != "" then "  title:       \(.title)" else empty end),
        (if (.description // "") != "" then "  description: \(.description | .[0:120] + (if length > 120 then "..." else "" end))" else empty end))
    '
    ;;

  *)
    echo "unknown command: $cmd" >&2
    exit 1
    ;;
esac
