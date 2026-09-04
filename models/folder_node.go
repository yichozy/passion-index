package models

import "github.com/google/uuid"

// FolderNode is the structured folder-tree view: nested children plus
// batched aggregate counts, assembled by folder_service.GetFolderTree.
type FolderNode struct {
	ID            uuid.UUID     `json:"id"`
	Name          string        `json:"name"`
	ParentID      *uuid.UUID    `json:"parent_id,omitempty"`
	DocumentCount int           `json:"document_count"`
	FolderCount   int           `json:"folder_count"`
	Folders       []*FolderNode `json:"folders"`
}
