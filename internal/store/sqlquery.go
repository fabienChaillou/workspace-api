package store

import (
	"fmt"
	"strings"

	"github.com/example/workspace-api/internal/domain"
)

// LastActionJoin computes, for each workspace aliased "w", the most recent
// UpdatedAt of its live actions as la.last_action_at (NULL when it has none).
// It is backed by the partial index idx_actions_workspace_updated.
const LastActionJoin = `LEFT JOIN LATERAL (
	SELECT max(a.updated_at) AS last_action_at
	FROM actions a
	WHERE a.workspace_id = w.id AND a.deleted_at IS NULL
) la ON true`

var sortColumns = map[domain.SortField]string{
	domain.SortByActionsUpdatedAt: "la.last_action_at",
	domain.SortByUpdatedAt:        "w.updated_at",
	domain.SortByCreatedAt:        "w.created_at",
	domain.SortByName:             "w.name",
}

// WorkspaceOrderBy returns the ORDER BY clause (without the keywords) for
// normalized options. Columns come from a whitelist, never from user input.
// NULLS LAST keeps workspaces without actions at the end in both directions;
// w.updated_at and w.id make the order total so pagination is stable.
func WorkspaceOrderBy(o domain.ListWorkspacesOptions) string {
	col, ok := sortColumns[o.Sort]
	if !ok {
		col = sortColumns[domain.SortByActionsUpdatedAt]
	}
	dir := "DESC"
	if o.Order == domain.SortAsc {
		dir = "ASC"
	}
	return fmt.Sprintf("%s %s NULLS LAST, w.updated_at DESC, w.id ASC", col, dir)
}

// WorkspaceWhere returns the WHERE condition (without the keyword) for the
// list filters, using "?" placeholders, and its arguments.
func WorkspaceWhere(f domain.QueryParams) (string, []any) {
	conds := []string{"w.deleted_at IS NULL"}
	var args []any
	if f.Name != "" {
		conds = append(conds, `w.name ILIKE ? ESCAPE '\'`)
		args = append(args, "%"+EscapeLike(f.Name)+"%")
	}
	if f.Status != "" {
		conds = append(conds, "w.status = ?")
		args = append(args, string(f.Status))
	}
	if f.Namespace != "" {
		conds = append(conds, "w.namespace = ?")
		args = append(args, f.Namespace)
	}
	if f.ActionType != "" {
		conds = append(conds, `EXISTS (
	SELECT 1 FROM actions fa
	WHERE fa.workspace_id = w.id AND fa.deleted_at IS NULL AND fa.type = ?
)`)
		args = append(args, string(f.ActionType))
	}
	return strings.Join(conds, " AND "), args
}

// EscapeLike escapes the LIKE wildcards so user input is matched literally.
func EscapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Dollarize rewrites "?" placeholders into PostgreSQL "$n" placeholders,
// starting at $start. The queries built here never contain a literal "?".
func Dollarize(sql string, start int) string {
	var b strings.Builder
	n := start
	for _, r := range sql {
		if r == '?' {
			fmt.Fprintf(&b, "$%d", n)
			n++
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
