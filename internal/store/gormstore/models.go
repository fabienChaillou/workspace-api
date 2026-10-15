package gormstore

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Workspace is the gorm model of the workspaces table. EnvVars holds the JSON
// serialization of []domain.EnvVars (jsonb column).
type Workspace struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name         string
	VcsUrl       string
	VcsKeyStr    string
	VcsBranch    string
	VcsDirectory string
	Status       string
	EnvVars      string `gorm:"type:jsonb"`
	Namespace    string
	TofuVersion  string
	Actions      []Action   `gorm:"foreignKey:WorkspaceID;constraint:OnDelete:CASCADE"`
	Variables    []Variable `gorm:"foreignKey:WorkspaceID;constraint:OnDelete:CASCADE"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

// BeforeCreate assigns a UUID when none is set.
func (w *Workspace) BeforeCreate(*gorm.DB) error {
	if w.ID == uuid.Nil {
		w.ID = uuid.New()
	}
	return nil
}

// Action is the gorm model of the actions table.
type Action struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID uuid.UUID `gorm:"type:uuid;not null"`
	Type        string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

// BeforeCreate assigns a UUID when none is set.
func (a *Action) BeforeCreate(*gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}

// Variable is the gorm model of the variables table. Name is unique among
// live rows (partial unique index uq_variables_name).
type Variable struct {
	WorkspaceID uuid.UUID `gorm:"type:uuid;not null"`
	Name        string    `gorm:"not null"`
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}
