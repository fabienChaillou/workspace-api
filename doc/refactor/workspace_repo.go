package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Workspace, Action, etc. sont supposés définis dans ce package
// (ou importés depuis ton package domaine).

type WorkspaceRepo struct {
	pool *pgxpool.Pool
}

func NewWorkspaceRepo(pool *pgxpool.Pool) *WorkspaceRepo {
	return &WorkspaceRepo{pool: pool}
}

// ListWorkspacesFilter : un champ nil ou vide = pas de filtre.
type ListWorkspacesFilter struct {
	Name       *string // recherche partielle, insensible à la casse
	ActionType *string // workspaces ayant au moins une action de ce type
	Namespace  *string // égalité stricte
	Page       int     // commence à 1
	Limit      int
}

type Pagination struct {
	TotalWorkspaces int `json:"totalWorkspaces"`
	Page            int `json:"page"`
	Limit           int `json:"limit"`
}

const (
	defaultLimit = 20
	maxLimit     = 100
)

// Conditions partagées entre la liste et le comptage.
// $1 = name, $2 = namespace, $3 = action type.
const workspaceFilterSQL = `
WHERE ($1::text IS NULL OR w.name ILIKE '%' || $1 || '%')
  AND ($2::text IS NULL OR w.namespace = $2)
  AND ($3::text IS NULL OR EXISTS (
        SELECT 1 FROM actions a
        WHERE a.workspace_id = w.id AND a.type = $3
      ))`

const listWorkspacesSQL = `
SELECT w.id, w.name, w.vcs_url, w.vcs_key_str, w.vcs_branch, w.vcs_directory,
       w.status, w.env_vars, w.namespace, w.tofu_version, w.last_action_at,
       w.created_at, w.updated_at,
       count(*) OVER() AS total
FROM workspaces w` + workspaceFilterSQL + `
ORDER BY w.last_action_at DESC NULLS LAST, w.id
LIMIT $4 OFFSET $5`

const countWorkspacesSQL = `SELECT count(*) FROM workspaces w` + workspaceFilterSQL

// ListWorkspaces renvoie une page de workspaces triés par dernière action
// (les plus récentes d'abord, les workspaces sans action à la fin).
func (r *WorkspaceRepo) ListWorkspaces(ctx context.Context, f ListWorkspacesFilter) ([]Workspace, Pagination, error) {
	page, limit := normalizePage(f.Page, f.Limit)
	filterArgs := workspaceFilterArgs(f)
	queryArgs := append(append([]any{}, filterArgs...), limit, (page-1)*limit)

	rows, err := r.pool.Query(ctx, listWorkspacesSQL, queryArgs...)
	if err != nil {
		return nil, Pagination{}, fmt.Errorf("list workspaces: %w", err)
	}

	var total int
	workspaces, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Workspace, error) {
		var w Workspace
		err := row.Scan(
			&w.ID, &w.Name, &w.VcsURL, &w.VcsKeyStr, &w.VcsBranch, &w.VcsDirectory,
			&w.Status, &w.EnvVars, &w.Namespace, &w.TofuVersion, &w.LastActionAt,
			&w.CreatedAt, &w.UpdatedAt,
			&total,
		)
		return w, err
	})
	if err != nil {
		return nil, Pagination{}, fmt.Errorf("scan workspaces: %w", err)
	}

	// Page au-delà de la fin : la window function ne renvoie aucune ligne,
	// donc pas de total. On le récupère avec une requête de comptage.
	if len(workspaces) == 0 && page > 1 {
		total, err = r.CountWorkspaces(ctx, f)
		if err != nil {
			return nil, Pagination{}, err
		}
	}

	return workspaces, Pagination{TotalWorkspaces: total, Page: page, Limit: limit}, nil
}

// CountWorkspaces renvoie le nombre total de workspaces correspondant aux filtres.
func (r *WorkspaceRepo) CountWorkspaces(ctx context.Context, f ListWorkspacesFilter) (int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, countWorkspacesSQL, workspaceFilterArgs(f)...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count workspaces: %w", err)
	}
	return total, nil
}

// workspaceFilterArgs construit $1, $2, $3. Un *string nil est envoyé comme NULL.
func workspaceFilterArgs(f ListWorkspacesFilter) []any {
	var name *string
	if f.Name != nil && *f.Name != "" {
		escaped := escapeLike(*f.Name)
		name = &escaped
	}
	return []any{name, nilIfEmpty(f.Namespace), nilIfEmpty(f.ActionType)}
}

func normalizePage(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return page, limit
}

func nilIfEmpty(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

// escapeLike évite que % et _ saisis par l'utilisateur soient
// interprétés comme des jokers par ILIKE.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func escapeLike(s string) string {
	return likeEscaper.Replace(s)
}
