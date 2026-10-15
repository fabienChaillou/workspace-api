package store

import (
	"strings"
	"testing"

	"github.com/example/workspace-api/internal/domain"
)

func TestWorkspaceOrderBy(t *testing.T) {
	cases := map[string]domain.ListWorkspacesOptions{
		"la.last_action_at DESC NULLS LAST, w.updated_at DESC, w.id ASC": {Sort: domain.SortByActionsUpdatedAt, Order: domain.SortDesc},
		"la.last_action_at ASC NULLS LAST, w.updated_at DESC, w.id ASC":  {Sort: domain.SortByActionsUpdatedAt, Order: domain.SortAsc},
		"w.name ASC NULLS LAST, w.updated_at DESC, w.id ASC":             {Sort: domain.SortByName, Order: domain.SortAsc},
		"w.created_at DESC NULLS LAST, w.updated_at DESC, w.id ASC":      {Sort: domain.SortByCreatedAt, Order: domain.SortDesc},
		// Unknown values never reach SQL: they fall back to the default column.
		"la.last_action_at DESC NULLS LAST, w.updated_at DESC, w.id ASC; ": {Sort: "x; DROP TABLE workspaces", Order: "desc"},
	}
	for want, o := range cases {
		if got := WorkspaceOrderBy(o); got != strings.TrimSuffix(want, "; ") {
			t.Fatalf("WorkspaceOrderBy(%+v) = %q", o, got)
		}
	}
}

func TestWorkspaceWhere(t *testing.T) {
	where, args := WorkspaceWhere(domain.QueryParams{})
	if where != "w.deleted_at IS NULL" || len(args) != 0 {
		t.Fatalf("no filter: %q %v", where, args)
	}

	where, args = WorkspaceWhere(domain.QueryParams{
		Name: "50%_off", Status: domain.WorkspaceStatusActive, Namespace: "ns", ActionType: domain.ActionTypePlan,
	})
	if strings.Count(where, "?") != 4 || len(args) != 4 {
		t.Fatalf("placeholders/args mismatch: %q %v", where, args)
	}
	if args[0] != `%50\%\_off%` || args[1] != "ACTIVE" || args[2] != "ns" || args[3] != "plan" {
		t.Fatalf("args = %v", args)
	}
	for _, frag := range []string{"w.name ILIKE ?", "w.status = ?", "w.namespace = ?", "fa.type = ?", "fa.deleted_at IS NULL"} {
		if !strings.Contains(where, frag) {
			t.Fatalf("where %q misses %q", where, frag)
		}
	}
}

func TestDollarize(t *testing.T) {
	if got := Dollarize("a = ? AND b = ? OR c = ?", 3); got != "a = $3 AND b = $4 OR c = $5" {
		t.Fatalf("Dollarize = %q", got)
	}
}

func TestEscapeLike(t *testing.T) {
	if got := EscapeLike(`a%b_c\d`); got != `a\%b\_c\\d` {
		t.Fatalf("EscapeLike = %q", got)
	}
}
