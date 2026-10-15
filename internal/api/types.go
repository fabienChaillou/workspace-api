package api

import (
	"time"

	"github.com/example/workspace-api/internal/domain"
)

// ------------------------------------------------------------- representations

// EnvVar is one workspace environment variable.
type EnvVar struct {
	Name   string `json:"name" minLength:"1" maxLength:"255" doc:"Variable name, unique within the workspace"`
	Value  string `json:"value" required:"false" doc:"Variable value"`
	Secure bool   `json:"secure" required:"false" doc:"Sensitive value"`
	Hidden bool   `json:"hidden" required:"false" doc:"Hidden from listings"`
}

// Workspace is the representation returned by the list endpoint.
type Workspace struct {
	ID           string     `json:"id" format:"uuid"`
	Name         string     `json:"name"`
	VcsURL       string     `json:"vcsUrl"`
	VcsKeyStr    string     `json:"vcsKeyStr"`
	VcsBranch    string     `json:"vcsBranch"`
	VcsDirectory string     `json:"vcsDirectory"`
	Status       string     `json:"status" enum:"ACTIVE,FAILURE,INACTIVE,DELETED"`
	EnvVars      []EnvVar   `json:"envVars"`
	Namespace    string     `json:"namespace"`
	TofuVersion  string     `json:"tofuVersion"`
	LastActionAt *time.Time `json:"lastActionAt,omitempty" doc:"Most recent updatedAt among the workspace actions"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// WorkspaceDetail is a workspace with its actions and variables.
type WorkspaceDetail struct {
	Workspace
	Actions   []Action   `json:"actions"`
	Variables []Variable `json:"variables"`
}

// Action is a Terraform/OpenTofu command run on a workspace.
type Action struct {
	ID          string    `json:"id" format:"uuid"`
	WorkspaceID string    `json:"workspaceId" format:"uuid"`
	Type        string    `json:"type" enum:"init,plan,apply,destroy"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Variable belongs to a workspace; its name is globally unique.
type Variable struct {
	WorkspaceID string    `json:"workspaceId" format:"uuid"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ------------------------------------------------------------------- mapping

func toEnvVars(in []EnvVar) []domain.EnvVars {
	out := make([]domain.EnvVars, len(in))
	for i, e := range in {
		out[i] = domain.EnvVars{Name: e.Name, Value: e.Value, Secure: e.Secure, Hidden: e.Hidden}
	}
	return out
}

func fromEnvVars(in []domain.EnvVars) []EnvVar {
	out := make([]EnvVar, len(in))
	for i, e := range in {
		out[i] = EnvVar{Name: e.Name, Value: e.Value, Secure: e.Secure, Hidden: e.Hidden}
	}
	return out
}

func fromWorkspace(w domain.Workspace) Workspace {
	return Workspace{
		ID: w.ID.String(), Name: w.Name, VcsURL: w.VcsURL, VcsKeyStr: w.VcsKeyStr, VcsBranch: w.VcsBranch,
		VcsDirectory: w.VcsDirectory, Status: string(w.Status), EnvVars: fromEnvVars(w.EnvVars),
		Namespace: w.Namespace, TofuVersion: w.TofuVersion, LastActionAt: w.LastActionAt,
		CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt,
	}
}

func fromWorkspaceDetail(w domain.Workspace) WorkspaceDetail {
	d := WorkspaceDetail{
		Workspace: fromWorkspace(w),
		Actions:   make([]Action, 0, len(w.Actions)),
		Variables: make([]Variable, 0, len(w.Variables)),
	}
	for _, a := range w.Actions {
		d.Actions = append(d.Actions, fromAction(a))
	}
	for _, v := range w.Variables {
		d.Variables = append(d.Variables, fromVariable(v))
	}
	return d
}

func fromAction(a domain.Action) Action {
	return Action{
		ID: a.ID.String(), WorkspaceID: a.WorkspaceID.String(), Type: string(a.Type), Status: a.Status,
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
}

func fromVariable(v domain.Variable) Variable {
	return Variable{
		WorkspaceID: v.WorkspaceID.String(), Name: v.Name, Status: v.Status,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}
