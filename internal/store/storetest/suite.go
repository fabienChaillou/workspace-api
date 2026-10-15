// Package storetest is the behavioural contract of store.Store. Every
// implementation (memstore in unit tests, pgx and gorm in integration tests)
// runs the same suite, which guarantees the three behave identically.
package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/workspace-api/internal/domain"
	"github.com/example/workspace-api/internal/store"
	"github.com/google/uuid"
)

// Factory returns an empty store for one subtest.
type Factory func(t *testing.T) store.Store

// Run executes the whole contract against the stores produced by newStore.
func Run(t *testing.T, newStore Factory) {
	tests := []struct {
		name string
		fn   func(t *testing.T, s store.Store)
	}{
		{"WorkspaceCRUD", testWorkspaceCRUD},
		{"WorkspacePartialUpdate", testWorkspacePartialUpdate},
		{"WorkspaceNotFound", testWorkspaceNotFound},
		{"ActionCRUD", testActionCRUD},
		{"ActionRequiresWorkspace", testActionRequiresWorkspace},
		{"VariableCRUD", testVariableCRUD},
		{"VariableRequiresWorkspace", testVariableRequiresWorkspace},
		{"VariableNameIsUnique", testVariableNameIsUnique},
		{"DeleteWorkspaceCascades", testDeleteWorkspaceCascades},
		{"ListDefaultOrderByActionsUpdatedAt", testListDefaultOrder},
		{"ListOtherSorts", testListOtherSorts},
		{"ListFilters", testListFilters},
		{"ListPagination", testListPagination},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.fn(t, newStore(t))
		})
	}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

func ptr[T any](v T) *T { return &v }

func mustWorkspace(t *testing.T, s store.Store, w domain.Workspace) domain.Workspace {
	t.Helper()
	out, err := s.CreateWorkspace(ctx(t), w)
	if err != nil {
		t.Fatalf("CreateWorkspace(%q): %v", w.Name, err)
	}
	return out
}

func mustAction(t *testing.T, s store.Store, wsID uuid.UUID, typ domain.ActionType) domain.Action {
	t.Helper()
	out, err := s.CreateAction(ctx(t), domain.Action{WorkspaceID: wsID, Type: typ, Status: "queued"})
	if err != nil {
		t.Fatalf("CreateAction: %v", err)
	}
	return out
}

func wantErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error = %v, want %v", err, target)
	}
}

func names(ws []domain.Workspace) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.Name
	}
	return out
}

func wantNames(t *testing.T, got []domain.Workspace, want ...string) {
	t.Helper()
	g := names(got)
	if len(g) != len(want) {
		t.Fatalf("names = %v, want %v", g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("names = %v, want %v", g, want)
		}
	}
}

func list(t *testing.T, s store.Store, o domain.ListWorkspacesOptions) domain.WorkspacePage {
	t.Helper()
	o, err := o.Normalize()
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	p, err := s.ListWorkspaces(ctx(t), o)
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	return p
}

// ------------------------------------------------------------------- tests

func testWorkspaceCRUD(t *testing.T, s store.Store) {
	env := []domain.EnvVars{
		{Name: "TF_LOG", Value: "DEBUG"},
		{Name: "AWS_SECRET", Value: "s3cr3t", Secure: true, Hidden: true},
	}
	created := mustWorkspace(t, s, domain.Workspace{
		Name: "network", VcsURL: "git@github.com:acme/infra.git", VcsKeyStr: "key", VcsBranch: "main",
		VcsDirectory: "network", Status: domain.WorkspaceStatusActive, EnvVars: env,
		Namespace: "team-a", TofuVersion: "1.8.0",
	})
	if created.ID == uuid.Nil || created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("missing generated fields: %+v", created)
	}

	got, err := s.GetWorkspace(ctx(t), created.ID)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if got.Name != "network" || got.VcsURL != created.VcsURL || got.VcsDirectory != "network" ||
		got.Namespace != "team-a" || got.TofuVersion != "1.8.0" || got.Status != domain.WorkspaceStatusActive {
		t.Fatalf("unexpected workspace: %+v", got)
	}
	if len(got.EnvVars) != 2 || got.EnvVars[1] != env[1] {
		t.Fatalf("env vars round-trip: %+v", got.EnvVars)
	}
	if !got.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("CreatedAt changed: %v vs %v", got.CreatedAt, created.CreatedAt)
	}
	if got.Actions == nil || got.Variables == nil || got.LastActionAt != nil {
		t.Fatalf("expected empty (non-nil) children and no LastActionAt: %+v", got)
	}

	updated, err := s.UpdateWorkspace(ctx(t), created.ID, domain.WorkspacePatch{
		Name: ptr("network-v2"), Status: ptr(domain.WorkspaceStatusFailure),
	})
	if err != nil {
		t.Fatalf("UpdateWorkspace: %v", err)
	}
	if updated.Name != "network-v2" || updated.Status != domain.WorkspaceStatusFailure {
		t.Fatalf("update not applied: %+v", updated)
	}
	if !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("UpdatedAt not bumped: %v -> %v", created.UpdatedAt, updated.UpdatedAt)
	}

	if err := s.DeleteWorkspace(ctx(t), created.ID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	_, err = s.GetWorkspace(ctx(t), created.ID)
	wantErr(t, err, domain.ErrNotFound)
	wantErr(t, s.DeleteWorkspace(ctx(t), created.ID), domain.ErrNotFound)
}

func testWorkspacePartialUpdate(t *testing.T, s store.Store) {
	w := mustWorkspace(t, s, domain.Workspace{
		Name: "dns", VcsBranch: "main", Namespace: "ns", EnvVars: []domain.EnvVars{{Name: "A", Value: "1"}},
	})
	got, err := s.UpdateWorkspace(ctx(t), w.ID, domain.WorkspacePatch{VcsBranch: ptr("develop")})
	if err != nil {
		t.Fatalf("UpdateWorkspace: %v", err)
	}
	if got.VcsBranch != "develop" || got.Name != "dns" || got.Namespace != "ns" || len(got.EnvVars) != 1 {
		t.Fatalf("partial update touched other fields: %+v", got)
	}
	if got.Status != domain.WorkspaceStatusActive {
		t.Fatalf("default status = %q, want ACTIVE", got.Status)
	}

	// Replacing env vars with an empty list clears them.
	got, err = s.UpdateWorkspace(ctx(t), w.ID, domain.WorkspacePatch{EnvVars: ptr([]domain.EnvVars{})})
	if err != nil {
		t.Fatalf("UpdateWorkspace: %v", err)
	}
	if got.EnvVars == nil || len(got.EnvVars) != 0 {
		t.Fatalf("env vars not cleared: %#v", got.EnvVars)
	}

	// An empty patch is valid and only bumps UpdatedAt.
	again, err := s.UpdateWorkspace(ctx(t), w.ID, domain.WorkspacePatch{})
	if err != nil {
		t.Fatalf("empty patch: %v", err)
	}
	if again.VcsBranch != "develop" {
		t.Fatalf("empty patch changed data: %+v", again)
	}
}

func testWorkspaceNotFound(t *testing.T, s store.Store) {
	id := uuid.New()
	_, err := s.GetWorkspace(ctx(t), id)
	wantErr(t, err, domain.ErrNotFound)
	_, err = s.UpdateWorkspace(ctx(t), id, domain.WorkspacePatch{Name: ptr("x")})
	wantErr(t, err, domain.ErrNotFound)
	wantErr(t, s.DeleteWorkspace(ctx(t), id), domain.ErrNotFound)
	_, err = s.ListActions(ctx(t), id)
	wantErr(t, err, domain.ErrNotFound)
	_, err = s.ListVariables(ctx(t), id)
	wantErr(t, err, domain.ErrNotFound)
}

func testActionCRUD(t *testing.T, s store.Store) {
	w := mustWorkspace(t, s, domain.Workspace{Name: "app"})
	other := mustWorkspace(t, s, domain.Workspace{Name: "other"})

	a := mustAction(t, s, w.ID, domain.ActionTypePlan)
	if a.ID == uuid.Nil || a.WorkspaceID != w.ID || a.Type != domain.ActionTypePlan || a.Status != "queued" {
		t.Fatalf("unexpected action: %+v", a)
	}
	got, err := s.GetAction(ctx(t), w.ID, a.ID)
	if err != nil || got.ID != a.ID || !got.UpdatedAt.Equal(a.UpdatedAt) {
		t.Fatalf("GetAction = %+v, %v", got, err)
	}
	// An action is only reachable through its own workspace.
	_, err = s.GetAction(ctx(t), other.ID, a.ID)
	wantErr(t, err, domain.ErrNotFound)

	b := mustAction(t, s, w.ID, domain.ActionTypeApply)
	actions, err := s.ListActions(ctx(t), w.ID)
	if err != nil || len(actions) != 2 || actions[0].ID != b.ID {
		t.Fatalf("ListActions (latest first) = %+v, %v", actions, err)
	}

	upd, err := s.UpdateAction(ctx(t), w.ID, a.ID, domain.ActionPatch{Status: ptr("succeeded")})
	if err != nil {
		t.Fatalf("UpdateAction: %v", err)
	}
	if upd.Status != "succeeded" || upd.Type != domain.ActionTypePlan || !upd.UpdatedAt.After(a.UpdatedAt) {
		t.Fatalf("UpdateAction = %+v", upd)
	}
	_, err = s.UpdateAction(ctx(t), other.ID, a.ID, domain.ActionPatch{Status: ptr("x")})
	wantErr(t, err, domain.ErrNotFound)

	ws, err := s.GetWorkspace(ctx(t), w.ID)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if len(ws.Actions) != 2 || ws.Actions[0].ID != a.ID || ws.LastActionAt == nil || !ws.LastActionAt.Equal(upd.UpdatedAt) {
		t.Fatalf("workspace actions/LastActionAt = %+v / %v", ws.Actions, ws.LastActionAt)
	}

	wantErr(t, s.DeleteAction(ctx(t), other.ID, a.ID), domain.ErrNotFound)
	if err := s.DeleteAction(ctx(t), w.ID, a.ID); err != nil {
		t.Fatalf("DeleteAction: %v", err)
	}
	_, err = s.GetAction(ctx(t), w.ID, a.ID)
	wantErr(t, err, domain.ErrNotFound)
	wantErr(t, s.DeleteAction(ctx(t), w.ID, a.ID), domain.ErrNotFound)
	actions, _ = s.ListActions(ctx(t), w.ID)
	if len(actions) != 1 {
		t.Fatalf("deleted action still listed: %+v", actions)
	}
}

func testActionRequiresWorkspace(t *testing.T, s store.Store) {
	_, err := s.CreateAction(ctx(t), domain.Action{WorkspaceID: uuid.New(), Type: domain.ActionTypeInit})
	wantErr(t, err, domain.ErrNotFound)

	w := mustWorkspace(t, s, domain.Workspace{Name: "gone"})
	if err := s.DeleteWorkspace(ctx(t), w.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateAction(ctx(t), domain.Action{WorkspaceID: w.ID, Type: domain.ActionTypeInit})
	wantErr(t, err, domain.ErrNotFound)
}

func testVariableCRUD(t *testing.T, s store.Store) {
	w := mustWorkspace(t, s, domain.Workspace{Name: "vars"})
	v, err := s.CreateVariable(ctx(t), domain.Variable{WorkspaceID: w.ID, Name: "region", Status: "set"})
	if err != nil {
		t.Fatalf("CreateVariable: %v", err)
	}
	if v.Name != "region" || v.WorkspaceID != w.ID || v.CreatedAt.IsZero() {
		t.Fatalf("unexpected variable: %+v", v)
	}
	if _, err := s.CreateVariable(ctx(t), domain.Variable{WorkspaceID: w.ID, Name: "azone"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetVariable(ctx(t), w.ID, "region")
	if err != nil || got.Status != "set" {
		t.Fatalf("GetVariable = %+v, %v", got, err)
	}
	vars, err := s.ListVariables(ctx(t), w.ID)
	if err != nil || len(vars) != 2 || vars[0].Name != "azone" {
		t.Fatalf("ListVariables (by name) = %+v, %v", vars, err)
	}

	upd, err := s.UpdateVariable(ctx(t), w.ID, "region", domain.VariablePatch{Status: ptr("locked")})
	if err != nil || upd.Status != "locked" || !upd.UpdatedAt.After(v.UpdatedAt) {
		t.Fatalf("UpdateVariable = %+v, %v", upd, err)
	}
	_, err = s.UpdateVariable(ctx(t), w.ID, "nope", domain.VariablePatch{Status: ptr("x")})
	wantErr(t, err, domain.ErrNotFound)

	ws, _ := s.GetWorkspace(ctx(t), w.ID)
	if len(ws.Variables) != 2 {
		t.Fatalf("workspace variables = %+v", ws.Variables)
	}

	if err := s.DeleteVariable(ctx(t), w.ID, "region"); err != nil {
		t.Fatalf("DeleteVariable: %v", err)
	}
	_, err = s.GetVariable(ctx(t), w.ID, "region")
	wantErr(t, err, domain.ErrNotFound)
	wantErr(t, s.DeleteVariable(ctx(t), w.ID, "region"), domain.ErrNotFound)
}

func testVariableRequiresWorkspace(t *testing.T, s store.Store) {
	_, err := s.CreateVariable(ctx(t), domain.Variable{WorkspaceID: uuid.New(), Name: "orphan"})
	wantErr(t, err, domain.ErrNotFound)
}

func testVariableNameIsUnique(t *testing.T, s store.Store) {
	a := mustWorkspace(t, s, domain.Workspace{Name: "a"})
	b := mustWorkspace(t, s, domain.Workspace{Name: "b"})
	if _, err := s.CreateVariable(ctx(t), domain.Variable{WorkspaceID: a.ID, Name: "shared"}); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateVariable(ctx(t), domain.Variable{WorkspaceID: a.ID, Name: "shared"})
	wantErr(t, err, domain.ErrConflict)
	// Name is a global unique key, not per workspace.
	_, err = s.CreateVariable(ctx(t), domain.Variable{WorkspaceID: b.ID, Name: "shared"})
	wantErr(t, err, domain.ErrConflict)

	// Once soft-deleted, the name can be reused.
	if err := s.DeleteVariable(ctx(t), a.ID, "shared"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateVariable(ctx(t), domain.Variable{WorkspaceID: b.ID, Name: "shared"}); err != nil {
		t.Fatalf("reuse after delete: %v", err)
	}
}

func testDeleteWorkspaceCascades(t *testing.T, s store.Store) {
	w := mustWorkspace(t, s, domain.Workspace{Name: "cascade"})
	a := mustAction(t, s, w.ID, domain.ActionTypeApply)
	if _, err := s.CreateVariable(ctx(t), domain.Variable{WorkspaceID: w.ID, Name: "cascade_var"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteWorkspace(ctx(t), w.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetAction(ctx(t), w.ID, a.ID)
	wantErr(t, err, domain.ErrNotFound)
	_, err = s.GetVariable(ctx(t), w.ID, "cascade_var")
	wantErr(t, err, domain.ErrNotFound)

	// The variable name was released by the cascade.
	other := mustWorkspace(t, s, domain.Workspace{Name: "other"})
	if _, err := s.CreateVariable(ctx(t), domain.Variable{WorkspaceID: other.ID, Name: "cascade_var"}); err != nil {
		t.Fatalf("name not released: %v", err)
	}
	p := list(t, s, domain.ListWorkspacesOptions{})
	wantNames(t, p.Items, "other")
}

func testListDefaultOrder(t *testing.T, s store.Store) {
	alpha := mustWorkspace(t, s, domain.Workspace{Name: "alpha"})
	bravo := mustWorkspace(t, s, domain.Workspace{Name: "bravo"})
	charlie := mustWorkspace(t, s, domain.Workspace{Name: "charlie"})
	mustWorkspace(t, s, domain.Workspace{Name: "delta"}) // no action: always last

	aAction := mustAction(t, s, alpha.ID, domain.ActionTypeInit)
	mustAction(t, s, charlie.ID, domain.ActionTypeInit)
	mustAction(t, s, bravo.ID, domain.ActionTypeInit)

	p := list(t, s, domain.ListWorkspacesOptions{})
	wantNames(t, p.Items, "bravo", "charlie", "alpha", "delta")
	if p.Items[0].LastActionAt == nil || p.Items[3].LastActionAt != nil {
		t.Fatalf("LastActionAt not filled correctly: %+v", p.Items)
	}

	// Touching alpha's action moves alpha to the top.
	if _, err := s.UpdateAction(ctx(t), alpha.ID, aAction.ID, domain.ActionPatch{Status: ptr("running")}); err != nil {
		t.Fatal(err)
	}
	p = list(t, s, domain.ListWorkspacesOptions{})
	wantNames(t, p.Items, "alpha", "bravo", "charlie", "delta")

	// Ascending keeps workspaces without actions last (NULLS LAST).
	p = list(t, s, domain.ListWorkspacesOptions{Order: domain.SortAsc})
	wantNames(t, p.Items, "charlie", "bravo", "alpha", "delta")

	// Deleted actions are ignored: alpha falls back to "no action".
	if err := s.DeleteAction(ctx(t), alpha.ID, aAction.ID); err != nil {
		t.Fatal(err)
	}
	p = list(t, s, domain.ListWorkspacesOptions{})
	if p.Items[0].Name != "bravo" || p.Items[1].Name != "charlie" {
		t.Fatalf("deleted action still counted: %v", names(p.Items))
	}
}

func testListOtherSorts(t *testing.T, s store.Store) {
	for _, n := range []string{"bravo", "alpha", "charlie"} {
		mustWorkspace(t, s, domain.Workspace{Name: n})
	}
	wantNames(t, list(t, s, domain.ListWorkspacesOptions{Sort: domain.SortByName, Order: domain.SortAsc}).Items,
		"alpha", "bravo", "charlie")
	wantNames(t, list(t, s, domain.ListWorkspacesOptions{Sort: domain.SortByName}).Items,
		"charlie", "bravo", "alpha")
	wantNames(t, list(t, s, domain.ListWorkspacesOptions{Sort: domain.SortByCreatedAt, Order: domain.SortAsc}).Items,
		"bravo", "alpha", "charlie")
	wantNames(t, list(t, s, domain.ListWorkspacesOptions{Sort: domain.SortByUpdatedAt}).Items,
		"charlie", "alpha", "bravo")
}

func testListFilters(t *testing.T, s store.Store) {
	prod := mustWorkspace(t, s, domain.Workspace{Name: "Prod-Network", Namespace: "team-a"})
	mustWorkspace(t, s, domain.Workspace{Name: "prod-dns", Namespace: "team-b", Status: domain.WorkspaceStatusFailure})
	stg := mustWorkspace(t, s, domain.Workspace{Name: "staging_net", Namespace: "team-a", Status: domain.WorkspaceStatusInactive})
	mustAction(t, s, prod.ID, domain.ActionTypePlan)
	mustAction(t, s, stg.ID, domain.ActionTypeApply)
	deleted := mustAction(t, s, stg.ID, domain.ActionTypeDestroy)
	if err := s.DeleteAction(ctx(t), stg.ID, deleted.ID); err != nil {
		t.Fatal(err)
	}

	by := func(f domain.QueryParams) []string {
		p := list(t, s, domain.ListWorkspacesOptions{Filter: f, Sort: domain.SortByName, Order: domain.SortAsc})
		if p.Pagination.TotalRows != int64(len(p.Items)) {
			t.Fatalf("TotalRows %d != %d items", p.Pagination.TotalRows, len(p.Items))
		}
		return names(p.Items)
	}
	check := func(label string, got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: got %v, want %v", label, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: got %v, want %v", label, got, want)
			}
		}
	}

	check("name contains, case-insensitive", by(domain.QueryParams{Name: "PROD"}), "Prod-Network", "prod-dns")
	check("name wildcard is literal", by(domain.QueryParams{Name: "_"}), "staging_net")
	check("name %% is literal", by(domain.QueryParams{Name: "%"}))
	check("status", by(domain.QueryParams{Status: domain.WorkspaceStatusFailure}), "prod-dns")
	check("namespace", by(domain.QueryParams{Namespace: "team-a"}), "Prod-Network", "staging_net")
	check("actionType", by(domain.QueryParams{ActionType: domain.ActionTypeApply}), "staging_net")
	check("actionType ignores deleted actions", by(domain.QueryParams{ActionType: domain.ActionTypeDestroy}))
	check("combined", by(domain.QueryParams{Namespace: "team-a", ActionType: domain.ActionTypePlan, Name: "net"}), "Prod-Network")
}

func testListPagination(t *testing.T, s store.Store) {
	for _, n := range []string{"w01", "w02", "w03", "w04", "w05", "w06", "w07", "w08", "w09", "w10", "w11", "w12"} {
		mustWorkspace(t, s, domain.Workspace{Name: n})
	}
	sorted := domain.ListWorkspacesOptions{Sort: domain.SortByName, Order: domain.SortAsc}

	p := list(t, s, sorted)
	if len(p.Items) != domain.DefaultLimit {
		t.Fatalf("default page size = %d, want %d", len(p.Items), domain.DefaultLimit)
	}
	want := domain.Pagination{Page: 1, Limit: 10, TotalRows: 12, TotalPages: 2}
	if p.Pagination != want {
		t.Fatalf("pagination = %+v, want %+v", p.Pagination, want)
	}

	sorted.Pagination = domain.Pagination{Page: 2}
	p = list(t, s, sorted)
	wantNames(t, p.Items, "w11", "w12")

	sorted.Pagination = domain.Pagination{Page: 3, Limit: 5}
	p = list(t, s, sorted)
	wantNames(t, p.Items, "w11", "w12")
	if p.Pagination.TotalPages != 3 {
		t.Fatalf("TotalPages = %d, want 3", p.Pagination.TotalPages)
	}

	// Past the last page: empty items, totals still reported.
	sorted.Pagination = domain.Pagination{Page: 9, Limit: 5}
	p = list(t, s, sorted)
	if len(p.Items) != 0 || p.Items == nil || p.Pagination.TotalRows != 12 {
		t.Fatalf("out of range page = %+v", p)
	}
}
