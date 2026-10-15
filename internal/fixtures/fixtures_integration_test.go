//go:build integration

package fixtures_test

import (
	"context"
	"strings"
	"testing"

	"github.com/example/workspace-api/internal/domain"
	"github.com/example/workspace-api/internal/fixtures"
	"github.com/example/workspace-api/internal/migrations"
	"github.com/example/workspace-api/internal/store"
	"github.com/example/workspace-api/internal/store/gormstore"
	"github.com/example/workspace-api/internal/store/pgxstore"
	"github.com/example/workspace-api/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Documented default order of GET /workspaces right after `make db-reseed`.
var wantDefaultOrder = []string{
	"payments-db",          // plan running, 5 min
	"platform-k8s-cluster", // apply failed, 10 min
	"data-airflow",         // plan queued, 30 min
	"platform-network",     // apply, 1h45
	"payments-api",         // apply, 23h
	"data-warehouse",       // apply, 2 days
	"platform-dns",         // apply, 3 days (its 1-min destroy is deleted)
	"sandbox-alice",        // destroy, 6 days
	"legacy-monitoring",    // destroy, 45 days
	"data-lake-new",        // no action, updated 1h ago
	"payments-sandbox",     // no action, updated 10 days ago
}

func TestFixtures(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDatabase(t)

	conn, err := pgx.Connect(ctx, db.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(ctx) })
	if _, err := conn.Exec(ctx, migrations.Schema); err != nil {
		t.Fatal(err)
	}
	if err := fixtures.Load(ctx, conn); err != nil {
		t.Fatal(err)
	}

	t.Run("idempotent", func(t *testing.T) {
		count := func() (n int) {
			if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM workspaces) + (SELECT count(*) FROM actions) + (SELECT count(*) FROM variables)`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		before := count()
		if err := fixtures.Load(ctx, conn); err != nil {
			t.Fatalf("second load: %v", err)
		}
		if after := count(); after != before {
			t.Fatalf("second load added rows: %d -> %d", before, after)
		}
	})

	pgxS, err := pgxstore.New(ctx, db.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pgxS.Close() })
	gormS, err := gormstore.New(ctx, db.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gormS.Close() })

	for name, s := range map[string]store.Store{"pgx": pgxS, "gorm": gormS} {
		t.Run(name, func(t *testing.T) { checkFixtures(t, s) })
	}

	t.Run("reset", func(t *testing.T) {
		if err := fixtures.Reset(ctx, conn); err != nil {
			t.Fatal(err)
		}
		if err := fixtures.Load(ctx, conn); err != nil {
			t.Fatal(err)
		}
		checkFixtures(t, pgxS)
	})
}

func checkFixtures(t *testing.T, s store.Store) {
	ctx := context.Background()
	list := func(o domain.ListWorkspacesOptions) domain.WorkspacePage {
		t.Helper()
		o, err := o.Normalize()
		if err != nil {
			t.Fatal(err)
		}
		p, err := s.ListWorkspaces(ctx, o)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	names := func(p domain.WorkspacePage) string {
		n := make([]string, len(p.Items))
		for i, w := range p.Items {
			n[i] = w.Name
		}
		return strings.Join(n, ",")
	}

	// Default order, over two pages of 10.
	p1 := list(domain.ListWorkspacesOptions{})
	p2 := list(domain.ListWorkspacesOptions{Pagination: domain.Pagination{Page: 2}})
	if got, want := names(p1)+","+names(p2), strings.Join(wantDefaultOrder, ","); got != want {
		t.Fatalf("default order:\n got  %s\n want %s", got, want)
	}
	if want := (domain.Pagination{Page: 1, Limit: 10, TotalRows: 11, TotalPages: 2}); p1.Pagination != want {
		t.Fatalf("pagination = %+v", p1.Pagination)
	}

	filters := []struct {
		f    domain.QueryParams
		want int
	}{
		{domain.QueryParams{Status: domain.WorkspaceStatusActive}, 7},
		{domain.QueryParams{Status: domain.WorkspaceStatusFailure}, 1},
		{domain.QueryParams{Status: domain.WorkspaceStatusInactive}, 2},
		{domain.QueryParams{Status: domain.WorkspaceStatusDeleted}, 1},
		{domain.QueryParams{Namespace: "platform"}, 3},
		{domain.QueryParams{Namespace: "team-payments"}, 3},
		{domain.QueryParams{Namespace: "team-data"}, 3},
		{domain.QueryParams{Namespace: "ops"}, 1}, // old-vpn is soft-deleted
		{domain.QueryParams{Name: "payments"}, 3},
		{domain.QueryParams{ActionType: domain.ActionTypeInit}, 4},
		{domain.QueryParams{ActionType: domain.ActionTypePlan}, 8},
		{domain.QueryParams{ActionType: domain.ActionTypeApply}, 6},
		{domain.QueryParams{ActionType: domain.ActionTypeDestroy}, 2}, // platform-dns destroy is deleted
		{domain.QueryParams{Namespace: "team-payments", ActionType: domain.ActionTypeApply}, 1},
	}
	for _, c := range filters {
		if got := list(domain.ListWorkspacesOptions{Filter: c.f}).Pagination.TotalRows; got != int64(c.want) {
			t.Errorf("filter %+v: %d workspaces, want %d", c.f, got, c.want)
		}
	}

	k8s, err := s.GetWorkspace(ctx, uuid.MustParse(fixtures.PlatformK8sCluster))
	if err != nil {
		t.Fatal(err)
	}
	if k8s.Status != domain.WorkspaceStatusFailure || len(k8s.Actions) != 2 || k8s.Actions[0].Status != "failed" ||
		len(k8s.Variables) != 2 || len(k8s.EnvVars) != 3 {
		t.Fatalf("platform-k8s-cluster = %+v", k8s)
	}

	dns, err := s.GetWorkspace(ctx, uuid.MustParse(fixtures.PlatformDNS))
	if err != nil {
		t.Fatal(err)
	}
	if len(dns.Actions) != 2 || !dns.EnvVars[1].Secure || !dns.EnvVars[1].Hidden {
		t.Fatalf("platform-dns = %+v", dns)
	}

	_, err = s.GetWorkspace(ctx, uuid.MustParse(fixtures.OldVPN))
	if err == nil {
		t.Fatal("soft-deleted workspace is visible")
	}
}
