package models

import "github.com/google/uuid"

// Folder is a document folder in a self-referential hierarchy.
// Root is implicit — folders with nil ParentID are top-level.
type Folder struct {
	BaseUUIDModel
	Name     string     `gorm:"not null" json:"name"`
	ParentID *uuid.UUID `gorm:"index;type:uuid" json:"parent_id"`

	// GORM relationships. Declared so the data graph is self-documenting and
	// Preload is available where it fits (e.g. single-valued lookups). No FK
	// constraints — integrity is enforced at the app layer; soft-delete +
	// OnDelete triggers don't compose cleanly.
	Parent    *Folder    `gorm:"foreignKey:ParentID;constraint:-" json:"-"`
	Children  []Folder   `gorm:"foreignKey:ParentID;constraint:-" json:"-"`
	Documents []Document `gorm:"foreignKey:FolderID;constraint:-" json:"-"`
}
