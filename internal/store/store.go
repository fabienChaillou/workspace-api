// Package store defines the persistence contract implemented by the pgx, gorm
// and in-memory stores.
//
// Semantics every implementation must honour (checked by storetest.Run):
//   - deletes are soft deletes; deleting a workspace soft-deletes its actions
//     and variables in the same transaction;
//   - actions and variables can only be created for an existing, non-deleted
//     workspace (domain.ErrNotFound otherwise);
//   - a variable name is unique among live variables (domain.ErrConflict);
//   - ListWorkspaces orders by the latest action UpdatedAt by default.
package store

import (
	"context"

	"github.com/example/workspace-api/internal/domain"
	"github.com/google/uuid"
)

// Store is the persistence contract used by the API layer.
type Store interface {
	CreateWorkspace(ctx context.Context, w domain.Workspace) (domain.Workspace, error)
	// GetWorkspace returns the workspace with its live actions (latest first)
	// and variables (by name).
	GetWorkspace(ctx context.Context, id uuid.UUID) (domain.Workspace, error)
	// ListWorkspaces expects options already normalized (opts.Normalize()).
	ListWorkspaces(ctx context.Context, opts domain.ListWorkspacesOptions) (domain.WorkspacePage, error)
	UpdateWorkspace(ctx context.Context, id uuid.UUID, p domain.WorkspacePatch) (domain.Workspace, error)
	DeleteWorkspace(ctx context.Context, id uuid.UUID) error

	CreateAction(ctx context.Context, a domain.Action) (domain.Action, error)
	GetAction(ctx context.Context, workspaceID, id uuid.UUID) (domain.Action, error)
	ListActions(ctx context.Context, workspaceID uuid.UUID) ([]domain.Action, error)
	UpdateAction(ctx context.Context, workspaceID, id uuid.UUID, p domain.ActionPatch) (domain.Action, error)
	DeleteAction(ctx context.Context, workspaceID, id uuid.UUID) error

	CreateVariable(ctx context.Context, v domain.Variable) (domain.Variable, error)
	GetVariable(ctx context.Context, workspaceID uuid.UUID, name string) (domain.Variable, error)
	ListVariables(ctx context.Context, workspaceID uuid.UUID) ([]domain.Variable, error)
	UpdateVariable(ctx context.Context, workspaceID uuid.UUID, name string, p domain.VariablePatch) (domain.Variable, error)
	DeleteVariable(ctx context.Context, workspaceID uuid.UUID, name string) error

	Ping(ctx context.Context) error
	Close() error
}
