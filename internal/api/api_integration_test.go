//go:build integration

package api_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/example/workspace-api/internal/api"
	"github.com/example/workspace-api/internal/store"
	"github.com/example/workspace-api/internal/store/gormstore"
	"github.com/example/workspace-api/internal/store/pgxstore"
	"github.com/example/workspace-api/internal/testutil"
)

type migratingStore interface {
	store.Store
	Migrate(context.Context) error
}

// End-to-end scenario over HTTP, run once per driver on a real PostgreSQL.
func TestEndToEnd(t *testing.T) {
	drivers := map[string]func(ctx context.Context, dsn string) (migratingStore, error){
		"pgx":  func(ctx context.Context, dsn string) (migratingStore, error) { return pgxstore.New(ctx, dsn) },
		"gorm": func(ctx context.Context, dsn string) (migratingStore, error) { return gormstore.New(ctx, dsn) },
	}
	for name, open := range drivers {
		t.Run(name, func(t *testing.T) {
			db := testutil.NewDatabase(t)
			s, err := open(context.Background(), db.DSN)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close() })
			if err := s.Migrate(context.Background()); err != nil {
				t.Fatal(err)
			}
			scenario(t, s)
		})
	}
}

func scenario(t *testing.T, s store.Store) {
	tapi := newAPI(t, s)

	alpha := createWorkspace(t, tapi, map[string]any{
		"name": "alpha", "namespace": "team-a", "tofuVersion": "1.8.0",
		"envVars": []map[string]any{{"name": "AWS_REGION", "value": "eu-west-3"}, {"name": "TOKEN", "value": "x", "secure": true, "hidden": true}},
	})
	bravo := createWorkspace(t, tapi, map[string]any{"name": "bravo", "namespace": "team-b", "status": "FAILURE"})
	createWorkspace(t, tapi, map[string]any{"name": "charlie", "namespace": "team-a"})

	post := func(path string, body map[string]any, want int) {
		t.Helper()
		resp := tapi.Post(path, body)
		wantStatus(t, resp.Code, want, resp.Body.String())
	}
	post("/workspaces/"+bravo.ID+"/actions", map[string]any{"type": "plan"}, http.StatusCreated)
	post("/workspaces/"+alpha.ID+"/actions", map[string]any{"type": "apply"}, http.StatusCreated)
	post("/workspaces/"+alpha.ID+"/variables", map[string]any{"name": "e2e_region"}, http.StatusCreated)
	post("/workspaces/"+bravo.ID+"/variables", map[string]any{"name": "e2e_region"}, http.StatusConflict)
	post("/workspaces/"+alpha.ID+"/actions", map[string]any{"type": "refresh"}, http.StatusUnprocessableEntity)

	resp := tapi.Get("/workspaces")
	wantStatus(t, resp.Code, http.StatusOK, resp.Body.String())
	l := decode[listResponse](t, resp.Body)
	if len(l.Items) != 3 || l.Items[0].Name != "alpha" || l.Items[1].Name != "bravo" || l.Items[2].Name != "charlie" {
		t.Fatalf("default order: %+v", l.Items)
	}
	if l.Pagination.TotalRows != 3 || l.Pagination.Limit != 10 || l.Pagination.TotalPages != 1 {
		t.Fatalf("pagination: %+v", l.Pagination)
	}

	resp = tapi.Get("/workspaces?namespace=team-a&actionType=apply&status=ACTIVE&name=ALP")
	if l := decode[listResponse](t, resp.Body); len(l.Items) != 1 || l.Items[0].ID != alpha.ID {
		t.Fatalf("filters: %+v", l.Items)
	}

	resp = tapi.Get("/workspaces/" + alpha.ID)
	d := decode[api.WorkspaceDetail](t, resp.Body)
	if len(d.Actions) != 1 || len(d.Variables) != 1 || len(d.EnvVars) != 2 || !d.EnvVars[1].Hidden || d.LastActionAt == nil {
		t.Fatalf("detail: %+v", d)
	}

	resp = tapi.Delete("/workspaces/" + alpha.ID)
	wantStatus(t, resp.Code, http.StatusNoContent, resp.Body.String())
	post("/workspaces/"+alpha.ID+"/actions", map[string]any{"type": "plan"}, http.StatusNotFound)
	post("/workspaces/"+bravo.ID+"/variables", map[string]any{"name": "e2e_region"}, http.StatusCreated)

	resp = tapi.Get("/workspaces?page=1&limit=1")
	if l := decode[listResponse](t, resp.Body); len(l.Items) != 1 || l.Pagination.TotalRows != 2 || l.Pagination.TotalPages != 2 {
		t.Fatalf("after delete: %+v", l)
	}

	// Concurrent creation of the same variable name: exactly one wins.
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- tapi.Post("/workspaces/"+bravo.ID+"/variables", map[string]any{"name": "race"}).Code
		}()
	}
	wg.Wait()
	close(codes)
	created := 0
	for c := range codes {
		switch c {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
		default:
			t.Fatalf("unexpected status %d", c)
		}
	}
	if created != 1 {
		t.Fatalf("%d concurrent creations succeeded, want 1", created)
	}
}
