#!/bin/bash
# Show a document's tree as an indented outline.
#
# Usage:
#   ./scripts/tree.sh <doc_id>              # pretty tree view (titles + page range + summary)
#   ./scripts/tree.sh <doc_id> --raw        # raw JSON from GraphQL
#
# Requires: jq
set -eu
BASE="${PASSION_INDEX_URL:-http://localhost:8900}"
DOC_ID="${1:?usage: $0 <doc_id> [--raw]}"
MODE="${2:-pretty}"

# 5 levels of nesting should cover any reasonable document.
QUERY='{
  GetDocumentByID(doc_id: "'"$DOC_ID"'") {
    status page_count
    tree {
      node_id title page_start page_end summary
      figures { name caption }
      nodes {
        node_id title page_start page_end summary
        figures { name caption }
        nodes {
          node_id title page_start page_end summary
          figures { name caption }
          nodes {
            node_id title page_start page_end summary
            figures { name caption }
            nodes {
              node_id title page_start page_end summary
              figures { name caption }
            }
          }
        }
      }
    }
  }
}'

# Use jq to build the JSON body — it handles newlines & quotes in $QUERY correctly.
# (Inline string concat like '{"query":"'"$QUERY"'"}' breaks when QUERY has literal newlines.)
req=$(jq -n --arg q "$QUERY" '{query: $q}')
resp=$(curl -s "$BASE/query" -H 'content-type: application/json' --data-binary "$req")

# Surface GraphQL errors (otherwise they hide as null data)
if echo "$resp" | jq -e '.errors' >/dev/null 2>&1; then
  echo "=== GraphQL errors ===" >&2
  echo "$resp" | jq '.errors' >&2
fi

if [ "$MODE" = "--raw" ]; then
  echo "$resp" | jq '.data.GetDocumentByID'
  exit 0
fi

# Pretty tree view.
echo "$resp" | jq -r '
def show($d):
  (if .title == "" then "(root)" else .title end) as $t
  | ((.figures // []) | length) as $fig_count
  | ("  " * $d + "• " + $t + "  [" + .node_id + "]  p" + (.page_start|tostring) + "-" + (.page_end|tostring)
      + (if $fig_count > 0 then "  (" + ($fig_count|tostring) + " fig)" else "" end)),
    (if (.summary // "") != "" then
      "  " * ($d + 1) + "↳ " + (.summary | .[0:100] + (if length > 100 then "..." else "" end))
     else empty end),
    (.nodes[]? | show($d + 1));

"status:  " + (.data.GetDocumentByID.status // "null"),
"pages:   " + ((.data.GetDocumentByID.page_count // 0) | tostring),
"",
(.data.GetDocumentByID.tree | if . then show(0) else "(no tree yet — document still processing)" end)
'
