package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/example/workspace-api/internal/domain"
	"github.com/google/uuid"
)

// ------------------------------------------------------------------- actions

type ActionPath struct {
	WorkspaceID string `path:"workspaceId" format:"uuid" doc:"Workspace ID"`
	ActionID    string `path:"actionId" format:"uuid" doc:"Action ID"`
}

func (p ActionPath) ids() (uuid.UUID, uuid.UUID, error) {
	ws, err := parseID("workspaceId", p.WorkspaceID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	id, err := parseID("actionId", p.ActionID)
	return ws, id, err
}

type createActionInput struct {
	WorkspacePath
	Body struct {
		Type   string `json:"type" enum:"init,plan,apply,destroy"`
		Status string `json:"status,omitempty" maxLength:"64"`
	}
}

type updateActionInput struct {
	ActionPath
	Body struct {
		Type   *string `json:"type,omitempty" enum:"init,plan,apply,destroy"`
		Status *string `json:"status,omitempty" maxLength:"64"`
	}
}

type actionOutput struct {
	Location string `header:"Location"`
	Body     Action
}

type actionsOutput struct {
	Body struct {
		Items []Action `json:"items"`
	}
}

func (h *handlers) registerActions(api huma.API) {
	tags := []string{"Actions"}
	base := "/workspaces/{workspaceId}/actions"

	huma.Register(api, huma.Operation{
		OperationID: "create-action", Method: http.MethodPost, Path: base,
		Summary: "Create an action on a workspace", Tags: tags, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createActionInput) (*actionOutput, error) {
		ws, err := parseID("workspaceId", in.WorkspaceID)
		if err != nil {
			return nil, err
		}
		a, err := h.store.CreateAction(ctx, domain.Action{
			WorkspaceID: ws, Type: domain.ActionType(in.Body.Type), Status: in.Body.Status,
		})
		if err != nil {
			return nil, h.fail(ctx, err)
		}
		return &actionOutput{Location: "/workspaces/" + ws.String() + "/actions/" + a.ID.String(), Body: fromAction(a)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-actions", Method: http.MethodGet, Path: base,
		Summary: "List the actions of a workspace (latest update first)", Tags: tags,
	}, func(ctx context.Context, in *WorkspacePath) (*actionsOutput, error) {
		ws, err := parseID("workspaceId", in.WorkspaceID)
		if err != nil {
			return nil, err
		}
		as, err := h.store.ListActions(ctx, ws)
		if err != nil {
			return nil, h.fail(ctx, err)
		}
		out := &actionsOutput{}
		out.Body.Items = make([]Action, 0, len(as))
		for _, a := range as {
			out.Body.Items = append(out.Body.Items, fromAction(a))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-action", Method: http.MethodGet, Path: base + "/{actionId}",
		Summary: "Get an action", Tags: tags,
	}, func(ctx context.Context, in *ActionPath) (*actionOutput, error) {
		ws, id, err := in.ids()
		if err != nil {
			return nil, err
		}
		a, err := h.store.GetAction(ctx, ws, id)
		if err != nil {
			return nil, h.fail(ctx, err)
		}
		return &actionOutput{Body: fromAction(a)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-action", Method: http.MethodPatch, Path: base + "/{actionId}",
		Summary: "Partially update an action", Tags: tags,
	}, func(ctx context.Context, in *updateActionInput) (*actionOutput, error) {
		ws, id, err := in.ids()
		if err != nil {
			return nil, err
		}
		p := domain.ActionPatch{Status: in.Body.Status}
		if in.Body.Type != nil {
			t := domain.ActionType(*in.Body.Type)
			p.Type = &t
		}
		a, err := h.store.UpdateAction(ctx, ws, id, p)
		if err != nil {
			return nil, h.fail(ctx, err)
		}
		return &actionOutput{Body: fromAction(a)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-action", Method: http.MethodDelete, Path: base + "/{actionId}",
		Summary: "Delete an action", Tags: tags, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *ActionPath) (*struct{}, error) {
		ws, id, err := in.ids()
		if err != nil {
			return nil, err
		}
		if err := h.store.DeleteAction(ctx, ws, id); err != nil {
			return nil, h.fail(ctx, err)
		}
		return nil, nil
	})
}

// ----------------------------------------------------------------- variables

type VariablePath struct {
	WorkspaceID string `path:"workspaceId" format:"uuid" doc:"Workspace ID"`
	Name        string `path:"name" minLength:"1" maxLength:"255" doc:"Variable name"`
}

type createVariableInput struct {
	WorkspacePath
	Body struct {
		Name   string `json:"name" minLength:"1" maxLength:"255" pattern:"^[A-Za-z_][A-Za-z0-9_\\-.]*$" doc:"Globally unique name"`
		Status string `json:"status,omitempty" maxLength:"64"`
	}
}

type updateVariableInput struct {
	VariablePath
	Body struct {
		Status *string `json:"status,omitempty" maxLength:"64"`
	}
}

type variableOutput struct {
	Location string `header:"Location"`
	Body     Variable
}

type variablesOutput struct {
	Body struct {
		Items []Variable `json:"items"`
	}
}

func (h *handlers) registerVariables(api huma.API) {
	tags := []string{"Variables"}
	base := "/workspaces/{workspaceId}/variables"

	huma.Register(api, huma.Operation{
		OperationID: "create-variable", Method: http.MethodPost, Path: base,
		Summary: "Create a variable on a workspace", Tags: tags, DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusConflict, http.StatusNotFound},
	}, func(ctx context.Context, in *createVariableInput) (*variableOutput, error) {
		ws, err := parseID("workspaceId", in.WorkspaceID)
		if err != nil {
			return nil, err
		}
		v, err := h.store.CreateVariable(ctx, domain.Variable{WorkspaceID: ws, Name: in.Body.Name, Status: in.Body.Status})
		if err != nil {
			return nil, h.fail(ctx, err)
		}
		return &variableOutput{Location: "/workspaces/" + ws.String() + "/variables/" + v.Name, Body: fromVariable(v)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-variables", Method: http.MethodGet, Path: base,
		Summary: "List the variables of a workspace", Tags: tags,
	}, func(ctx context.Context, in *WorkspacePath) (*variablesOutput, error) {
		ws, err := parseID("workspaceId", in.WorkspaceID)
		if err != nil {
			return nil, err
		}
		vs, err := h.store.ListVariables(ctx, ws)
		if err != nil {
			return nil, h.fail(ctx, err)
		}
		out := &variablesOutput{}
		out.Body.Items = make([]Variable, 0, len(vs))
		for _, v := range vs {
			out.Body.Items = append(out.Body.Items, fromVariable(v))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-variable", Method: http.MethodGet, Path: base + "/{name}",
		Summary: "Get a variable", Tags: tags,
	}, func(ctx context.Context, in *VariablePath) (*variableOutput, error) {
		ws, err := parseID("workspaceId", in.WorkspaceID)
		if err != nil {
			return nil, err
		}
		v, err := h.store.GetVariable(ctx, ws, in.Name)
		if err != nil {
			return nil, h.fail(ctx, err)
		}
		return &variableOutput{Body: fromVariable(v)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-variable", Method: http.MethodPatch, Path: base + "/{name}",
		Summary: "Partially update a variable", Tags: tags,
	}, func(ctx context.Context, in *updateVariableInput) (*variableOutput, error) {
		ws, err := parseID("workspaceId", in.WorkspaceID)
		if err != nil {
			return nil, err
		}
		v, err := h.store.UpdateVariable(ctx, ws, in.Name, domain.VariablePatch{Status: in.Body.Status})
		if err != nil {
			return nil, h.fail(ctx, err)
		}
		return &variableOutput{Body: fromVariable(v)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-variable", Method: http.MethodDelete, Path: base + "/{name}",
		Summary: "Delete a variable", Tags: tags, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *VariablePath) (*struct{}, error) {
		ws, err := parseID("workspaceId", in.WorkspaceID)
		if err != nil {
			return nil, err
		}
		if err := h.store.DeleteVariable(ctx, ws, in.Name); err != nil {
			return nil, h.fail(ctx, err)
		}
		return nil, nil
	})
}
