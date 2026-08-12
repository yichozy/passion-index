package document_service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/aliyun"
	"github.com/yichozy/hopebox/log"
	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"
)

// ReSummarizeDocumentTree regenerates summaries for an existing document.
//
//	force=false → re-summarize every node whose Summary is "" (retry failed
//	              LLM calls) AND every ancestor of such nodes (because their
//	              existing summary was built from incomplete children data).
//	              Subtrees with no failure are left untouched.
//	force=true  → clear all summaries first; every node is re-summarized.
//
// Does NOT re-run OCR or structuring — only the summary step. Images
// already in OSS (uploaded during the original pipeline) are re-signed and
// handed back to the LLM.
//
// Status transitions mirror the original pipeline's SUMMARY phase so
// pollers can detect the re-summarize in progress:
//
//	DONE → SUMMARY (start) → DONE (success) / FAILED (any error)
//
// Designed to run in a goroutine so the resolver returns immediately; the
// caller passes context.WithoutCancel(ctx) so the work outlives the HTTP
// request.
func ReSummarizeDocumentTree(ctx context.Context, doc_id uuid.UUID, force bool) (err error) {
	defer func() {
		if err != nil {
			log.Errorf(ctx, "resummarize[%s]: failed: %v", doc_id, err)
			if e := orm_document.UpdateStatus(ctx, doc_id, models.StatusFailed); e != nil {
				log.Errorf(ctx, "resummarize[%s]: failed to record FAILED status: %v", doc_id, e)
			}
		}
	}()

	rows, err := orm_node.GetByDocID(ctx, doc_id)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("document %s not found or has no tree", doc_id)
	}

	// Mark in-progress so pollers see the transition immediately.
	if e := orm_document.UpdateStatus(ctx, doc_id, models.StatusSummary); e != nil {
		return fmt.Errorf("set status SUMMARY: %w", e)
	}

	root := models.AssembleTree(rows)
	levels := root.GroupByLevelBottomUp()

	// Clear summaries that need to be regenerated. SummarizeDocumentTree
	// (called below) skips nodes with non-empty Summary, so clearing is
	// how we tell it what to (re)process.
	//
	//   force=true  → clear every Summary
	//   force=false → clear a node's Summary only when one of its children
	//                 has an empty Summary. Bottom-up iteration means a
	//                 cleared parent propagates the empty state to its own
	//                 parent on the next level, so a single failed leaf
	//                 clears its whole ancestor chain — but a fully
	//                 successful subtree is left alone.
	for _, level := range levels {
		for _, node := range level {
			if force {
				node.Summary = ""
				continue
			}
			if node.Summary == "" {
				continue // already empty (failed leaf or just-cleared ancestor)
			}
			for i := range node.Nodes {
				if node.Nodes[i].Summary == "" {
					node.Summary = ""
					break
				}
			}
		}
	}

	// Re-sign OSS URLs for every figure (images were uploaded at a fixed
	// key path during the original pipeline).
	oss, err := aliyun.NewOss()
	if err != nil {
		return fmt.Errorf("init oss: %w", err)
	}
	image_urls := map[string]string{}
	for _, level := range levels {
		for _, node := range level {
			for i := range node.Figures {
				name := node.Figures[i].Name
				key := fmt.Sprintf("passion-index/%s/images/%s", doc_id, name)
				url, url_err := oss.GetObjectURL(ctx, key)
				if url_err != nil {
					log.Warnf(ctx, "resummarize[%s]: image url missing for %s: %v", doc_id, name, url_err)
					continue
				}
				image_urls[name] = url
			}
		}
	}

	SummarizeDocumentTree(ctx, root, image_urls)

	// Persist updated summaries. Per-node update — N rows, but re-summarize
	// is an admin operation, not a hot path.
	for _, level := range levels {
		for _, node := range level {
			if err := orm_node.UpdateSummary(ctx, doc_id, node.ID, node.Summary); err != nil {
				log.Warnf(ctx, "resummarize[%s]: failed to persist node %d: %v", doc_id, node.ID, err)
			}
		}
	}

	if e := orm_document.UpdateStatus(ctx, doc_id, models.StatusDone); e != nil {
		return fmt.Errorf("set status DONE: %w", e)
	}
	log.Infof(ctx, "resummarize[%s]: done (force=%v)", doc_id, force)
	return nil
}
