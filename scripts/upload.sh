#!/bin/bash
# Upload a PDF and start the processing pipeline.
#
# Usage:
#   ./scripts/upload.sh <pdf-path>
#
# Output: JSON with id, filename, status (PENDING).
# Tip: pipe to jq to grab just the doc id:
#   DOC_ID=$(./scripts/upload.sh paper.pdf | jq -r '.data.UploadDocument.id')
set -eu
BASE="${PASSION_INDEX_URL:-http://localhost:8900}"
PDF="${1:?usage: $0 <pdf-path>}"

if [ ! -f "$PDF" ]; then
  echo "error: file not found: $PDF" >&2
  exit 1
fi

curl -s "$BASE/query" \
  -F operations='{"query":"mutation($file: Upload!) { UploadDocument(file: $file) { id filename status } }","variables":{"file":null}}' \
  -F map='{"0":["variables.file"]}' \
  -F "0=@$PDF" \
  | jq
