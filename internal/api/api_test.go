package api_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/example/workspace-api/internal/api"
	"github.com/example/workspace-api/internal/store"
	"github.com/example/workspace-api/internal/store/memstore"
	"github.com/google/uuid"
)

// newAPI returns a test API on top of the given store (in-memory by default).
func newAPI(t *testing.T, s store.Store) humatest.TestAPI {
	t.Helper()
	if s == nil {
		s = memstore.New()
	}
	_, tapi := humatest.New(t)
	api.Register(tapi, s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return tapi
}

func decode[T any](t *testing.T, body io.Reader) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(body).Decode(&v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return v
}

func wantStatus(t *testing.T, code int, want int, body string) {
	t.Helper()
	if code != want {
		t.Fatalf("status = %d, want %d; body: %s", code, want, body)
	}
}

type listResponse struct {
	Items      []api.Workspace `json:"items"`
	Pagination struct {
		Page       int   `json:"page"`
		Limit      int   `json:"limit"`
		TotalRows  int64 `json:"total_rows"`
		TotalPages int   `json:"total_pages"`
	} `json:"pagination"`
}

func createWorkspace(t *testing.T, tapi humatest.TestAPI, body map[string]any) api.WorkspaceDetail {
	t.Helper()
	resp := tapi.Post("/workspaces", body)
	wantStatus(t, resp.Code, http.StatusCreated, resp.Body.String())
	return decode[api.WorkspaceDetail](t, resp.Body)
}

func TestCreateWorkspace(t *testing.T) {
	tapi := newAPI(t, nil)
	resp := tapi.Post("/workspaces", map[string]any{
		"name":      "network",
		"vcsUrl":    "git@github.com:acme/infra.git",
		"namespace": "team-a",
		"envVars":   []map[string]any{{"name": "TF_LOG", "value": "DEBUG"}, {"name": "TOKEN", "value": "x", "secure": true}},
	})
	wantStatus(t, resp.Code, http.StatusCreated, resp.Body.String())
	w := decode[api.WorkspaceDetail](t, resp.Body)

	if _, err := uuid.Parse(w.ID); err != nil {
		t.Fatalf("id: %v", err)
	}
	if got := resp.Header().Get("Location"); got != "/workspaces/"+w.ID {
		t.Fatalf("Location = %q", got)
	}
	if w.Status != "ACTIVE" {
		t.Fatalf("default status = %q, want ACTIVE", w.Status)
	}
	if len(w.EnvVars) != 2 || !w.EnvVars[1].Secure || w.EnvVars[0].Secure {
		t.Fatalf("envVars = %+v", w.EnvVars)
	}
	if w.Actions == nil || w.Variables == nil {
		t.Fatalf("actions/variables must be [] not null")
	}
}

func TestCreateWorkspaceValidation(t *testing.T) {
	tapi := newAPI(t, nil)
	cases := map[string]map[string]any{
		"missing name":         {"namespace": "x"},
		"empty name":           {"name": ""},
		"unknown status":       {"name": "a", "status": "BROKEN"},
		"env var without name": {"name": "a", "envVars": []map[string]any{{"value": "v"}}},
		"duplicated env var":   {"name": "a", "envVars": []map[string]any{{"name": "A"}, {"name": "A"}}},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resp := tapi.Post("/workspaces", body)
			wantStatus(t, resp.Code, http.StatusUnprocessableEntity, resp.Body.String())
		})
	}
}

func TestWorkspaceLifecycle(t *testing.T) {
	tapi := newAPI(t, nil)
	w := createWorkspace(t, tapi, map[string]any{"name": "app", "vcsBranch": "main"})

	resp := tapi.Patch("/workspaces/"+w.ID, map[string]any{"vcsBranch": "develop", "status": "INACTIVE"})
	wantStatus(t, resp.Code, http.StatusOK, resp.Body.String())
	upd := decode[api.WorkspaceDetail](t, resp.Body)
	if upd.VcsBranch != "develop" || upd.Status != "INACTIVE" || upd.Name != "app" {
		t.Fatalf("patch = %+v", upd)
	}

	resp = tapi.Patch("/workspaces/"+w.ID, map[string]any{"status": "NOPE"})
	wantStatus(t, resp.Code, http.StatusUnprocessableEntity, resp.Body.String())

	resp = tapi.Get("/workspaces/" + w.ID)
	wantStatus(t, resp.Code, http.StatusOK, resp.Body.String())

	resp = tapi.Delete("/workspaces/" + w.ID)
	wantStatus(t, resp.Code, http.StatusNoContent, resp.Body.String())
	resp = tapi.Get("/workspaces/" + w.ID)
	wantStatus(t, resp.Code, http.StatusNotFound, resp.Body.String())
	resp = tapi.Delete("/workspaces/" + w.ID)
	wantStatus(t, resp.Code, http.StatusNotFound, resp.Body.String())
}

func TestInvalidIDs(t *testing.T) {
	tapi := newAPI(t, nil)
	for _, path := range []string{
		"/workspaces/not-a-uuid",
		"/workspaces/not-a-uuid/actions",
		"/workspaces/" + uuid.NewString() + "/actions/not-a-uuid",
	} {
		resp := tapi.Get(path)
		wantStatus(t, resp.Code, http.StatusUnprocessableEntity, path+": "+resp.Body.String())
	}
	resp := tapi.Get("/workspaces/" + uuid.NewString())
	wantStatus(t, resp.Code, http.StatusNotFound, resp.Body.String())
}

func TestActionsRequireWorkspace(t *testing.T) {
	tapi := newAPI(t, nil)
	resp := tapi.Post("/workspaces/"+uuid.NewString()+"/actions", map[string]any{"type": "plan"})
	wantStatus(t, resp.Code, http.StatusNotFound, resp.Body.String())

	resp = tapi.Post("/workspaces/"+uuid.NewString()+"/variables", map[string]any{"name": "x"})
	wantStatus(t, resp.Code, http.StatusNotFound, resp.Body.String())
}

func TestActionCRUD(t *testing.T) {
	tapi := newAPI(t, nil)
	w := createWorkspace(t, tapi, map[string]any{"name": "app"})
	base := "/workspaces/" + w.ID + "/actions"

	resp := tapi.Post(base, map[string]any{"type": "refresh"})
	wantStatus(t, resp.Code, http.StatusUnprocessableEntity, resp.Body.String())

	resp = tapi.Post(base, map[string]any{"type": "plan", "status": "queued"})
	wantStatus(t, resp.Code, http.StatusCreated, resp.Body.String())
	a := decode[api.Action](t, resp.Body)
	if a.WorkspaceID != w.ID || a.Type != "plan" || resp.Header().Get("Location") != base+"/"+a.ID {
		t.Fatalf("action = %+v, Location = %q", a, resp.Header().Get("Location"))
	}

	resp = tapi.Patch(base+"/"+a.ID, map[string]any{"status": "succeeded"})
	wantStatus(t, resp.Code, http.StatusOK, resp.Body.String())
	if got := decode[api.Action](t, resp.Body); got.Status != "succeeded" || got.Type != "plan" {
		t.Fatalf("patched = %+v", got)
	}

	resp = tapi.Get(base)
	wantStatus(t, resp.Code, http.StatusOK, resp.Body.String())
	if l := decode[struct{ Items []api.Action }](t, resp.Body); len(l.Items) != 1 {
		t.Fatalf("list = %+v", l)
	}

	// The action is not reachable through another workspace.
	other := createWorkspace(t, tapi, map[string]any{"name": "other"})
	resp = tapi.Get("/workspaces/" + other.ID + "/actions/" + a.ID)
	wantStatus(t, resp.Code, http.StatusNotFound, resp.Body.String())

	resp = tapi.Delete(base + "/" + a.ID)
	wantStatus(t, resp.Code, http.StatusNoContent, resp.Body.String())
	resp = tapi.Get(base + "/" + a.ID)
	wantStatus(t, resp.Code, http.StatusNotFound, resp.Body.String())
}

func TestVariableCRUDAndUniqueness(t *testing.T) {
	tapi := newAPI(t, nil)
	w := createWorkspace(t, tapi, map[string]any{"name": "app"})
	other := createWorkspace(t, tapi, map[string]any{"name": "other"})
	base := "/workspaces/" + w.ID + "/variables"

	resp := tapi.Post(base, map[string]any{"name": "region", "status": "set"})
	wantStatus(t, resp.Code, http.StatusCreated, resp.Body.String())

	resp = tapi.Post("/workspaces/"+other.ID+"/variables", map[string]any{"name": "region"})
	wantStatus(t, resp.Code, http.StatusConflict, resp.Body.String())

	resp = tapi.Post(base, map[string]any{"name": "has space"})
	wantStatus(t, resp.Code, http.StatusUnprocessableEntity, resp.Body.String())

	resp = tapi.Patch(base+"/region", map[string]any{"status": "locked"})
	wantStatus(t, resp.Code, http.StatusOK, resp.Body.String())
	if v := decode[api.Variable](t, resp.Body); v.Status != "locked" {
		t.Fatalf("patched = %+v", v)
	}

	resp = tapi.Get("/workspaces/" + w.ID)
	if d := decode[api.WorkspaceDetail](t, resp.Body); len(d.Variables) != 1 || d.Variables[0].Name != "region" {
		t.Fatalf("workspace variables = %+v", d.Variables)
	}

	resp = tapi.Delete(base + "/region")
	wantStatus(t, resp.Code, http.StatusNoContent, resp.Body.String())
	resp = tapi.Post("/workspaces/"+other.ID+"/variables", map[string]any{"name": "region"})
	wantStatus(t, resp.Code, http.StatusCreated, resp.Body.String())
}

func TestListWorkspaces(t *testing.T) {
	tapi := newAPI(t, nil)
	ids := map[string]string{}
	for _, n := range []string{"alpha", "bravo", "charlie"} {
		ids[n] = createWorkspace(t, tapi, map[string]any{"name": n, "namespace": "ns-" + n}).ID
	}
	for _, n := range []string{"alpha", "charlie", "bravo"} {
		resp := tapi.Post("/workspaces/"+ids[n]+"/actions", map[string]any{"type": "init"})
		wantStatus(t, resp.Code, http.StatusCreated, resp.Body.String())
	}
	resp := tapi.Post("/workspaces/"+ids["alpha"]+"/actions", map[string]any{"type": "apply"})
	wantStatus(t, resp.Code, http.StatusCreated, resp.Body.String())

	get := func(query string) listResponse {
		t.Helper()
		resp := tapi.Get("/workspaces" + query)
		wantStatus(t, resp.Code, http.StatusOK, resp.Body.String())
		return decode[listResponse](t, resp.Body)
	}
	namesOf := func(l listResponse) string {
		var n []string
		for _, w := range l.Items {
			n = append(n, w.Name)
		}
		return strings.Join(n, ",")
	}

	l := get("")
	if got := namesOf(l); got != "alpha,bravo,charlie" {
		t.Fatalf("default order = %s", got)
	}
	if l.Pagination.Page != 1 || l.Pagination.Limit != 10 || l.Pagination.TotalRows != 3 || l.Pagination.TotalPages != 1 {
		t.Fatalf("pagination = %+v", l.Pagination)
	}
	if l.Items[0].LastActionAt == nil {
		t.Fatal("lastActionAt missing")
	}

	if got := namesOf(get("?actionType=apply")); got != "alpha" {
		t.Fatalf("actionType filter = %s", got)
	}
	if got := namesOf(get("?namespace=ns-bravo")); got != "bravo" {
		t.Fatalf("namespace filter = %s", got)
	}
	if got := namesOf(get("?name=AR")); got != "charlie" {
		t.Fatalf("name filter = %s", got)
	}
	if got := namesOf(get("?status=FAILURE")); got != "" {
		t.Fatalf("status filter = %s", got)
	}
	if got := namesOf(get("?sort=name&order=asc&page=2&limit=2")); got != "charlie" {
		t.Fatalf("page 2 = %s", got)
	}
	l = get("?page=5&limit=2")
	if len(l.Items) != 0 || l.Items == nil || l.Pagination.TotalPages != 2 {
		t.Fatalf("out of range page = %+v", l)
	}

	for _, q := range []string{"?status=BROKEN", "?actionType=refresh", "?limit=0", "?limit=101", "?page=0", "?sort=vcs_url", "?order=up"} {
		resp := tapi.Get("/workspaces" + q)
		wantStatus(t, resp.Code, http.StatusUnprocessableEntity, q+": "+resp.Body.String())
	}
}

func TestHealth(t *testing.T) {
	resp := newAPI(t, nil).Get("/healthz")
	wantStatus(t, resp.Code, http.StatusOK, resp.Body.String())
}
