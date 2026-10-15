// Command api-gorm serves the Workspace API backed by gorm.
package main

import (
	"context"

	"github.com/example/workspace-api/internal/server"
	"github.com/example/workspace-api/internal/store"
	"github.com/example/workspace-api/internal/store/gormstore"
)

func main() {
	server.Main("gorm", func(ctx context.Context, dsn string) (store.Store, error) {
		return gormstore.New(ctx, dsn)
	})
}
