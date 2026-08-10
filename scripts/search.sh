#!/bin/bash
# Full-text search across documents.
#
# Usage:
#   ./scripts/search.sh "lung cancer immunotherapy"
#   ./scripts/search.sh "cost" --doc-ids "uuid1,uuid2"
set -eu
BASE="${PASSION_INDEX_URL:-http://localhost:8900}"
QUERY="${1:?usage: $0 <query> [--doc-ids uuid1,uuid2]}"
shift

DOC_IDS=""
while [ $# -gt 0 ]; do
	case "$1" in
		--doc-ids) DOC_IDS="$2"; shift 2 ;;
		*) shift ;;
	esac
done

DOC_IDS_ARG=""
if [ -n "$DOC_IDS" ]; then
	IDS=$(echo "$DOC_IDS" | tr ',' '\n' | sed 's/^/"/;s/$/"/' | paste -sd, -)
	DOC_IDS_ARG="doc_ids: [$IDS],"
fi

QUERY_GQL='{
  SearchDocuments(query: "'"$QUERY"'", '"$DOC_IDS_ARG"' limit: 10) {
    doc_id filename score
    matches { node_id score }
  }
}'

req=$(jq -n --arg q "$QUERY_GQL" '{query: $q}')
resp=$(curl -s "$BASE/query" -H 'content-type: application/json' --data-binary "$req")

if echo "$resp" | jq -e '.errors' >/dev/null 2>&1; then
	echo "=== GraphQL errors ===" >&2
	echo "$resp" | jq '.errors' >&2
fi

echo "$resp" | jq -r '
if (.data.SearchDocuments | length) == 0 then
  "(no results)"
else
  .data.SearchDocuments[] |
  "📄 \(.filename)  score=\(.score | tostring | .[0:5])  [\(.doc_id | .[0:8])]...",
  (.matches[]? | "   → node \(.node_id) (score=\(.score | tostring | .[0:5]))"),
  ""
end
'
