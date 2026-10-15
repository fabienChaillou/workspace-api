// Package fixtures loads the development data set (fixtures.sql). The data is
// plain SQL on the shared schema, so it serves both the pgx and gorm versions.
package fixtures

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// SQL is the fixture script. It is idempotent: existing rows are skipped.
//
//go:embed fixtures.sql
var SQL string

// IDs of the fixture workspaces, handy for curl examples and tests.
const (
	PlatformNetwork    = "a0000000-0000-4000-8000-000000000001"
	PlatformDNS        = "a0000000-0000-4000-8000-000000000002"
	PlatformK8sCluster = "a0000000-0000-4000-8000-000000000003"
	PaymentsAPI        = "a0000000-0000-4000-8000-000000000004"
	PaymentsDB         = "a0000000-0000-4000-8000-000000000005"
	PaymentsSandbox    = "a0000000-0000-4000-8000-000000000006"
	DataWarehouse      = "a0000000-0000-4000-8000-000000000007"
	DataAirflow        = "a0000000-0000-4000-8000-000000000008"
	DataLakeNew        = "a0000000-0000-4000-8000-000000000009"
	SandboxAlice       = "a0000000-0000-4000-8000-000000000010"
	LegacyMonitoring   = "a0000000-0000-4000-8000-000000000011"
	OldVPN             = "a0000000-0000-4000-8000-000000000012" // soft-deleted
)

// Execer is satisfied by *pgx.Conn, *pgxpool.Pool and pgx.Tx.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Load inserts the fixtures. The schema must already exist.
func Load(ctx context.Context, db Execer) error {
	if _, err := db.Exec(ctx, SQL); err != nil {
		return fmt.Errorf("load fixtures: %w", err)
	}
	return nil
}

// Reset empties every table. It deletes ALL data, not only the fixtures.
func Reset(ctx context.Context, db Execer) error {
	if _, err := db.Exec(ctx, "TRUNCATE variables, actions, workspaces"); err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	return nil
}
