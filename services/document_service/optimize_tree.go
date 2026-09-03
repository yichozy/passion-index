package document_service

// Tree optimization — the expand half of PageIndex's tree_optimize.py:
// split over-long leaf sections into subsections so beam/block landing
// precision stays high. (The merge half was evaluated and dropped
// 2026-09-02: same-page siblings in our Popo trees carry genuinely
// distinct Text — merging loses real content, and wiping subtrees
// editorializes the document's true structure that structure-first
// agents navigate by.)
//
// Runs on the in-memory tree in GenerateDocumentTree BEFORE
// summarization/persist/embedding, so summaries and embeddings always
// see the final shape. Children own their page-sliced Text; the parent
// keeps only its opening pages. Splitting is monotonic — expanded
// parents stop being leaves, so no bookkeeping is needed to prevent
// undoing. Best-effort: LLM failures skip nodes, the pipeline never
// fails here (same contract as SummarizeDocumentTree).

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/gatlin"
	"github.com/yichozy/hopebox/llm"
	"github.com/yichozy/hopebox/llm_types"
	"github.com/yichozy/hopebox/log"
	"github.com/yichozy/passion-index/models"
)

const (
	// TRIGGER_PAGES: only look at expanding leaves spanning more pages.
	TRIGGER_PAGES = 5
	// EXPAND_CONCURRENCY bounds parallel LLM expand calls.
	EXPAND_CONCURRENCY = 32
	// PAGE_CHARS caps the per-page text handed to the model in prompts.
	PAGE_CHARS = 6000
	// OPTIMIZE_ROUNDS: expand children can themselves be over-long, so a
	// later round splits them too. Rounds without targets break early.
	OPTIMIZE_ROUNDS = 3
	// EXPAND_TIMEOUT per LLM expand call.
	EXPAND_TIMEOUT = 120 * time.Second
)

// EXPAND_PROMPT is ported verbatim from tree_optimize.py. Page numbers
// are 1-based for the LLM; results normalize to 0-based.
const EXPAND_PROMPT = `You are splitting an over-long section of a PDF into its subsections.

Section title: %s
Pages: %d-%d

%s

List the subsection headings that BEGIN within these pages, in document order,
each with the page number it begins on. Rules:

- Use only headings printed in the document. Never invent or paraphrase one.
- A running header, a table column label, a table row label, or a cross-reference
  is not a subsection heading.
- If this section is continuous prose, or a single table spanning the pages,
  return an empty list. That is a valid and expected answer.
- Do not include the section's own title.

Reply with JSON only:
{"subsections": [{"title": "<verbatim heading>", "page": <int>}]}`

// subsection is one LLM-proposed subsection heading. On the wire (the
// JSON schema handed to the model) Page is 1-based as returned; the
// filtered survivors normalize it to 0-based in place.
type subsection struct {
	Title string `json:"title"`
	Page  int    `json:"page" jsonschema:"schema_description=The 1-based page number the heading begins on"`
}

// expandReply is the structured LLM output.
type expandReply struct {
	Subsections []subsection `json:"subsections" jsonschema:"schema_description=Verbatim subsection headings that begin within the section, in document order"`
}

// OptimizeTree mutates root in place: split every over-long leaf
// (span > TRIGGER_PAGES) into subsections via one LLM heading proposal
// per node, then repeat — expand children can themselves be over-long —
// until nothing is left to split. pages may be empty (extraction failed
// or the doc has no page text) — the whole step is skipped. LLM
// failures log and skip the node.
func OptimizeTree(ctx context.Context, root *models.Node, pages []models.Page) {
	if root == nil || len(pages) == 0 {
		return
	}

	model := os.Getenv("PASSION_INDEX_TREE_OPTIMIZE_MODEL")
	if model == "" {
		model = llm_types.DeepSeekV4Flash
	}
	fallback := os.Getenv("PASSION_INDEX_TREE_OPTIMIZE_FALLBACK_MODEL")
	if fallback == "" {
		fallback = llm_types.DoubleSeedEvolving
	}

	// Densify page rows into a 0-based text index (holes are empty).
	max_idx := -1
	for i := range pages {
		if pages[i].PageIdx > max_idx {
			max_idx = pages[i].PageIdx
		}
	}
	page_texts := make([]string, max_idx+1)
	for i := range pages {
		page_texts[pages[i].PageIdx] = pages[i].Text
	}

	before := countNodes(root)
	for round := 0; round < OPTIMIZE_ROUNDS; round++ {
		var targets []*models.Node
		var walk func(n *models.Node)
		walk = func(n *models.Node) {
			if len(n.Nodes) == 0 && n.PageEnd-n.PageStart+1 > TRIGGER_PAGES {
				targets = append(targets, n)
			}
			for i := range n.Nodes {
				walk(&n.Nodes[i])
			}
		}
		walk(root)
		if len(targets) == 0 {
			break // converged — no new nodes, later rounds find nothing
		}

		g := gatlin.NewGroup(ctx, EXPAND_CONCURRENCY)
		for _, node := range targets {
			g.Go(func() error {
				// Render the section's pages: 1-based labels, PAGE_CHARS each.
				var pages_builder strings.Builder
				for page := node.PageStart; page <= node.PageEnd; page++ {
					if page >= len(page_texts) {
						continue
					}
					text := page_texts[page]
					if len(text) > PAGE_CHARS {
						text = text[:PAGE_CHARS]
					}
					if text == "" {
						continue
					}
					fmt.Fprintf(&pages_builder, "[Page %d]\n%s\n\n", page+1, text)
				}
				if pages_builder.Len() == 0 {
					return nil
				}
				prompt := fmt.Sprintf(EXPAND_PROMPT, node.Title,
					node.PageStart+1, node.PageEnd+1, pages_builder.String())

				resp, err := llm.LLMChatWithStructOutput[expandReply](ctx, llm_types.ChatKwargs{
					ModelName:  model,
					UserPrompt: prompt,
				}, fallback, nil, EXPAND_TIMEOUT)
				if err != nil {
					log.Warnf(ctx, "tree optimize: expand LLM failed (title=%q): %v", node.Title, err)
					return nil
				}

				// Filter hallucinated proposals: outside the section's page
				// range, not verbatim on the claimed page, equal to the
				// section's own title, duplicate, or out of document order.
				// Survivors carry a normalized 0-based Page from here on.
				headings := make([]subsection, 0, len(resp.Subsections))
				last_page := -1
				seen_titles := map[string]bool{}
				for i := range resp.Subsections {
					title := strings.TrimSpace(resp.Subsections[i].Title)
					start := resp.Subsections[i].Page - 1
					if title == "" || title == node.Title {
						continue
					}
					if start < node.PageStart || start > node.PageEnd {
						continue
					}
					if start >= len(page_texts) || !strings.Contains(page_texts[start], title) {
						continue
					}
					if start < last_page || seen_titles[title] {
						continue
					}
					last_page = start
					seen_titles[title] = true
					headings = append(headings, subsection{Title: title, Page: start})
				}
				if len(headings) == 0 {
					return nil
				}

				children := make([]models.Node, len(headings))
				for i := range headings {
					// Boundary rule: if the next heading is the first line of
					// its page, this child ends one page earlier (no page is
					// shared); otherwise the page is shared. The last child
					// takes the section's end. Unknown pages split strictly.
					end := node.PageEnd
					if i < len(headings)-1 {
						next_start := headings[i+1].Page
						shares_page := false
						if next_start < len(page_texts) {
							first_line := page_texts[next_start]
							if idx := strings.IndexByte(first_line, '\n'); idx >= 0 {
								first_line = first_line[:idx]
							}
							shares_page = !strings.Contains(first_line, headings[i+1].Title)
						}
						if shares_page {
							end = next_start
						} else {
							end = next_start - 1
						}
					}
					children[i] = models.Node{
						ID:        uuid.Must(uuid.NewV7()),
						DocID:     node.DocID,
						Title:     headings[i].Title,
						PageStart: headings[i].Page,
						PageEnd:   end,
						Text:      slicePagesText(page_texts, headings[i].Page, end),
					}
				}
				// Parent keeps only its opening pages (before the first
				// heading) as Text — everything else now lives in children.
				node.Text = slicePagesText(page_texts, node.PageStart, headings[0].Page-1)
				node.Nodes = children
				return nil
			})
		}
		_ = g.Wait()
	}
	log.Infof(ctx, "tree optimize (expand): %d → %d nodes", before, countNodes(root))
}

// slicePagesText joins the page texts of a 0-based inclusive range.
func slicePagesText(page_texts []string, start, end int) string {
	if start > end {
		return ""
	}
	var parts []string
	for page := start; page <= end; page++ {
		if page < len(page_texts) && page_texts[page] != "" {
			parts = append(parts, page_texts[page])
		}
	}
	return strings.Join(parts, "\n\n")
}

// countNodes totals real (non-synthetic-root) nodes.
func countNodes(root *models.Node) int {
	count := 0
	var walk func(n *models.Node)
	walk = func(n *models.Node) {
		for i := range n.Nodes {
			count++
			walk(&n.Nodes[i])
		}
	}
	walk(root)
	return count
}
