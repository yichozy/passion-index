#!/bin/bash
# Document operations: metadata, tree, nodes, search, polling, upload.
#
# Usage:
#   ./scripts/docs.sh get <doc_id>
#   ./scripts/docs.sh tree <doc_id> [--raw]
#   ./scripts/docs.sh node <node_id> [--raw]
#   ./scripts/docs.sh pages <doc_id> <page1> [page2...]
#   ./scripts/docs.sh search "<query>" [--doc-ids uuid1,uuid2] [--metadata '{"key":"value"}']
#   ./scripts/docs.sh poll <doc_id> [interval_seconds=5] [max_minutes=10]
#   ./scripts/docs.sh upload <pdf_path> <folder_id> [metadata_json]
#   ./scripts/docs.sh resummarize <doc_id> [--force]
set -eu
BASE="${PASSION_INDEX_URL:-http://localhost:8900}"

cmd="${1:-}"
[ -z "$cmd" ] && { echo "usage: $0 <get|tree|node|pages|search|poll|upload|resummarize> ..." >&2; exit 1; }
shift

# Send a JSON query and return the response body.
# Optional second arg: variables JSON object (for queries that need typed
# variables like JSON scalars that can't be inlined in GraphQL).
send_query() {
	local query="$1" req
	if [ -n "${2:-}" ]; then
		req=$(jq -n --arg q "$query" --argjson v "$2" '{query: $q, variables: $v}')
	else
		req=$(jq -n --arg q "$query" '{query: $q}')
	fi
	if [ -z "$req" ]; then
		echo "error: failed to build request body (jq failed — likely invalid JSON variables)" >&2
		exit 1
	fi
	curl -s "$BASE/query" -H 'content-type: application/json' --data-binary "$req"
}

# Print GraphQL errors to stderr and exit 1 if present.
surface_errors() {
	local resp="$1"
	if echo "$resp" | jq -e '.errors' >/dev/null 2>&1; then
		echo "=== GraphQL errors ===" >&2
		echo "$resp" | jq '.errors' >&2
		exit 1
	fi
}

case "$cmd" in
	get)
		doc_id="${1:?usage: get <doc_id>}"
		query='{ GetDocument(id: "'"$doc_id"'") { id filename status folder { id name } metadata page_count error created_at updated_at } }'
		resp=$(send_query "$query"); surface_errors "$resp"
		echo "$resp" | jq '.data.GetDocument'
		;;

	tree)
		doc_id="${1:?usage: tree <doc_id> [--raw]}"
		mode="${2:-pretty}"
		query='{ GetDocument(id: "'"$doc_id"'") { status page_count tree { node_id title page_start page_end summary figures { name caption } nodes { node_id title page_start page_end summary figures { name caption } nodes { node_id title page_start page_end summary figures { name caption } nodes { node_id title page_start page_end summary figures { name caption } nodes { node_id title page_start page_end summary figures { name caption } } } } } } } }'
		resp=$(send_query "$query"); surface_errors "$resp"
		if [ "$mode" = "--raw" ]; then
			echo "$resp" | jq '.data.GetDocument'
			exit 0
		fi
		echo "$resp" | jq -r '
			def show($d):
				(if .title == "" then "(root)" else .title end) as $t
				| ((.figures // []) | length) as $fig_count
				| ("  " * $d + "• " + $t + "  [" + (.node_id|tostring) + "]  p" + (.page_start|tostring) + "-" + (.page_end|tostring)
						+ (if $fig_count > 0 then "  (" + ($fig_count|tostring) + " fig)" else "" end)),
					(if (.summary // "") != "" then
						"  " * ($d + 1) + "↳ " + (.summary | .[0:100] + (if length > 100 then "..." else "" end))
					 else empty end),
					(.nodes[]? | show($d + 1));
			"status:  " + (.data.GetDocument.status // "null"),
			"pages:   " + ((.data.GetDocument.page_count // 0) | tostring),
			"",
			(.data.GetDocument.tree | if . then show(0) else "(no tree yet — document still processing)" end)
		'
		;;

	node)
		node_id="${1:?usage: node <node_id> [--raw]}"
		mode="${2:-pretty}"
		query='{ GetDocumentNode(node_id: "'"$node_id"'") { node_id title page_start page_end summary text figures { name page caption } nodes { node_id title summary nodes { node_id title summary } } } }'
		resp=$(send_query "$query"); surface_errors "$resp"
		if [ "$mode" = "--raw" ]; then
			echo "$resp" | jq '.data.GetDocumentNode'
			exit 0
		fi
		echo "$resp" | jq -r '
			.data.GetDocumentNode as $n |
			if $n == null then
				"(node not found)"
			else
				"● \($n.title)  [\($n.node_id)]  p\($n.page_start)-\($n.page_end)",
				(if ($n.summary // "") != "" then "  ↳ \($n.summary)" else empty end),
				(if ($n.text // "") != "" then "  [text]" else empty end),
				(if ($n.text // "") != "" then $n.text else empty end),
				(($n.figures // [])[] | "  📷 \(.name) (p\(.page)) \(.caption // "")"),
				(($n.nodes // [])[] | "  └─ \(.title)  [\(.node_id)] \(.summary[:80] // "")")
			end
		'
		;;

	pages)
		doc_id="${1:?usage: pages <doc_id> <page1> [page2...]}"
		shift
		[ $# -eq 0 ] && { echo "error: provide at least one page number" >&2; exit 1; }
		pages=$(IFS=,; echo "[${*}]")
		query='{ GetDocumentNodesByPages(doc_id: "'"$doc_id"'", pages: '"$pages"') { node_id title page_start page_end summary text } }'
		resp=$(send_query "$query"); surface_errors "$resp"
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
		;;

	search)
		query_str="${1:?usage: search <query> [--doc-ids uuid1,uuid2] [--metadata json]}"
		shift
		doc_ids=""
		metadata=""
		while [ $# -gt 0 ]; do
			case "$1" in
				--doc-ids) doc_ids="$2"; shift 2 ;;
				--metadata) metadata="$2"; shift 2 ;;
				*) shift ;;
			esac
		done
		doc_ids_arg=""
		if [ -n "$doc_ids" ]; then
			ids=$(echo "$doc_ids" | tr ',' '\n' | sed 's/^/"/;s/$/"/' | paste -sd, -)
			doc_ids_arg="doc_ids: [$ids],"
		fi
		# GraphQL doesn't support inline JSON objects — pass metadata as variable.
		gql_query='query($metadata: JSON) {
			SearchDocuments(query: "'"$query_str"'", '"$doc_ids_arg"' metadata: $metadata, limit: 10) {
				doc_id filename score
				matches { node_id score }
			}
		}'
		if [ -n "$metadata" ]; then
			# Validate metadata is valid JSON before sending — keys must be
			# double-quoted (e.g. '{"indication":"lung cancer"}', not '{indication:"lung cancer"}').
			if ! echo "$metadata" | jq -e . >/dev/null 2>&1; then
				echo "error: --metadata is not valid JSON: $metadata" >&2
				echo '       hint: JSON requires double-quoted keys, e.g. --metadata '\''{"indication":"lung cancer"}'\''' >&2
				exit 1
			fi
			resp=$(send_query "$gql_query" "{\"metadata\":$metadata}")
		else
			resp=$(send_query "$gql_query" '{"metadata":null}')
		fi
		surface_errors "$resp"
		echo "$resp" | jq -r '
			if (.data.SearchDocuments | length) == 0 then
				"(no results)"
			else
				.data.SearchDocuments[] |
				"📄 \(.filename)  score=\(.score | tostring | .[0:5])  [\(.doc_id)]",
				(.matches[]? | "   → node \(.node_id) (score=\(.score | tostring | .[0:5]))"),
				""
			end
		'
		;;

	poll)
		doc_id="${1:?usage: poll <doc_id> [interval_seconds] [max_minutes]}"
		interval="${2:-5}"
		max_min="${3:-10}"
		deadline=$(( $(date +%s) + max_min * 60 ))
		query='{ GetDocument(id: "'"$doc_id"'") { status page_count error } }'
		while [ "$(date +%s)" -lt "$deadline" ]; do
			resp=$(send_query "$query")
			status=$(echo "$resp" | jq -r '.data.GetDocument.status')
			printf '[%s] status=%s\n' "$(date +%H:%M:%S)" "$status"
			case "$status" in
				DONE)
					echo "--- DONE ---"
					echo "$resp" | jq '.data.GetDocument'
					exit 0
					;;
				FAILED)
					echo "--- FAILED ---"
					echo "$resp" | jq '.data.GetDocument'
					exit 1
					;;
			esac
			sleep "$interval"
		done
		echo "error: timed out after ${max_min} minutes (still in $status)" >&2
		exit 2
		;;

	upload)
		pdf="${1:?usage: upload <pdf_path> <folder_id> [metadata_json]}"
		folder_id="${2:?usage: upload <pdf_path> <folder_id> [metadata_json]}"
		metadata_json="${3:-null}"
		if [ ! -f "$pdf" ]; then
			echo "error: file not found: $pdf" >&2
			exit 1
		fi
		# Upload uses multipart form. metadata_json is either a JSON object
		# string (e.g. '{"doi":"10.1234"}') or null when omitted.
		curl -s "$BASE/query" \
			-F operations='{"query":"mutation($file: Upload!, $folder_id: UUID!, $metadata: JSON) { UploadDocument(file: $file, folder_id: $folder_id, metadata: $metadata) { id filename status metadata folder { id name } } }","variables":{"file":null,"folder_id":"'"$folder_id"'","metadata":'"$metadata_json"'}}' \
			-F map='{"0":["variables.file"]}' \
			-F "0=@$pdf" \
			| jq
		;;

	resummarize)
		doc_id="${1:?usage: resummarize <doc_id> [--force]}"
		shift
		force="false"
		while [ $# -gt 0 ]; do
			case "$1" in
				--force) force="true"; shift ;;
				*) shift ;;
			esac
		done
		query='mutation { ReSummarizeDocument(doc_id: "'"$doc_id"'", force: '"$force"') }'
		resp=$(send_query "$query"); surface_errors "$resp"
		echo "$resp" | jq '.data.ReSummarizeDocument'
		;;

	*)
		echo "unknown command: $cmd" >&2
		exit 1
		;;
esac
