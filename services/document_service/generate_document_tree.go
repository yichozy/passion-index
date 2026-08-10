package document_service

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/yichozy/hopebox/aliyun"
	"github.com/yichozy/hopebox/log"
	"github.com/yichozy/hopebox/mineru_popo"
	"github.com/yichozy/hopebox/mineru_private"
	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"
)

// GenerateDocumentTree runs the document processing pipeline in the background.
// Status doubles as step indicator: OCR → STRUCTURING → SUMMARY → DONE.
// Return value signals step failure to the deferred FAILED-status update;
// callers fire-and-forget (goroutine) and discard it.
func GenerateDocumentTree(ctx context.Context, doc_id string) (err error) {
	doc, err := orm_document.GetDocumentByID(ctx, doc_id)
	if err != nil {
		log.Errorf(ctx, "pipeline[%s]: get document failed: %v", doc_id, err)
		return
	}

	defer func() {
		if err != nil {
			log.Errorf(ctx, "pipeline[%s]: failed: %v", doc_id, err)
			doc.Status = models.StatusFailed
			doc.Error = err.Error()
			orm_document.Update(ctx, &doc)
		}
	}()

	// Step 1: OCR — download PDF → MinerU → zip bytes
	orm_document.UpdateStatus(ctx, doc_id, models.StatusOCR)

	pdf_path := filepath.Join(os.TempDir(), doc_id+".pdf")
	oss, err := aliyun.NewOss()
	if err != nil {
		return fmt.Errorf("oss_init: %w", err)
	}
	if err = oss.Download(ctx, doc.FileKey, pdf_path); err != nil {
		return fmt.Errorf("oss_download: %w", err)
	}
	defer os.Remove(pdf_path)

	zip_bytes, err := mineru_private.NewClient().Process(ctx, pdf_path, map[string]string{
		"backend":             "hybrid-engine",
		"parse_method":        "ocr",
		"formula_enable":      "true",
		"table_enable":        "false",
		"language":            "en",
		"return_content_list": "true",
		"return_middle_json":  "true",
		"return_images":       "true",
		"response_format_zip": "true",
	})
	if err != nil {
		return fmt.Errorf("ocr: %w", err)
	}
	log.Infof(ctx, "pipeline[%s]: ocr done — %d bytes", doc_id, len(zip_bytes))

	// Step 2: Structuring — Popo build tree → map → persist
	orm_document.UpdateStatus(ctx, doc_id, models.StatusStructuring)

	pdf_bytes, err := os.ReadFile(pdf_path)
	if err != nil {
		return fmt.Errorf("read_pdf: %w", err)
	}

	popo_doc, err := mineru_popo.NewClient().Process(ctx, zip_bytes, pdf_bytes)
	if err != nil {
		return fmt.Errorf("structuring: %w", err)
	}

	root_node, page_count := ConvertPopoResultToTree(popo_doc)

	doc.PageCount = page_count
	orm_document.Update(ctx, &doc)
	log.Infof(ctx, "pipeline[%s]: structuring done — %d nodes, %d pages", doc_id, len(root_node.Nodes), page_count)

	// Step 3: Summary — LLM bottom-up node summaries
	orm_document.UpdateStatus(ctx, doc_id, models.StatusSummary)

	// Extract images from MinerU zip (<doc>/<subdir>/images/<hash>.jpg),
	// persist each to OSS at passion-index/<docID>/images/<name>, and store
	// the resulting signed URL for SummarizeDocumentTree to hand to the LLM.
	// Bytes are local to each iteration (GC'd immediately after upload) so
	// memory peak = one image, not the whole document.
	image_urls := map[string]string{}
	if reader, err := zip.NewReader(bytes.NewReader(zip_bytes), int64(len(zip_bytes))); err == nil {
		for _, f := range reader.File {
			if f.FileInfo().IsDir() {
				continue
			}
			if !strings.EqualFold(filepath.Base(filepath.Dir(f.Name)), "images") {
				continue
			}
			switch strings.ToLower(filepath.Ext(f.Name)) {
			case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp":
			default:
				continue
			}
			if closer, err := f.Open(); err == nil {
				data, read_err := io.ReadAll(closer)
				closer.Close()
				if read_err != nil {
					continue
				}
				name := filepath.Base(f.Name)
				oss_key := fmt.Sprintf("passion-index/%s/images/%s", doc_id, name)
				if upload_err := oss.UploadBytes(ctx, oss_key, data); upload_err != nil {
					log.Warnf(ctx, "pipeline[%s]: oss upload image %s: %v", doc_id, name, upload_err)
					continue
				}
				url, url_err := oss.GetObjectURL(ctx, oss_key)
				if url_err != nil {
					log.Warnf(ctx, "pipeline[%s]: oss sign url %s: %v", doc_id, name, url_err)
					continue
				}
				image_urls[name] = url
			}
		}
	}
	SummarizeDocumentTree(ctx, root_node, image_urls)

	log.Infof(ctx, "pipeline[%s]: summary done", doc_id)

	// Flatten the in-memory tree (with summaries) into node rows and persist.
	rows := root_node.FlattenTree(doc.ID)
	if err := orm_node.Create(ctx, rows); err != nil {
		return fmt.Errorf("insert nodes: %w", err)
	}

	// Done — update document metadata.
	doc.Status = models.StatusDone
	orm_document.Update(ctx, &doc)
	log.Infof(ctx, "pipeline[%s] done", doc_id)
	return nil
}
