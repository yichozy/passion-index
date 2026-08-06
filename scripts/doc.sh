#!/bin/bash
# Show document metadata (status, page count, errors, timestamps).
# Does NOT include the tree (use tree.sh for that).
#
# Usage:
#   ./scripts/doc.sh <doc_id>
set -eu
BASE="${PASSION_INDEX_URL:-http://localhost:8900}"
DOC_ID="${1:?usage: $0 <doc_id>}"

resp=$(curl -s "$BASE/query" -H 'content-type: application/json' \
  --data-binary '{"query":"{ GetDocumentByID(doc_id: \"'"$DOC_ID"'\") { doc_id filename status processing_step page_count error created_at updated_at } }"}')

# Surface GraphQL errors (otherwise they hide as null data)
if echo "$resp" | jq -e '.errors' >/dev/null 2>&1; then
  echo "=== GraphQL errors ===" >&2
  echo "$resp" | jq '.errors' >&2
fi

echo "$resp" | jq '.data.GetDocumentByID'
