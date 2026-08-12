package models

import (
	"github.com/google/uuid"
)

// Document is the documents table row.
type Document struct {
	BaseUUIDModel
	Filename  string         `gorm:"not null" json:"filename"`
	FileKey   string         `gorm:"not null" json:"file_key"`
	Status    string         `gorm:"index;not null" json:"status"`
	PageCount int            `json:"page_count"`
	Error     string         `json:"error"`
	Metadata  map[string]any `gorm:"type:jsonb;serializer:json" json:"metadata,omitempty"`
	FolderID  *uuid.UUID     `gorm:"index;type:uuid" json:"folder_id"`

	// GORM relationship — see Folder for the constraint policy.
	Folder *Folder `gorm:"foreignKey:FolderID;constraint:-" json:"-"`
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
