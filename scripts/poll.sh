#!/bin/bash
# Poll a document's status every N seconds until it reaches DONE or FAILED.
#
# Usage:
#   ./scripts/poll.sh <doc_id> [interval_seconds=5] [max_minutes=10]
#
# When DONE, prints the final document metadata.
# When FAILED, prints the error field and exits with code 1.
set -eu
BASE="${PASSION_INDEX_URL:-http://localhost:8900}"
DOC_ID="${1:?usage: $0 <doc_id> [interval_seconds] [max_minutes]}"
INTERVAL="${2:-5}"
MAX_MIN="${3:-10}"

deadline=$(( $(date +%s) + MAX_MIN * 60 ))
query='{ GetDocumentByID(doc_id: "'"$DOC_ID"'") { status processing_step page_count error } }'

while [ "$(date +%s)" -lt "$deadline" ]; do
  resp=$(curl -s "$BASE/query" -H 'content-type: application/json' \
    --data-binary '{"query":"'"$query"'"}')
  status=$(echo "$resp" | jq -r '.data.GetDocumentByID.status')
  step=$(echo "$resp" | jq -r '.data.GetDocumentByID.processing_step // ""')

  printf '[%s] status=%s step=%s\n' "$(date +%H:%M:%S)" "$status" "$step"

  case "$status" in
    DONE)
      echo "--- DONE ---"
      echo "$resp" | jq '.data.GetDocumentByID'
      exit 0
      ;;
    FAILED)
      echo "--- FAILED ---"
      echo "$resp" | jq '.data.GetDocumentByID'
      exit 1
      ;;
  esac
  sleep "$INTERVAL"
done

echo "error: timed out after ${MAX_MIN} minutes (still in $status)" >&2
exit 2
