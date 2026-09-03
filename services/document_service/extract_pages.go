package document_service

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/yichozy/passion-index/models"
)

// content_item is one block of MinerU's <name>_content_list.json. Only
// the fields we aggregate into page text are captured; page_idx is
// 0-based in content_list, matching Node.PageStart/PageEnd. A nil
// PageIdx (or a negative one) skips the block.
type content_item struct {
	Text         string   `json:"text"`
	ImgCaption   []string `json:"img_caption"`
	TableCaption []string `json:"table_caption"`
	PageIdx      *int     `json:"page_idx"`
}

// ExtractPages pulls per-page markdown out of the MinerU result zip's
// *_content_list.json. Text blocks and image/table captions are
// concatenated per page in document order; blank pages stay absent
// (consumers index by PageIdx). A zip without a content list yields an
// empty slice, not an error — tree optimization degrades gracefully,
// the pipeline never fails here.
func ExtractPages(zip_bytes []byte, doc_id uuid.UUID) ([]models.Page, error) {
	reader, err := zip.NewReader(bytes.NewReader(zip_bytes), int64(len(zip_bytes)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	for _, f := range reader.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, "_content_list.json") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f.Name, err)
		}

		var items []content_item
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, fmt.Errorf("parse %s: %w", f.Name, err)
		}

		// Aggregate items into per-page text, growing the index as pages appear.
		var texts []string
		ensure := func(page_idx int) {
			for len(texts) <= page_idx {
				texts = append(texts, "")
			}
		}
		for _, item := range items {
			if item.PageIdx == nil || *item.PageIdx < 0 {
				continue
			}
			ensure(*item.PageIdx)
			var parts []string
			if item.Text != "" {
				parts = append(parts, item.Text)
			}
			parts = append(parts, item.ImgCaption...)
			parts = append(parts, item.TableCaption...)
			if len(parts) > 0 {
				if texts[*item.PageIdx] != "" {
					texts[*item.PageIdx] += "\n"
				}
				texts[*item.PageIdx] += strings.Join(parts, "\n")
			}
		}

		pages := make([]models.Page, 0, len(texts))
		for idx := range texts {
			if texts[idx] == "" {
				continue
			}
			pages = append(pages, models.Page{
				DocID:   doc_id,
				PageIdx: idx,
				Text:    texts[idx],
			})
		}
		return pages, nil
	}
	return nil, nil
}
