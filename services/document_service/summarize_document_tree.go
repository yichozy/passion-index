package document_service

import (
	"context"
	"fmt"
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
	leafSummaryTimeout     = 90 * time.Second
	leafSummaryConcurrency = 8
)

// leafSummarySystemPrompt is the system prompt for leaf-node summarization.
const leafSummarySystemPrompt = "You are given a section of a document. Your task is to generate a concise description of the main points covered in this section, integrating information from both the text and the figures. Return the description only. Do not include any other text."

// SummarizeDocumentTree fills each leaf node's Summary in parallel with
// bounded concurrency. Non-leaf nodes and the synthetic root (NodeID="0000")
// are left untouched. image_urls maps Figure.Name → OSS signed URL (the
// LLM fetches bytes server-side). LLM failures are logged and leave
// Summary="" — they do not abort the pipeline.
func SummarizeDocumentTree(ctx context.Context, root *models.Node, image_urls map[string]string) {
	if root == nil || len(root.Nodes) == 0 {
		return
	}

	model := os.Getenv("PASSION_INDEX_LEAF_MODEL")
	if model == "" {
		model = llm_types.DeepSeekV4Flash
	}
	fallback := os.Getenv("PASSION_INDEX_LEAF_FALLBACK_MODEL")
	if fallback == "" {
		fallback = llm_types.DoubleSeedEvolving
	}

	g := gatlin.NewGroup(ctx, leafSummaryConcurrency)

	root.WalkLeaves(func(node *models.Node) {
		if node.Text == "" && len(node.Figures) == 0 {
			return
		}
		g.Go(func() error {
			// Build user prompt inline.
			var builder strings.Builder
			fmt.Fprintf(&builder, "Title: %s\n\n", node.Title)
			fmt.Fprintf(&builder, "Text: %s\n", node.Text)
			if len(node.Figures) > 0 {
				fmt.Fprintf(&builder, "\nFigures (%d total, identified by filename):\n", len(node.Figures))
				for _, fig := range node.Figures {
					caption := fig.Caption
					if caption == "" {
						caption = "(no caption)"
					}
					fmt.Fprintf(&builder, "- %s [caption: %s]\n", fig.Name, caption)
				}
			}

			// Collect image URLs (skip figures that never made it to OSS).
			var images []*llm_types.ImgArgs
			for _, figure := range node.Figures {
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

			resp, err := llm.LLMChat(ctx, llm_types.ChatKwargs{
				ModelName:    model,
				SystemPrompt: leafSummarySystemPrompt,
				UserPrompt:   builder.String(),
				UserImage:    images,
			}, fallback, nil, nil, nil, nil, leafSummaryTimeout)
			if err != nil {
				log.Warnf(ctx, "summary: leaf LLM failed (title=%q): %v", node.Title, err)
				return nil // swallow — pipeline continues, Summary stays ""
			}
			node.Summary = resp.Content
			return nil
		})
	})

	_ = g.Wait()
}
