//go:build integration

package gormstore_test

import (
	"context"
	"testing"

	"github.com/example/workspace-api/internal/store"
	"github.com/example/workspace-api/internal/store/gormstore"
	"github.com/example/workspace-api/internal/store/storetest"
	"github.com/example/workspace-api/internal/testutil"
)

func TestContract(t *testing.T) {
	db := testutil.NewDatabase(t)

	s, err := gormstore.New(context.Background(), db.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	storetest.Run(t, func(t *testing.T) store.Store {
		db.Truncate(t)
		return s
	})
}
