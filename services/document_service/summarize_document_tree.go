package document_service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/yichozy/hopebox/gatlin"
	"github.com/yichozy/hopebox/llm"
	"github.com/yichozy/hopebox/llm_types"
	"github.com/yichozy/hopebox/log"
	"github.com/yichozy/passion-index/models"
)

const (
	SUMMARY_TIMEOUT     = 180 * time.Second
	SUMMARY_CONCURRENCY = 8
	// SMALL_NODE_TEXT_CHAR_SIZE: leaves with Text shorter than this (and no figures)
	// reuse their own Text as Summary — the LLM would just rephrase it.
	// ~200 tokens, matches PageIndex's small_node_tokens threshold.
	SMALL_NODE_TEXT_CHAR_SIZE = 800
)

// LEAF_SUMMARY_PROMPT asks the LLM to summarize a leaf section's text + figures.
// Output is JSON with `points` (a CoT scaffold — discarded) and `summary`
// (what we keep). Prompt pattern borrowed from PageIndex.
//
// Placeholders (filled via strings.NewReplacer): {{ text }}.
const LEAF_SUMMARY_PROMPT = `You are given a text chunk from a document.
Your task is to generate a concise description of everything that is covered in the text, summarizing all its points without omitting any type of content.
Keep the description concise and to the point, avoiding unnecessary details.

Given Text: {{ text }}

Reply strictly in the following JSON format:
{
    "points": <a list of points covered in the text>,
    "summary": <a concise description of everything that is covered in the text>
}

Follow strictly the above JSON return format. Do not include any other text!`

// PARENT_SUMMARY_PROMPT asks the LLM to summarize a section from its own
// opening text (possibly empty) plus its subsections' titles + summaries.
// Same JSON output shape as leafSummaryPrompt.
//
// Placeholders: {{ title }}, {{ opening_text }}, {{ children }}.
const PARENT_SUMMARY_PROMPT = `You are given a section of a document: the text that opens the section (possibly empty) and the titles and summaries of its subsections.
Your task is to generate a concise description of everything that is covered in the whole section, summarizing all its points without omitting any type of content.
Keep the description concise and to the point, avoiding unnecessary details.

Section Title: {{ title }}

Opening Text: {{ opening_text }}

Subsection Titles and Summaries: {{ children }}

Reply strictly in the following JSON format:
{
    "points": <a list of points covered in the section>,
    "summary": <a concise description of everything that is covered in the section>
}

Follow strictly the above JSON return format. Do not include any other text!`

// SummarizeDocumentTree fills Summary on every node bottom-up, except the
// synthetic root. Leaves with short Text reuse it as the summary (no LLM
// call); intermediate nodes are summarized from their own Text + their
// children's summaries. LLM failures are logged and leave Summary="" —
// they do not abort the pipeline.
//
// Processing is level-by-level (deepest first) so each parent sees its
// children's summaries by the time it runs. Within a level, nodes run in
// parallel via a bounded gatlin group.
func SummarizeDocumentTree(ctx context.Context, root *models.Node, image_urls map[string]string) {
	if root == nil {
		return
	}

	model := os.Getenv("PASSION_INDEX_SUMMARY_MODEL")
	if model == "" {
		model = llm_types.DeepSeekV4Flash
	}
	fallback := os.Getenv("PASSION_INDEX_SUMMARY_FALLBACK_MODEL")
	if fallback == "" {
		fallback = llm_types.DoubleSeedEvolving
	}

	for _, level := range root.GroupByLevelBottomUp() {
		g := gatlin.NewGroup(ctx, SUMMARY_CONCURRENCY)
		for _, node := range level {
			// Incremental skip — node already has a summary, don't regenerate.
			if node.Summary != "" {
				continue
			}
			// Small leaf shortcut — text-only leaf with short Text: reuse as-is.
			if len(node.Nodes) == 0 && len(node.Figures) == 0 && len(node.Text) < SMALL_NODE_TEXT_CHAR_SIZE {
				node.Summary = strings.TrimSpace(node.Text)
				continue
			}
			node := node
			g.Go(func() error {
				summarizeNode(ctx, node, image_urls, model, fallback)
				return nil
			})
		}
		_ = g.Wait()
	}
}

func summarizeNode(ctx context.Context, node *models.Node, image_urls map[string]string, model, fallback string) {
	// reply is the JSON shape both prompts ask for. Points is a CoT scaffold
	// — the LLM produces it (which improves summary quality) but we never
	// read it. Summary is what gets stored on the node. The schema_description
	// tags are picked up by utils.GenerateSchema[reply]() and rendered into the
	// JSON schema passed to the LLM via StructureOutput.
	type reply struct {
		Points  []string `json:"points" jsonschema:"schema_description=List of main points covered in the text (enumerated before writing the summary)"`
		Summary string   `json:"summary" jsonschema:"schema_description=A concise description of everything covered in the text"`
	}

	var prompt string
	var images []*llm_types.ImgArgs

	if len(node.Nodes) == 0 {
		// Leaf — its own Text + Figures (figures attached as image URLs).
		prompt = strings.NewReplacer("{{ text }}", node.Text).Replace(LEAF_SUMMARY_PROMPT)
		for i := range node.Figures {
			figure := node.Figures[i]
			url, ok := image_urls[figure.Name]
			if !ok {
				log.Warnf(ctx, "summary: image url missing for: %s", figure.Name)
				continue
			}
			images = append(images, llm_types.NewImageArgsFromUrl(
				url,
				llm_types.WithFileName(figure.Name),
			))
		}
	} else {
		// Intermediate — own Text acts as "opening text" (the part of the
		// section not covered by any subsection) plus children's summaries.
		// Skip children whose Summary is "" (LLM failure or empty small-text
		// shortcut) so the parent prompt only sees what actually got
		// summarized.
		children_for_prompt := make([]map[string]string, 0, len(node.Nodes))
		for i := range node.Nodes {
			if node.Nodes[i].Summary == "" {
				continue
			}
			children_for_prompt = append(children_for_prompt, map[string]string{
				"title":   node.Nodes[i].Title,
				"summary": node.Nodes[i].Summary,
			})
		}
		listing, _ := json.Marshal(children_for_prompt)
		prompt = strings.NewReplacer(
			"{{ title }}", node.Title,
			"{{ opening_text }}", node.Text,
			"{{ children }}", string(listing),
		).Replace(PARENT_SUMMARY_PROMPT)
		// Parent doesn't see images directly — it composes from children.
	}

	resp, err := llm.LLMChatWithStructOutput[reply](ctx, llm_types.ChatKwargs{
		ModelName:  model,
		UserPrompt: prompt,
		UserImage:  images,
	}, fallback, nil, SUMMARY_TIMEOUT)
	if err != nil {
		log.Warnf(ctx, "summary: LLM failed (title=%q): %v", node.Title, err)
		return
	}
	node.Summary = strings.TrimSpace(resp.Summary)
}
