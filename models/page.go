package models

import "github.com/google/uuid"

// Page is the per-page markdown text of a document, extracted from the
// MinerU content_list at structuring time. Node.Text stays the serving
// content; pages are the page-granular ground truth that tree
// optimization (expand) slices when splitting over-long sections, and
// the reserve store for any future page-level serving. IDs are
// assigned by BaseUUIDModel's BeforeCreate at insert time.
type Page struct {
	BaseUUIDModel
	DocID   uuid.UUID `gorm:"index;type:uuid;uniqueIndex:idx_pages_doc_page" json:"doc_id"`
	PageIdx int       `gorm:"uniqueIndex:idx_pages_doc_page" json:"page_idx"` // 0-based, matches Node.PageStart/PageEnd
	Text    string    `json:"text"`
}

func (Page) TableName() string { return "pages" }

// PageText is the wire view of one page's text — page is 1-based.
type PageText struct {
	Page int    `json:"page"`
	Text string `json:"text"`
}
