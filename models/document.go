package models

import "github.com/lib/pq"

// Document is the documents table row.
type Document struct {
	BaseUUIDModel
	Filename       string         `gorm:"not null" json:"filename"`
	FileKey        string         `gorm:"not null" json:"file_key"`
	Status         string         `gorm:"index;not null" json:"status"`
	PageCount      int            `json:"page_count"`
	Error          string         `json:"error"`
	DOI            string         `gorm:"index" json:"doi"`
	Indication     pq.StringArray `gorm:"type:text[]" json:"indication"`
	Study          pq.StringArray `gorm:"type:text[]" json:"study"`
	LiteratureType pq.StringArray `gorm:"type:text[]" json:"literature_type"`
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
