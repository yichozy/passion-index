package document_search_service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/llm"
	"github.com/yichozy/hopebox/llm_types"
	"github.com/yichozy/hopebox/log"
)

// BEAM_TIMEOUT bounds a single rank attempt (per attempt — hopebox retries
// 3x with a fallback model, but a deadline-exceeded aborts immediately
// without retrying). Observed single-call latency reaches ~50s under
// provider slowdown, so 60s left no headroom; 120s bounds a slow attempt
// while keeping the worst case for a synchronous search reasonable.
const BEAM_TIMEOUT = 120 * time.Second

// BEAM_RANK_PROMPT asks the LLM to rank tree-node candidates by relevance
// to a user query. Output is JSON with ranked_ids (best-to-worst) and done
// (true if leaf reached or content already specific enough).
//
// Pattern borrowed from ConDB's contextdb/prompts/beam.jinja. Placeholders
// (filled via strings.NewReplacer, same pattern as the summary prompts in
// document_service): {{ query }}, {{ pick_limit }}, {{ candidates_block }}.
const BEAM_RANK_PROMPT = `You are ranking document tree nodes to find the most relevant section for a user question.

Query: {{ query }}

Candidates (rank up to {{ pick_limit }} ids, best first):
{{ candidates_block }}

Reply strictly in the following JSON format:
{
    "ranked_ids": ["<uuid1>", "<uuid2>", ...],
    "relevance": <integer 0-10, how well the top-ranked candidate answers the query>,
    "done": <true if reached leaf nodes or content specific enough to answer; false if high-level sections needing deeper exploration>
}

Follow strictly the above JSON return format. Do not include any other text!`

// rank_reply is the shared LLM reply contract for tree-node ranking,
// used by both beam search (RankCandidates) and block search. relevance
// is a 0-10 score for the top-ranked candidate (used by SearchDocuments
// to rank docs); done signals "evidence sufficient, stop descending".
type rank_reply struct {
	RankedIDs []string `json:"ranked_ids" jsonschema:"schema_description=IDs of candidate nodes in best-to-worst order, max pick_limit"`
	Relevance float64  `json:"relevance" jsonschema:"schema_description=Integer 0-10, how well the top-ranked candidate answers the query"`
	Done      bool     `json:"done" jsonschema:"schema_description=true if reached leaf nodes or content specific enough to answer; false if high-level sections needing deeper exploration"`
}

// call_ranker sends a ranking prompt to the LLM and parses the reply into
// UUIDs + relevance + done. Shared by beam and block search — same reply
// contract, same env-var model selection.
//
// Reuses PASSION_INDEX_SUMMARY_MODEL / PASSION_INDEX_SUMMARY_FALLBACK_MODEL
// env vars (same as the summary pipeline) — ranking is a similar-complexity
// task. hopebox/llm already retries 3 times and switches to the fallback
// model on the last attempt.
func call_ranker(ctx context.Context, prompt string) ([]uuid.UUID, float64, bool, error) {
	model := os.Getenv("PASSION_INDEX_SUMMARY_MODEL")
	if model == "" {
		model = llm_types.DeepSeekV4Flash
	}
	fallback := os.Getenv("PASSION_INDEX_SUMMARY_FALLBACK_MODEL")
	if fallback == "" {
		fallback = llm_types.DoubleSeedEvolving
	}

	resp, err := llm.LLMChatWithStructOutput[rank_reply](ctx, llm_types.ChatKwargs{
		ModelName:  model,
		UserPrompt: prompt,
		// Observability for the KV-cache story: block search relies on
		// DeepSeek's server-side prefix cache, and cached_tokens here is
		// the hit evidence (later blocks should show most of input cached).
		UsageHook: func(ctx context.Context, _ string, usage llm_types.Usage) error {
			log.Infof(ctx, "ranker usage: input=%d cached=%d output=%d",
				usage.InputTokens, usage.CachedTokens, usage.OutputTokens)
			return nil
		},
	}, fallback, nil, BEAM_TIMEOUT)
	if err != nil {
		return nil, 0, false, err
	}

	// LLMs occasionally return malformed UUIDs — skip those; only fail when
	// nothing usable survives.
	ids := make([]uuid.UUID, 0, len(resp.RankedIDs))
	for _, s := range resp.RankedIDs {
		if id, parse_err := uuid.Parse(s); parse_err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, 0, false, fmt.Errorf("LLM returned no valid UUIDs: %v", resp.RankedIDs)
	}
	return ids, resp.Relevance, resp.Done, nil
}

// RankCandidates asks the LLM to rank beam candidates for a query. Builds
// the prompt (each candidate renders as a 7-line block: id / title / summary
// / text / range / path / parent_context), calls the LLM, parses UUID
// strings into uuid.UUID. Returns the ranked IDs, a 0-10 relevance score
// for the top-ranked candidate (used by SearchDocuments to rank docs), and
// the done signal.
func RankCandidates(ctx context.Context, query string, candidates []BeamCandidate, pick_limit int) ([]uuid.UUID, float64, bool, error) {
	// Build the candidates block. Truncations keep the prompt bounded:
	// text to BEAM_TEXT_TRUNCATE chars, parent summary to
	// BEAM_PARENT_TRUNCATE chars.
	var block strings.Builder
	for _, c := range candidates {
		fmt.Fprintf(&block, "- id: %s\n", c.Node.ID)
		fmt.Fprintf(&block, "  title: %s\n", c.Node.Title)
		fmt.Fprintf(&block, "  summary: %s\n", c.Node.Summary)
		text := c.Node.Text
		if len(text) > BEAM_TEXT_TRUNCATE {
			text = text[:BEAM_TEXT_TRUNCATE]
		}
		fmt.Fprintf(&block, "  text: %s\n", text)
		fmt.Fprintf(&block, "  range: p%d-%d\n", c.Node.PageStart, c.Node.PageEnd)
		fmt.Fprintf(&block, "  path: %s\n", c.SectionPath)
		parent_ctx := c.ParentSummary
		if parent_ctx == "" {
			parent_ctx = "(root)"
		}
		if len(parent_ctx) > BEAM_PARENT_TRUNCATE {
			parent_ctx = parent_ctx[:BEAM_PARENT_TRUNCATE]
		}
		fmt.Fprintf(&block, "  parent_context: %s\n", parent_ctx)
	}
	prompt := strings.NewReplacer(
		"{{ query }}", query,
		"{{ pick_limit }}", strconv.Itoa(pick_limit),
		"{{ candidates_block }}", block.String(),
	).Replace(BEAM_RANK_PROMPT)

	return call_ranker(ctx, prompt)
}
