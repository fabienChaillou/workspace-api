// Package domain contains the business types shared by the API layer and the
// storage implementations (pgx, gorm, in-memory). It has no dependency on any
// database driver.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors. Stores wrap them with context (fmt.Errorf("...: %w", ErrX))
// and the API layer maps them to HTTP status codes.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrInvalid  = errors.New("invalid")
)

// WorkspaceStatus is the lifecycle status of a workspace.
type WorkspaceStatus string

const (
	WorkspaceStatusActive   WorkspaceStatus = "ACTIVE"
	WorkspaceStatusFailure  WorkspaceStatus = "FAILURE"
	WorkspaceStatusInactive WorkspaceStatus = "INACTIVE"
	WorkspaceStatusDeleted  WorkspaceStatus = "DELETED"
)

// WorkspaceStatuses lists every valid workspace status.
var WorkspaceStatuses = []WorkspaceStatus{
	WorkspaceStatusActive, WorkspaceStatusFailure, WorkspaceStatusInactive, WorkspaceStatusDeleted,
}

// Valid reports whether s is a known workspace status.
func (s WorkspaceStatus) Valid() bool {
	for _, v := range WorkspaceStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// ActionType is one of the base Terraform/OpenTofu commands.
type ActionType string

const (
	ActionTypeInit    ActionType = "init"
	ActionTypePlan    ActionType = "plan"
	ActionTypeApply   ActionType = "apply"
	ActionTypeDestroy ActionType = "destroy"
)

// ActionTypes lists every valid action type.
var ActionTypes = []ActionType{ActionTypeInit, ActionTypePlan, ActionTypeApply, ActionTypeDestroy}

// Valid reports whether t is a known action type.
func (t ActionType) Valid() bool {
	for _, v := range ActionTypes {
		if t == v {
			return true
		}
	}
	return false
}

// EnvVars is one environment variable of a workspace. A workspace stores a
// list of them, serialized as JSON in the workspaces.env_vars column.
type EnvVars struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Secure bool   `json:"secure"`
	Hidden bool   `json:"hidden"`
}

// Workspace is the aggregate root: it owns Actions and Variables.
type Workspace struct {
	ID           uuid.UUID
	Name         string
	VcsURL       string
	VcsKeyStr    string
	VcsBranch    string
	VcsDirectory string
	Status       WorkspaceStatus
	EnvVars      []EnvVars
	Namespace    string
	TofuVersion  string
	Actions      []Action   // filled by GetWorkspace only
	Variables    []Variable // filled by GetWorkspace only
	LastActionAt *time.Time // max(updated_at) of the workspace's live actions, nil if none
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Action is a Terraform/OpenTofu command run on a workspace.
type Action struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Type        ActionType
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Variable belongs to a workspace. Name is unique across all live variables.
type Variable struct {
	WorkspaceID uuid.UUID
	Name        string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// WorkspacePatch holds the fields to change; nil means "leave unchanged".
type WorkspacePatch struct {
	Name         *string
	VcsURL       *string
	VcsKeyStr    *string
	VcsBranch    *string
	VcsDirectory *string
	Status       *WorkspaceStatus
	EnvVars      *[]EnvVars
	Namespace    *string
	TofuVersion  *string
}

// ActionPatch holds the action fields to change; nil means "leave unchanged".
type ActionPatch struct {
	Type   *ActionType
	Status *string
}

// VariablePatch holds the variable fields to change; nil means "leave unchanged".
type VariablePatch struct {
	Status *string
}
