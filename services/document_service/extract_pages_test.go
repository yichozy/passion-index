package document_service

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/google/uuid"
)

func content_list_zip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestExtractPagesGroupsByPageIdx(t *testing.T) {
	zip_bytes := content_list_zip(t, map[string]string{
		"doc/auto/doc_content_list.json": `[
			{"type":"text","text":"first page body","page_idx":0},
			{"type":"image","img_path":"images/a.jpg","img_caption":["Figure 1: enrollment"],"page_idx":1},
			{"type":"table","table_caption":["Table 1: outcomes"],"page_idx":1},
			{"type":"text","text":"third page","page_idx":2},
			{"type":"text","text":"missing page_idx"},
			{"type":"text","text":"negative","page_idx":-1}
		]`,
	})
	doc_id := uuid.New()
	pages, err := ExtractPages(zip_bytes, doc_id)
	if err != nil {
		t.Fatalf("ExtractPages: %v", err)
	}
	if len(pages) != 3 {
		t.Fatalf("want 3 pages, got %d", len(pages))
	}
	want_texts := []string{"first page body", "Figure 1: enrollment\nTable 1: outcomes", "third page"}
	for i := range pages {
		if pages[i].PageIdx != i {
			t.Errorf("page %d idx = %d", i, pages[i].PageIdx)
		}
		if pages[i].Text != want_texts[i] {
			t.Errorf("page %d text = %q, want %q", i, pages[i].Text, want_texts[i])
		}
		if pages[i].DocID != doc_id {
			t.Errorf("page %d missing doc id: %+v", i, pages[i])
		}
	}
}

func TestExtractPagesSkipsBlankPages(t *testing.T) {
	zip_bytes := content_list_zip(t, map[string]string{
		"x_content_list.json": `[
			{"type":"text","text":"a","page_idx":0},
			{"type":"image","img_path":"images/b.jpg","page_idx":1},
			{"type":"text","text":"c","page_idx":2}
		]`,
	})
	pages, err := ExtractPages(zip_bytes, uuid.New())
	if err != nil {
		t.Fatalf("ExtractPages: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("image-only page 1 must be absent, got %d pages", len(pages))
	}
	if pages[1].PageIdx != 2 || pages[1].Text != "c" {
		t.Errorf("hole indexing broken: %+v", pages[1])
	}
}

func TestExtractPagesWithoutContentList(t *testing.T) {
	zip_bytes := content_list_zip(t, map[string]string{
		"doc/auto/images/a.jpg": "jpeg-bytes",
	})
	pages, err := ExtractPages(zip_bytes, uuid.New())
	if err != nil {
		t.Fatalf("missing content_list must not error: %v", err)
	}
	if len(pages) != 0 {
		t.Fatalf("want no pages, got %d", len(pages))
	}
}
