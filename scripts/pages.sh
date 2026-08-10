#!/bin/bash
# Find tree nodes covering specific pages.
#
# Usage:
#   ./scripts/pages.sh <doc_id> <page1> [page2] [page3...]
#
# Example:
#   ./scripts/pages.sh 019fe999-... 1 5 10
#   ./scripts/pages.sh 019fe999-... 3
set -eu
BASE="${PASSION_INDEX_URL:-http://localhost:8900}"
DOC_ID="${1:?usage: $0 <doc_id> <page1> [page2...]}"
shift
[ $# -eq 0 ] && { echo "error: provide at least one page number" >&2; exit 1; }

# Build GraphQL pages array: args "1 5 10" → "[1,5,10]"
PAGES=$(IFS=,; echo "[${*}]")

QUERY='{
  GetDocumentNodesByPages(doc_id: "'"$DOC_ID"'", pages: '"$PAGES"') {
    node_id title page_start page_end summary text
  }
}'

req=$(jq -n --arg q "$QUERY" '{query: $q}')
resp=$(curl -s "$BASE/query" -H 'content-type: application/json' --data-binary "$req")

if echo "$resp" | jq -e '.errors' >/dev/null 2>&1; then
  echo "=== GraphQL errors ===" >&2
  echo "$resp" | jq '.errors' >&2
fi

echo "$resp" | jq -r '
if .data.GetDocumentNodesByPages == null then
  "(null — document not found or no tree yet)"
elif (.data.GetDocumentNodesByPages | length) == 0 then
  "(no nodes cover the requested pages)"
else
  .data.GetDocumentNodesByPages[] |
  "● \(.title)  [\(.node_id)]  p\(.page_start)-\(.page_end)",
  (if (.summary // "") != "" then "  ↳ \(.summary)" else empty end),
  (if (.text // "") != "" then .text else "(no text)" end),
  ""
end
'
