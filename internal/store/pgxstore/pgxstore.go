// Package pgxstore implements store.Store with the pgx v5 driver and plain SQL.
package pgxstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/example/workspace-api/internal/domain"
	"github.com/example/workspace-api/internal/migrations"
	"github.com/example/workspace-api/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the pgx implementation of store.Store.
type Store struct {
	pool *pgxpool.Pool
}

var _ store.Store = (*Store)(nil)

// New opens a connection pool and checks connectivity.
func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgx: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pgx: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Migrate applies the shared schema.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, migrations.Schema); err != nil {
		return fmt.Errorf("pgx: migrate: %w", err)
	}
	return nil
}

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *Store) Close() error {
	s.pool.Close()
	return nil
}

// ---------------------------------------------------------------- workspaces

const workspaceColumns = `w.id, w.name, w.vcs_url, w.vcs_key_str, w.vcs_branch, w.vcs_directory,
	w.status, w.env_vars::text, w.namespace, w.tofu_version, w.created_at, w.updated_at`

func scanWorkspace(row pgx.Row, extra ...any) (domain.Workspace, error) {
	var (
		w      domain.Workspace
		status string
		env    string
	)
	dest := append([]any{
		&w.ID, &w.Name, &w.VcsURL, &w.VcsKeyStr, &w.VcsBranch, &w.VcsDirectory,
		&status, &env, &w.Namespace, &w.TofuVersion, &w.CreatedAt, &w.UpdatedAt,
	}, extra...)
	if err := row.Scan(dest...); err != nil {
		return w, err
	}
	w.Status = domain.WorkspaceStatus(status)
	vars, err := domain.DecodeEnvVars(env)
	if err != nil {
		return w, err
	}
	w.EnvVars = vars
	return w, nil
}

func (s *Store) CreateWorkspace(ctx context.Context, in domain.Workspace) (domain.Workspace, error) {
	env, err := domain.EncodeEnvVars(in.EnvVars)
	if err != nil {
		return domain.Workspace{}, err
	}
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	if in.Status == "" {
		in.Status = domain.WorkspaceStatusActive
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO workspaces AS w
			(id, name, vcs_url, vcs_key_str, vcs_branch, vcs_directory, status, env_vars, namespace, tofu_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10)
		RETURNING `+workspaceColumns,
		in.ID, in.Name, in.VcsURL, in.VcsKeyStr, in.VcsBranch, in.VcsDirectory,
		string(in.Status), env, in.Namespace, in.TofuVersion)
	w, err := scanWorkspace(row)
	if err != nil {
		return domain.Workspace{}, mapErr(err, "workspace")
	}
	w.Actions, w.Variables = []domain.Action{}, []domain.Variable{}
	return w, nil
}

func (s *Store) GetWorkspace(ctx context.Context, id uuid.UUID) (domain.Workspace, error) {
	// One read-only snapshot so the workspace and its children are consistent.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return domain.Workspace{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only

	var lastActionAt *time.Time
	row := tx.QueryRow(ctx, `
		SELECT `+workspaceColumns+`, la.last_action_at
		FROM workspaces w `+store.LastActionJoin+`
		WHERE w.id = $1 AND w.deleted_at IS NULL`, id)
	w, err := scanWorkspace(row, &lastActionAt)
	if err != nil {
		return domain.Workspace{}, mapErr(err, "workspace "+id.String())
	}
	w.LastActionAt = lastActionAt

	if w.Actions, err = listActions(ctx, tx, id); err != nil {
		return domain.Workspace{}, err
	}
	if w.Variables, err = listVariables(ctx, tx, id); err != nil {
		return domain.Workspace{}, err
	}
	return w, nil
}

func (s *Store) ListWorkspaces(ctx context.Context, opts domain.ListWorkspacesOptions) (domain.WorkspacePage, error) {
	where, args := store.WorkspaceWhere(opts.Filter)

	var total int64
	if err := s.pool.QueryRow(ctx,
		store.Dollarize(`SELECT count(*) FROM workspaces w WHERE `+where, 1), args...,
	).Scan(&total); err != nil {
		return domain.WorkspacePage{}, fmt.Errorf("count workspaces: %w", err)
	}

	page := domain.WorkspacePage{Items: []domain.Workspace{}, Pagination: opts.Pagination.WithTotal(total)}
	if total == 0 || opts.Pagination.Offset() >= int(total) {
		return page, nil
	}

	n := len(args)
	query := `SELECT ` + workspaceColumns + `, la.last_action_at
		FROM workspaces w ` + store.LastActionJoin + `
		WHERE ` + store.Dollarize(where, 1) + `
		ORDER BY ` + store.WorkspaceOrderBy(opts) + fmt.Sprintf(`
		LIMIT $%d OFFSET $%d`, n+1, n+2)
	rows, err := s.pool.Query(ctx, query, append(args, opts.Pagination.Limit, opts.Pagination.Offset())...)
	if err != nil {
		return domain.WorkspacePage{}, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var lastActionAt *time.Time
		w, err := scanWorkspace(rows, &lastActionAt)
		if err != nil {
			return domain.WorkspacePage{}, err
		}
		w.LastActionAt = lastActionAt
		page.Items = append(page.Items, w)
	}
	if err := rows.Err(); err != nil {
		return domain.WorkspacePage{}, err
	}
	return page, nil
}

func (s *Store) UpdateWorkspace(ctx context.Context, id uuid.UUID, p domain.WorkspacePatch) (domain.Workspace, error) {
	var env *string
	if p.EnvVars != nil {
		e, err := domain.EncodeEnvVars(*p.EnvVars)
		if err != nil {
			return domain.Workspace{}, err
		}
		env = &e
	}
	var status *string
	if p.Status != nil {
		st := string(*p.Status)
		status = &st
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE workspaces SET
			name          = COALESCE($2, name),
			vcs_url       = COALESCE($3, vcs_url),
			vcs_key_str   = COALESCE($4, vcs_key_str),
			vcs_branch    = COALESCE($5, vcs_branch),
			vcs_directory = COALESCE($6, vcs_directory),
			status        = COALESCE($7, status),
			env_vars      = COALESCE($8::jsonb, env_vars),
			namespace     = COALESCE($9, namespace),
			tofu_version  = COALESCE($10, tofu_version),
			updated_at    = now()
		WHERE id = $1 AND deleted_at IS NULL`,
		id, p.Name, p.VcsURL, p.VcsKeyStr, p.VcsBranch, p.VcsDirectory, status, env, p.Namespace, p.TofuVersion)
	if err != nil {
		return domain.Workspace{}, mapErr(err, "workspace "+id.String())
	}
	if tag.RowsAffected() == 0 {
		return domain.Workspace{}, fmt.Errorf("workspace %s: %w", id, domain.ErrNotFound)
	}
	return s.GetWorkspace(ctx, id)
}

func (s *Store) DeleteWorkspace(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE workspaces SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("workspace %s: %w", id, domain.ErrNotFound)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE actions SET deleted_at = now() WHERE workspace_id = $1 AND deleted_at IS NULL`, id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`UPDATE variables SET deleted_at = now() WHERE workspace_id = $1 AND deleted_at IS NULL`, id)
		return err
	})
}

// requireWorkspace returns ErrNotFound unless the workspace exists and is live.
// FOR SHARE blocks a concurrent DeleteWorkspace until the caller's tx ends.
func requireWorkspace(ctx context.Context, q querier, id uuid.UUID) error {
	var one int
	err := q.QueryRow(ctx,
		`SELECT 1 FROM workspaces WHERE id = $1 AND deleted_at IS NULL FOR SHARE`, id).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("workspace %s: %w", id, domain.ErrNotFound)
	}
	return err
}

// ------------------------------------------------------------------- actions

const actionColumns = `id, workspace_id, type, status, created_at, updated_at`

func scanAction(row pgx.Row) (domain.Action, error) {
	var (
		a   domain.Action
		typ string
	)
	err := row.Scan(&a.ID, &a.WorkspaceID, &typ, &a.Status, &a.CreatedAt, &a.UpdatedAt)
	a.Type = domain.ActionType(typ)
	return a, err
}

func (s *Store) CreateAction(ctx context.Context, in domain.Action) (domain.Action, error) {
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	var out domain.Action
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := requireWorkspace(ctx, tx, in.WorkspaceID); err != nil {
			return err
		}
		var err error
		out, err = scanAction(tx.QueryRow(ctx, `
			INSERT INTO actions (id, workspace_id, type, status)
			VALUES ($1, $2, $3, $4)
			RETURNING `+actionColumns,
			in.ID, in.WorkspaceID, string(in.Type), in.Status))
		return err
	})
	if err != nil {
		return domain.Action{}, mapErr(err, "action")
	}
	return out, nil
}

func (s *Store) GetAction(ctx context.Context, workspaceID, id uuid.UUID) (domain.Action, error) {
	a, err := scanAction(s.pool.QueryRow(ctx, `
		SELECT `+actionColumns+` FROM actions
		WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL`, id, workspaceID))
	if err != nil {
		return domain.Action{}, mapErr(err, "action "+id.String())
	}
	return a, nil
}

func (s *Store) ListActions(ctx context.Context, workspaceID uuid.UUID) ([]domain.Action, error) {
	var out []domain.Action
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var one int
		err := tx.QueryRow(ctx,
			`SELECT 1 FROM workspaces WHERE id = $1 AND deleted_at IS NULL`, workspaceID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("workspace %s: %w", workspaceID, domain.ErrNotFound)
		}
		if err != nil {
			return err
		}
		out, err = listActions(ctx, tx, workspaceID)
		return err
	})
	return out, err
}

func listActions(ctx context.Context, q querier, workspaceID uuid.UUID) ([]domain.Action, error) {
	rows, err := q.Query(ctx, `
		SELECT `+actionColumns+` FROM actions
		WHERE workspace_id = $1 AND deleted_at IS NULL
		ORDER BY updated_at DESC, id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list actions: %w", err)
	}
	defer rows.Close()
	out := []domain.Action{}
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) UpdateAction(ctx context.Context, workspaceID, id uuid.UUID, p domain.ActionPatch) (domain.Action, error) {
	var typ *string
	if p.Type != nil {
		t := string(*p.Type)
		typ = &t
	}
	a, err := scanAction(s.pool.QueryRow(ctx, `
		UPDATE actions SET
			type       = COALESCE($3, type),
			status     = COALESCE($4, status),
			updated_at = now()
		WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL
		RETURNING `+actionColumns, id, workspaceID, typ, p.Status))
	if err != nil {
		return domain.Action{}, mapErr(err, "action "+id.String())
	}
	return a, nil
}

func (s *Store) DeleteAction(ctx context.Context, workspaceID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE actions SET deleted_at = now()
		WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL`, id, workspaceID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("action %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// ----------------------------------------------------------------- variables

const variableColumns = `workspace_id, name, status, created_at, updated_at`

func scanVariable(row pgx.Row) (domain.Variable, error) {
	var v domain.Variable
	err := row.Scan(&v.WorkspaceID, &v.Name, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}

func (s *Store) CreateVariable(ctx context.Context, in domain.Variable) (domain.Variable, error) {
	var out domain.Variable
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := requireWorkspace(ctx, tx, in.WorkspaceID); err != nil {
			return err
		}
		var err error
		out, err = scanVariable(tx.QueryRow(ctx, `
			INSERT INTO variables (workspace_id, name, status)
			VALUES ($1, $2, $3)
			RETURNING `+variableColumns,
			in.WorkspaceID, in.Name, in.Status))
		return err
	})
	if err != nil {
		return domain.Variable{}, mapErr(err, "variable "+in.Name)
	}
	return out, nil
}

func (s *Store) GetVariable(ctx context.Context, workspaceID uuid.UUID, name string) (domain.Variable, error) {
	v, err := scanVariable(s.pool.QueryRow(ctx, `
		SELECT `+variableColumns+` FROM variables
		WHERE workspace_id = $1 AND name = $2 AND deleted_at IS NULL`, workspaceID, name))
	if err != nil {
		return domain.Variable{}, mapErr(err, "variable "+name)
	}
	return v, nil
}

func (s *Store) ListVariables(ctx context.Context, workspaceID uuid.UUID) ([]domain.Variable, error) {
	var out []domain.Variable
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var one int
		err := tx.QueryRow(ctx,
			`SELECT 1 FROM workspaces WHERE id = $1 AND deleted_at IS NULL`, workspaceID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("workspace %s: %w", workspaceID, domain.ErrNotFound)
		}
		if err != nil {
			return err
		}
		out, err = listVariables(ctx, tx, workspaceID)
		return err
	})
	return out, err
}

func listVariables(ctx context.Context, q querier, workspaceID uuid.UUID) ([]domain.Variable, error) {
	rows, err := q.Query(ctx, `
		SELECT `+variableColumns+` FROM variables
		WHERE workspace_id = $1 AND deleted_at IS NULL
		ORDER BY name`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list variables: %w", err)
	}
	defer rows.Close()
	out := []domain.Variable{}
	for rows.Next() {
		v, err := scanVariable(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpdateVariable(ctx context.Context, workspaceID uuid.UUID, name string, p domain.VariablePatch) (domain.Variable, error) {
	v, err := scanVariable(s.pool.QueryRow(ctx, `
		UPDATE variables SET
			status     = COALESCE($3, status),
			updated_at = now()
		WHERE workspace_id = $1 AND name = $2 AND deleted_at IS NULL
		RETURNING `+variableColumns, workspaceID, name, p.Status))
	if err != nil {
		return domain.Variable{}, mapErr(err, "variable "+name)
	}
	return v, nil
}

func (s *Store) DeleteVariable(ctx context.Context, workspaceID uuid.UUID, name string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE variables SET deleted_at = now()
		WHERE workspace_id = $1 AND name = $2 AND deleted_at IS NULL`, workspaceID, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("variable %s: %w", name, domain.ErrNotFound)
	}
	return nil
}

// ------------------------------------------------------------------- helpers

// querier is satisfied by *pgxpool.Pool and pgx.Tx.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// mapErr translates driver errors into domain errors.
func mapErr(err error, what string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrInvalid) {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", what, domain.ErrNotFound)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%s already exists: %w", what, domain.ErrConflict)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%s: parent workspace: %w", what, domain.ErrNotFound)
		case "23514", "22P02": // check_violation, invalid_text_representation
			return fmt.Errorf("%s: %s: %w", what, pgErr.Message, domain.ErrInvalid)
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}
