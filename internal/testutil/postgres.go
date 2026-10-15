// Package testutil provides helpers for the integration tests. Each test
// package gets its own throw-away database on the PostgreSQL server started by
// `make db-up`, so packages can run in parallel without interfering.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// DefaultDSN matches the container started by `make db-up`.
const DefaultDSN = "postgres://postgres:postgres@localhost:5432/workspace-api?sslmode=disable"

// AdminDSN is the server used to create test databases. Override with
// TEST_DATABASE_URL.
func AdminDSN() string {
	if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return DefaultDSN
}

// Database is a temporary database dropped at the end of the test.
type Database struct {
	DSN  string
	Name string
}

// NewDatabase creates an empty database and registers its removal.
func NewDatabase(t testing.TB) *Database {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, AdminDSN())
	if err != nil {
		t.Fatalf("PostgreSQL is not reachable at %s (start it with `make db-up`): %v", AdminDSN(), err)
	}
	defer admin.Close(ctx)

	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	name := "wsapi_test_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, AdminDSN())
		if err != nil {
			t.Logf("drop test database %s: %v", name, err)
			return
		}
		defer conn.Close(ctx)
		if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Logf("drop test database %s: %v", name, err)
		}
	})

	u, err := url.Parse(AdminDSN())
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	u.Path = "/" + name
	return &Database{DSN: u.String(), Name: name}
}

// Truncate empties every table of the database.
func (d *Database) Truncate(t testing.TB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, d.DSN)
	if err != nil {
		t.Fatalf("connect %s: %v", d.Name, err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "TRUNCATE variables, actions, workspaces"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}
