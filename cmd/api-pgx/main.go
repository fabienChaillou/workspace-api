// Command api-pgx serves the Workspace API backed by the pgx driver.
package main

import (
	"context"

	"github.com/example/workspace-api/internal/server"
	"github.com/example/workspace-api/internal/store"
	"github.com/example/workspace-api/internal/store/pgxstore"
)

func main() {
	server.Main("pgx", func(ctx context.Context, dsn string) (store.Store, error) {
		return pgxstore.New(ctx, dsn)
	})
}
