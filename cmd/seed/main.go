// Command seed applies the schema and loads the development fixtures into
// DATABASE_URL. With -reset, every table is emptied first (all data is lost)
// so timestamps are fresh and the default list ordering is the documented one.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/example/workspace-api/internal/fixtures"
	"github.com/example/workspace-api/internal/migrations"
	"github.com/example/workspace-api/internal/server"
	"github.com/jackc/pgx/v5"
)

func main() {
	reset := flag.Bool("reset", false, "empty all tables before loading (DELETES ALL DATA)")
	flag.Parse()
	if err := run(*reset); err != nil {
		log.Fatal(err)
	}
}

func run(reset bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := server.FromEnv()
	conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, migrations.Schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if reset {
		if err := fixtures.Reset(ctx, conn); err != nil {
			return err
		}
	}
	if err := fixtures.Load(ctx, conn); err != nil {
		return err
	}

	var ws, actions, vars int
	err = conn.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM workspaces WHERE deleted_at IS NULL),
		(SELECT count(*) FROM actions    WHERE deleted_at IS NULL),
		(SELECT count(*) FROM variables  WHERE deleted_at IS NULL)`).Scan(&ws, &actions, &vars)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "fixtures loaded: %d workspaces, %d actions, %d variables (live rows)\n", ws, actions, vars)
	return nil
}
