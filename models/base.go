package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// BaseUUIDModel provides UUID v7 primary key + timestamps + soft delete.
// Embed in any gorm model that needs auto-generated UUID PK and soft delete.
type BaseUUIDModel struct {
	ID        uuid.UUID      `json:"id" gorm:"primaryKey;type:uuid"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// BeforeCreate auto-generates UUID v7 if not set.
func (m *BaseUUIDModel) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		m.ID = id
	}
	return nil
}
