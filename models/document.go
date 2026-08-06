// Package models defines passion-index's data structures.
//
// Document is a gorm model (documents table row); Node + Figure
// are the recursive tree structure serialized into Document.Tree (JSONB).
package models

import (
	"time"
)

// Document is the documents table row.
//
// Status is uppercase to match GraphQL DocStatus enum — required for
// utils.CopyObj (JSON roundtrip) to correctly unmarshal into gqlgen's
// types.DocStatus.
//
// Tree stores the serialized root Node (NodeID="0000") as JSONB. The root
// node's children are the actual document sections.
type Document struct {
	DocID     string    `gorm:"primaryKey" json:"doc_id"`
	Filename  string    `gorm:"not null" json:"filename"`
	FileKey   string    `gorm:"not null" json:"file_key"`     // OSS object key for the PDF
	Status    string    `gorm:"index;not null" json:"status"` // uppercase: PROCESSING / COMPLETED / FAILED
	PageCount int       `json:"page_count"`
	Error     string    `json:"error"`
	Tree      *Node     `gorm:"type:jsonb;serializer:json;index:idx_documents_tree,type:gin" json:"tree"`
	CreatedAt time.Time `gorm:"index:idx_documents_created,sort:desc" json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
