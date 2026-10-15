// Package server holds the configuration and the run loop shared by the pgx
// and gorm binaries.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/workspace-api/internal/api"
	"github.com/example/workspace-api/internal/store"
)

// DefaultDatabaseURL matches the container started by `make db-up`.
const DefaultDatabaseURL = "postgres://postgres:postgres@localhost:5432/workspace-api?sslmode=disable"

// Config is read from the environment.
type Config struct {
	DatabaseURL string // DATABASE_URL
	Addr        string // ADDR, default :8080
}

// FromEnv loads the configuration.
func FromEnv() Config {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), Addr: os.Getenv("ADDR")}
	if c.DatabaseURL == "" {
		c.DatabaseURL = DefaultDatabaseURL
	}
	if c.Addr == "" {
		c.Addr = ":8080"
	}
	return c
}

// Migrator is implemented by the SQL stores.
type Migrator interface {
	Migrate(ctx context.Context) error
}

// Opener builds a store from a DSN.
type Opener func(ctx context.Context, dsn string) (store.Store, error)

// Main opens the store, applies the schema and serves until SIGINT/SIGTERM.
func Main(flavor string, open Opener) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("store", flavor)
	slog.SetDefault(logger)
	if err := run(flavor, open, logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(flavor string, open Opener, logger *slog.Logger) error {
	cfg := FromEnv()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	openCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s, err := open(openCtx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer s.Close()
	if m, ok := s.(Migrator); ok {
		if err := m.Migrate(openCtx); err != nil {
			return err
		}
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.NewHandler(s, api.Config{Title: "Workspace API (" + flavor + ")", Version: "1.0.0", Logger: logger}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr, "docs", "http://localhost"+cfg.Addr+"/docs")
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	shutdownCtx, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	return srv.Shutdown(shutdownCtx)
}
