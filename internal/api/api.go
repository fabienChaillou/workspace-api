// Package api exposes the store through a Huma v2 REST API.
//
//	POST   /workspaces
//	GET    /workspaces                      filters + sort + pagination
//	GET    /workspaces/{workspaceId}        with actions and variables
//	PATCH  /workspaces/{workspaceId}
//	DELETE /workspaces/{workspaceId}        soft delete, cascades
//	POST   /workspaces/{workspaceId}/actions
//	GET    /workspaces/{workspaceId}/actions[/{actionId}]
//	PATCH  /workspaces/{workspaceId}/actions/{actionId}
//	DELETE /workspaces/{workspaceId}/actions/{actionId}
//	POST   /workspaces/{workspaceId}/variables
//	GET    /workspaces/{workspaceId}/variables[/{name}]
//	PATCH  /workspaces/{workspaceId}/variables/{name}
//	DELETE /workspaces/{workspaceId}/variables/{name}
//
// Actions and variables are only reachable under their workspace, so they
// cannot be created without a (live) workspace ID.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/example/workspace-api/internal/domain"
	"github.com/example/workspace-api/internal/store"
	"github.com/google/uuid"
)

// Config holds the API metadata.
type Config struct {
	Title   string
	Version string
	Logger  *slog.Logger
}

// NewHandler builds the HTTP handler (API + /healthz + /docs + /openapi.json).
func NewHandler(s store.Store, cfg Config) http.Handler {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig(cfg.Title, cfg.Version))
	Register(api, s, cfg.Logger)
	return mux
}

// Register adds every operation to api.
func Register(api huma.API, s store.Store, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	h := &handlers{store: s, log: logger}

	huma.Register(api, huma.Operation{
		OperationID: "health", Method: http.MethodGet, Path: "/healthz",
		Summary: "Liveness and database check", Tags: []string{"Health"},
	}, h.health)

	h.registerWorkspaces(api)
	h.registerActions(api)
	h.registerVariables(api)
}

type handlers struct {
	store store.Store
	log   *slog.Logger
}

type healthOutput struct {
	Body struct {
		Status string `json:"status" example:"ok"`
	}
}

func (h *handlers) health(ctx context.Context, _ *struct{}) (*healthOutput, error) {
	if err := h.store.Ping(ctx); err != nil {
		return nil, huma.Error503ServiceUnavailable("database unavailable")
	}
	out := &healthOutput{}
	out.Body.Status = "ok"
	return out, nil
}

// fail maps domain errors to RFC 9457 problem responses.
func (h *handlers) fail(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, domain.ErrConflict):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, domain.ErrInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return huma.Error503ServiceUnavailable("request canceled or timed out")
	default:
		h.log.ErrorContext(ctx, "internal error", "error", err)
		return huma.Error500InternalServerError("internal error")
	}
}

// parseID parses a path UUID. Huma already validated the format; this is a
// defensive second check.
func parseID(name, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, huma.Error422UnprocessableEntity(name + " must be a UUID")
	}
	return id, nil
}
