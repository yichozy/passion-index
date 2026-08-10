#!/bin/bash
# Fetch a single tree node by doc_id + node_id.
#
# Usage:
#   ./scripts/node.sh <doc_id> <node_id> [--raw]
#
# Example:
#   ./scripts/node.sh 019fe999-... 0005
#   ./scripts/node.sh 019fe999-... 0005 --raw
set -eu
BASE="${PASSION_INDEX_URL:-http://localhost:8900}"
DOC_ID="${1:?usage: $0 <doc_id> <node_id> [--raw]}"
NODE_ID="${2:?usage: $0 <doc_id> <node_id> [--raw]}"
MODE="${3:-pretty}"

QUERY='{
  GetDocumentNodeByNodeID(doc_id: "'"$DOC_ID"'", node_id: "'"$NODE_ID"'") {
    node_id title page_start page_end summary text
    figures { name page caption }
    nodes { node_id title summary }
    nodes { node_id title summary nodes { node_id title summary } }
  }
}'

req=$(jq -n --arg q "$QUERY" '{query: $q}')
resp=$(curl -s "$BASE/query" -H 'content-type: application/json' --data-binary "$req")

if echo "$resp" | jq -e '.errors' >/dev/null 2>&1; then
  echo "=== GraphQL errors ===" >&2
  echo "$resp" | jq '.errors' >&2
fi

if [ "$MODE" = "--raw" ]; then
  echo "$resp" | jq '.data.GetDocumentNodeByNodeID'
  exit 0
fi

echo "$resp" | jq -r '
.data.GetDocumentNodeByNodeID as $n |
if $n == null then
  "(node not found)"
else
  "● \($n.title)  [\($n.node_id)]  p\($n.page_start)-\($n.page_end)",
  (if ($n.summary // "") != "" then "  ↳ \($n.summary)" else empty end),
  (if ($n.text // "") != "" then "" else empty end),
  (if ($n.text // "") != "" then "  [text]" else empty end),
  (if ($n.text // "") != "" then $n.text else empty end),
  (($n.figures // [])[] | "  📷 \(.name) (p\(.page)) \(.caption // "")"),
  (($n.nodes // [])[] | "  └─ \(.title)  [\(.node_id)] \(.summary[:80] // "")")
end
'
