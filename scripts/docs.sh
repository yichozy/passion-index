#!/bin/bash
# Document operations over the passion-index REST API.
#
# Usage:
#   ./scripts/docs.sh get <doc_id>
#   ./scripts/docs.sh tree <doc_id> [--raw]
#   ./scripts/docs.sh node <node_id> [--raw]
#   ./scripts/docs.sh pages <doc_id> <page1> [page2...]     # 5 / 7 / 10-12
#   ./scripts/docs.sh search "<query>" [folder_id] [--recursive] [--keyword]
#   ./scripts/docs.sh search-nodes "<query>" <folder_id> [--recursive]
#   ./scripts/docs.sh figure <doc_id> <figure_name> [output_path]
#   ./scripts/docs.sh poll <doc_id> [interval_seconds=5] [max_minutes=10]
#   ./scripts/docs.sh upload <pdf_path> <folder_id> [metadata_json]
#   ./scripts/docs.sh resummarize <doc_id> [--force]
set -eu
BASE="${PASSION_INDEX_URL:-http://localhost:8900}"

cmd="${1:-}"
[ -z "$cmd" ] && { echo "usage: $0 <get|tree|node|pages|search|search-nodes|figure|poll|upload|resummarize> ..." >&2; exit 1; }
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
	get)
		rest_get "/getDocumentById" --data-urlencode "id=${1:?usage: get <doc_id>}" | jq 'del(.tree)'
		;;

	tree)
		doc_id="${1:?usage: tree <doc_id> [--raw]}"
		mode="${2:-pretty}"
		resp=$(rest_get "/getDocumentById" --data-urlencode "id=$doc_id")
		if [ "$mode" = "--raw" ]; then
			echo "$resp" | jq '.'
			exit 0
		fi
		echo "$resp" | jq -r '
			def show($d):
				(if .title == "" then "(root)" else .title end) as $t
				| ((.figures // []) | length) as $fig_count
				| ("  " * $d + "• " + $t + "  [" + (.id|tostring) + "]  p" + (.page_start|tostring) + "-" + (.page_end|tostring)
						+ (if $fig_count > 0 then "  (" + ($fig_count|tostring) + " fig)" else "" end)),
					(if (.summary // "") != "" then
						"  " * ($d + 1) + "↳ " + (.summary | .[0:100] + (if length > 100 then "..." else "" end))
					 else empty end),
					(.nodes[]? | show($d + 1));
			"status:  " + (.status // "null"),
			"pages:   " + ((.page_count // 0) | tostring),
			"",
			(.tree | if . then show(0) else "(no tree yet — document still processing)" end)
		'
		;;

	node)
		node_id="${1:?usage: node <node_id> [--raw]}"
		mode="${2:-pretty}"
		resp=$(rest_get "/getDocumentSectionById" --data-urlencode "id=$node_id")
		if [ "$mode" = "--raw" ]; then
			echo "$resp" | jq '.'
			exit 0
		fi
		echo "$resp" | jq -r '
			"● \(.title)  [\(.id)]  p\(.page_start)-\(.page_end)",
			(if (.summary // "") != "" then "  ↳ \(.summary)" else empty end),
			(if (.text // "") != "" then "  [text]" else empty end),
			(if (.text // "") != "" then .text else empty end),
			((.figures // [])[] | "  📷 \(.name) (p\(.page)) \(.caption // "")"),
			((.nodes // [])[] | "  └─ \(.title)  [\(.id)] \(.summary[:80] // "")")
		'
		;;

	pages)
		doc_id="${1:?usage: pages <doc_id> <page1> [page2...]}"
		shift
		[ $# -eq 0 ] && { echo "error: provide at least one page (single, list member, or range like 5-10)" >&2; exit 1; }
		pages=$(IFS=,; echo "$*")
		rest_get "/getPageContent" --data-urlencode "doc_id=$doc_id" --data-urlencode "pages=$pages" | jq -r '
			if length == 0 then
				"(no pages returned)"
			else
				.[] |
				"● p\(.page)",
				(if (.text // "") != "" then .text else "(no text — document may predate the pages store)" end),
				""
			end'
		;;

	figure)
		doc_id="${1:?usage: figure <doc_id> <figure_name> [output_path]}"
		figure_name="${2:?usage: figure <doc_id> <figure_name> [output_path]}"
		output_path="${3:-}"
		resp=$(rest_get "/getFigure" --data-urlencode "doc_id=$doc_id" --data-urlencode "name=$figure_name")
		if [ -n "$output_path" ]; then
			data=$(echo "$resp" | jq -r '.data // empty')
			[ -z "$data" ] && { echo "(figure not found)" >&2; exit 1; }
			printf '%s' "$data" | base64 --decode >"$output_path"
			echo "$resp" | jq --arg output_path "$output_path" '
				{ name, page, caption, saved_to: $output_path, bytes: ((.data | @base64d) | length) }
			'
			exit 0
		fi
		echo "$resp" | jq '.'
		;;

	search)
		# Document-level search. Default semantic: vector recall over node
		# embeddings + DocScore — matches by meaning. --keyword: BM25 over
		# filename + title + description — literal terms only.
		query_str="${1:?usage: search <query> [folder_id] [--recursive] [--keyword]}"
		folder_id="${2:-}"
		shift 2>/dev/null || true
		recursive="false"; mode="semantic"
		for arg in "$@"; do
			case "$arg" in
				--recursive) recursive="true" ;;
				--keyword) mode="keyword" ;;
			esac
		done
		args=(--data-urlencode "q=$query_str" --data-urlencode "mode=$mode" --data-urlencode "recursive=$recursive")
		[ -n "$folder_id" ] && args+=(--data-urlencode "folder_id=$folder_id")
		rest_get "/searchDocuments" "${args[@]}" | jq -r '
			if length == 0 then
				"(no results)"
			else
				.[] |
				"📄 \(.filename)  score=\(.score | tostring | .[0:5])  [\(.doc_id)]",
				(if (.title // "") != "" then "  title:       \(.title)" else empty end),
				(if (.description // "") != "" then "  description: \(.description | .[0:120] + (if length > 120 then "..." else "" end))" else empty end),
				""
			end'
		;;

	search-nodes)
		# Node-level search: BM25 over title + summary + text inside nodes.
		# Returns matching sections, each with its parent doc's filename.
		query_str="${1:?usage: search-nodes <query> <folder_id> [--recursive]}"
		folder_id="${2:?usage: search-nodes <query> <folder_id>}"
		recursive="false"
		[ "${3:-}" = "--recursive" ] && recursive="true"
		rest_get "/searchDocumentSections" \
			--data-urlencode "q=$query_str" \
			--data-urlencode "folder_id=$folder_id" \
			--data-urlencode "recursive=$recursive" | jq -r '
			if length == 0 then
				"(no results)"
			else
				.[].nodes // . | .[] |
				"● \(.title)  score=\(.score | tostring | .[0:5])  [\(.id)]",
				"  📄 \(.filename)  [\(.doc_id)]  p\(.page_start)-\(.page_end)",
				(if (.summary // "") != "" then "  ↳ \(.summary | .[0:120] + (if length > 120 then "..." else "" end))" else empty end),
				""
			end'
		;;

	poll)
		doc_id="${1:?usage: poll <doc_id> [interval_seconds] [max_minutes]}"
		interval="${2:-5}"
		max_min="${3:-10}"
		deadline=$(( $(date +%s) + max_min * 60 ))
		status="?"
		while [ "$(date +%s)" -lt "$deadline" ]; do
			resp=$(curl -s --get "$BASE/getDocumentById" --data-urlencode "id=$doc_id")
			status=$(echo "$resp" | jq -r '.status // "null"')
			printf '[%s] status=%s\n' "$(date +%H:%M:%S)" "$status"
			case "$status" in
				DONE)
					echo "--- DONE ---"
					echo "$resp" | jq '{id, filename, status, page_count, error}'
					exit 0
					;;
				FAILED)
					echo "--- FAILED ---"
					echo "$resp" | jq '{id, filename, status, error}'
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
		metadata_json="${3:-}"
		[ ! -f "$pdf" ] && { echo "error: file not found: $pdf" >&2; exit 1; }
		form_args=(-F "file=@$pdf" -F "folder_id=$folder_id")
		[ -n "$metadata_json" ] && form_args+=(-F "metadata=$metadata_json")
		curl -s -w "\nHTTP %{http_code}\n" "$BASE/uploadDocument" "${form_args[@]}" | jq . 2>/dev/null || true
		;;

	resummarize)
		doc_id="${1:?usage: resummarize <doc_id> [--force]}"
		force="false"
		[ "${2:-}" = "--force" ] && force="true"
		curl -s -X POST "$BASE/resummarizeDocument" -H "content-type: application/json" \
			-d "$(jq -n --arg d "$doc_id" --argjson f "$force" '{doc_id: $d, force: $f}')" | jq .
		;;

	*)
		echo "unknown command: $cmd" >&2
		exit 1
		;;
esac
