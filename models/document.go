package models

// Document is the documents table row (metadata only — tree nodes live in
// the nodes table, linked by doc_id).
type Document struct {
	BaseUUIDModel
	Filename  string `gorm:"not null" json:"filename"`
	FileKey   string `gorm:"not null" json:"file_key"`
	Status    string `gorm:"index;not null" json:"status"`
	PageCount int    `json:"page_count"`
	Error     string `json:"error"`
}

// Document status constants — status doubles as the current pipeline step.
// Flow: PENDING → OCR → STRUCTURING → SUMMARY → DONE (or FAILED at any step).
const (
	StatusPending     = "PENDING"
	StatusOCR         = "OCR"
	StatusStructuring = "STRUCTURING"
	StatusSummary     = "SUMMARY"
	StatusDone        = "DONE"
	StatusFailed      = "FAILED"
)
