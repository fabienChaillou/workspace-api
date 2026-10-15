package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/example/workspace-api/internal/domain"
)

type WorkspacePath struct {
	WorkspaceID string `path:"workspaceId" format:"uuid" doc:"Workspace ID"`
}

type createWorkspaceInput struct {
	Body struct {
		Name         string   `json:"name" minLength:"1" maxLength:"255"`
		VcsURL       string   `json:"vcsUrl,omitempty" maxLength:"2048"`
		VcsKeyStr    string   `json:"vcsKeyStr,omitempty"`
		VcsBranch    string   `json:"vcsBranch,omitempty" maxLength:"255"`
		VcsDirectory string   `json:"vcsDirectory,omitempty" maxLength:"1024"`
		Status       string   `json:"status,omitempty" enum:"ACTIVE,FAILURE,INACTIVE,DELETED" default:"ACTIVE"`
		EnvVars      []EnvVar `json:"envVars,omitempty" maxItems:"500"`
		Namespace    string   `json:"namespace,omitempty" maxLength:"255"`
		TofuVersion  string   `json:"tofuVersion,omitempty" maxLength:"64"`
	}
}

type workspaceDetailOutput struct {
	Location string `header:"Location"`
	Body     WorkspaceDetail
}

type listWorkspacesInput struct {
	Name       string `query:"name" maxLength:"255" doc:"Case-insensitive substring of the workspace name"`
	Status     string `query:"status" enum:"ACTIVE,FAILURE,INACTIVE,DELETED" doc:"Exact workspace status"`
	Namespace  string `query:"namespace" maxLength:"255" doc:"Exact namespace"`
	ActionType string `query:"actionType" enum:"init,plan,apply,destroy" doc:"Workspaces having at least one action of this type"`
	Sort       string `query:"sort" enum:"actions_updated_at,updated_at,created_at,name" default:"actions_updated_at" doc:"Sort field; by default the most recent action update"`
	Order      string `query:"order" enum:"asc,desc" default:"desc"`
	Page       int    `query:"page" minimum:"1" default:"1" doc:"1-based page number"`
	Limit      int    `query:"limit" minimum:"1" maximum:"100" default:"10" doc:"Page size"`
}

type listWorkspacesOutput struct {
	Body struct {
		Items      []Workspace       `json:"items"`
		Pagination domain.Pagination `json:"pagination"`
	}
}

type updateWorkspaceInput struct {
	WorkspacePath
	Body struct {
		Name         *string   `json:"name,omitempty" minLength:"1" maxLength:"255"`
		VcsURL       *string   `json:"vcsUrl,omitempty" maxLength:"2048"`
		VcsKeyStr    *string   `json:"vcsKeyStr,omitempty"`
		VcsBranch    *string   `json:"vcsBranch,omitempty" maxLength:"255"`
		VcsDirectory *string   `json:"vcsDirectory,omitempty" maxLength:"1024"`
		Status       *string   `json:"status,omitempty" enum:"ACTIVE,FAILURE,INACTIVE,DELETED"`
		EnvVars      *[]EnvVar `json:"envVars,omitempty" maxItems:"500" doc:"Replaces the whole list"`
		Namespace    *string   `json:"namespace,omitempty" maxLength:"255"`
		TofuVersion  *string   `json:"tofuVersion,omitempty" maxLength:"64"`
	}
}

func (h *handlers) registerWorkspaces(api huma.API) {
	tags := []string{"Workspaces"}

	huma.Register(api, huma.Operation{
		OperationID: "create-workspace", Method: http.MethodPost, Path: "/workspaces",
		Summary: "Create a workspace", Tags: tags, DefaultStatus: http.StatusCreated,
	}, h.createWorkspace)

	huma.Register(api, huma.Operation{
		OperationID: "list-workspaces", Method: http.MethodGet, Path: "/workspaces",
		Summary:     "List workspaces",
		Description: "Ordered by default by the most recent action update (workspaces without actions last). Paginated, 10 items per page by default.",
		Tags:        tags,
	}, h.listWorkspaces)

	huma.Register(api, huma.Operation{
		OperationID: "get-workspace", Method: http.MethodGet, Path: "/workspaces/{workspaceId}",
		Summary: "Get a workspace with its actions and variables", Tags: tags,
	}, h.getWorkspace)

	huma.Register(api, huma.Operation{
		OperationID: "update-workspace", Method: http.MethodPatch, Path: "/workspaces/{workspaceId}",
		Summary: "Partially update a workspace", Tags: tags,
	}, h.updateWorkspace)

	huma.Register(api, huma.Operation{
		OperationID: "delete-workspace", Method: http.MethodDelete, Path: "/workspaces/{workspaceId}",
		Summary: "Delete a workspace with its actions and variables", Tags: tags,
		DefaultStatus: http.StatusNoContent,
	}, h.deleteWorkspace)
}

func (h *handlers) createWorkspace(ctx context.Context, in *createWorkspaceInput) (*workspaceDetailOutput, error) {
	b := in.Body
	env := toEnvVars(b.EnvVars)
	if err := domain.ValidateEnvVars(env); err != nil {
		return nil, h.fail(ctx, err)
	}
	w, err := h.store.CreateWorkspace(ctx, domain.Workspace{
		Name: b.Name, VcsURL: b.VcsURL, VcsKeyStr: b.VcsKeyStr, VcsBranch: b.VcsBranch,
		VcsDirectory: b.VcsDirectory, Status: domain.WorkspaceStatus(b.Status), EnvVars: env,
		Namespace: b.Namespace, TofuVersion: b.TofuVersion,
	})
	if err != nil {
		return nil, h.fail(ctx, err)
	}
	return &workspaceDetailOutput{Location: "/workspaces/" + w.ID.String(), Body: fromWorkspaceDetail(w)}, nil
}

func (h *handlers) listWorkspaces(ctx context.Context, in *listWorkspacesInput) (*listWorkspacesOutput, error) {
	opts, err := domain.ListWorkspacesOptions{
		Filter: domain.QueryParams{
			Name: in.Name, Status: domain.WorkspaceStatus(in.Status),
			Namespace: in.Namespace, ActionType: domain.ActionType(in.ActionType),
		},
		Sort:       domain.SortField(in.Sort),
		Order:      domain.SortOrder(in.Order),
		Pagination: domain.Pagination{Page: in.Page, Limit: in.Limit},
	}.Normalize()
	if err != nil {
		return nil, h.fail(ctx, err)
	}
	page, err := h.store.ListWorkspaces(ctx, opts)
	if err != nil {
		return nil, h.fail(ctx, err)
	}
	out := &listWorkspacesOutput{}
	out.Body.Items = make([]Workspace, 0, len(page.Items))
	for _, w := range page.Items {
		out.Body.Items = append(out.Body.Items, fromWorkspace(w))
	}
	out.Body.Pagination = page.Pagination
	return out, nil
}

func (h *handlers) getWorkspace(ctx context.Context, in *WorkspacePath) (*workspaceDetailOutput, error) {
	id, err := parseID("workspaceId", in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	w, err := h.store.GetWorkspace(ctx, id)
	if err != nil {
		return nil, h.fail(ctx, err)
	}
	return &workspaceDetailOutput{Body: fromWorkspaceDetail(w)}, nil
}

func (h *handlers) updateWorkspace(ctx context.Context, in *updateWorkspaceInput) (*workspaceDetailOutput, error) {
	id, err := parseID("workspaceId", in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	b := in.Body
	p := domain.WorkspacePatch{
		Name: b.Name, VcsURL: b.VcsURL, VcsKeyStr: b.VcsKeyStr, VcsBranch: b.VcsBranch,
		VcsDirectory: b.VcsDirectory, Namespace: b.Namespace, TofuVersion: b.TofuVersion,
	}
	if b.Status != nil {
		st := domain.WorkspaceStatus(*b.Status)
		p.Status = &st
	}
	if b.EnvVars != nil {
		env := toEnvVars(*b.EnvVars)
		if err := domain.ValidateEnvVars(env); err != nil {
			return nil, h.fail(ctx, err)
		}
		p.EnvVars = &env
	}
	w, err := h.store.UpdateWorkspace(ctx, id, p)
	if err != nil {
		return nil, h.fail(ctx, err)
	}
	return &workspaceDetailOutput{Body: fromWorkspaceDetail(w)}, nil
}

func (h *handlers) deleteWorkspace(ctx context.Context, in *WorkspacePath) (*struct{}, error) {
	id, err := parseID("workspaceId", in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := h.store.DeleteWorkspace(ctx, id); err != nil {
		return nil, h.fail(ctx, err)
	}
	return nil, nil
}
