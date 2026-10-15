// Package memstore is an in-memory store.Store used by the API unit tests.
// It honours the same contract as the SQL stores (see storetest).
package memstore

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/workspace-api/internal/domain"
	"github.com/example/workspace-api/internal/store"
	"github.com/google/uuid"
)

type actionRow struct {
	domain.Action
	deleted bool
}

type variableRow struct {
	domain.Variable
	deleted bool
}

type workspaceRow struct {
	domain.Workspace
	deleted bool
}

// Store is a thread-safe in-memory store.
type Store struct {
	mu         sync.RWMutex
	workspaces map[uuid.UUID]*workspaceRow
	actions    []*actionRow
	variables  []*variableRow
	lastNow    time.Time
}

var _ store.Store = (*Store)(nil)

// New returns an empty store.
func New() *Store {
	return &Store{workspaces: map[uuid.UUID]*workspaceRow{}}
}

// now returns a strictly increasing timestamp (microsecond precision, like
// PostgreSQL) so orderings are deterministic. Caller must hold the lock.
func (s *Store) now() time.Time {
	t := time.Now().UTC().Truncate(time.Microsecond)
	if !t.After(s.lastNow) {
		t = s.lastNow.Add(time.Microsecond)
	}
	s.lastNow = t
	return t
}

func (s *Store) Ping(context.Context) error { return nil }
func (s *Store) Close() error               { return nil }

func (s *Store) liveWorkspace(id uuid.UUID) (*workspaceRow, error) {
	w, ok := s.workspaces[id]
	if !ok || w.deleted {
		return nil, fmt.Errorf("workspace %s: %w", id, domain.ErrNotFound)
	}
	return w, nil
}

// ---------------------------------------------------------------- workspaces

func (s *Store) CreateWorkspace(_ context.Context, in domain.Workspace) (domain.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	if _, exists := s.workspaces[in.ID]; exists {
		return domain.Workspace{}, fmt.Errorf("workspace %s already exists: %w", in.ID, domain.ErrConflict)
	}
	if in.Status == "" {
		in.Status = domain.WorkspaceStatusActive
	}
	if !in.Status.Valid() {
		return domain.Workspace{}, fmt.Errorf("workspace status %q: %w", in.Status, domain.ErrInvalid)
	}
	in.EnvVars = cloneEnv(in.EnvVars)
	in.CreatedAt = s.now()
	in.UpdatedAt = in.CreatedAt
	in.Actions, in.Variables, in.LastActionAt = nil, nil, nil
	s.workspaces[in.ID] = &workspaceRow{Workspace: in}
	out := in
	out.EnvVars = cloneEnv(in.EnvVars)
	out.Actions, out.Variables = []domain.Action{}, []domain.Variable{}
	return out, nil
}

func (s *Store) GetWorkspace(_ context.Context, id uuid.UUID) (domain.Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w, err := s.liveWorkspace(id)
	if err != nil {
		return domain.Workspace{}, err
	}
	out := s.view(w)
	out.Actions = s.actionsOf(id)
	out.Variables = s.variablesOf(id)
	return out, nil
}

// view copies a workspace and computes LastActionAt. Caller holds the lock.
func (s *Store) view(w *workspaceRow) domain.Workspace {
	out := w.Workspace
	out.EnvVars = cloneEnv(w.EnvVars)
	for _, a := range s.actions {
		if a.WorkspaceID == w.ID && !a.deleted {
			if out.LastActionAt == nil || a.UpdatedAt.After(*out.LastActionAt) {
				t := a.UpdatedAt
				out.LastActionAt = &t
			}
		}
	}
	return out
}

func (s *Store) ListWorkspaces(_ context.Context, opts domain.ListWorkspacesOptions) (domain.WorkspacePage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	f := opts.Filter
	var items []domain.Workspace
	for _, w := range s.workspaces {
		if w.deleted {
			continue
		}
		if f.Name != "" && !strings.Contains(strings.ToLower(w.Name), strings.ToLower(f.Name)) {
			continue
		}
		if f.Status != "" && w.Status != f.Status {
			continue
		}
		if f.Namespace != "" && w.Namespace != f.Namespace {
			continue
		}
		if f.ActionType != "" && !s.hasAction(w.ID, f.ActionType) {
			continue
		}
		items = append(items, s.view(w))
	}

	sort.SliceStable(items, func(i, j int) bool { return less(items[i], items[j], opts) })

	page := domain.WorkspacePage{Items: []domain.Workspace{}, Pagination: opts.Pagination.WithTotal(int64(len(items)))}
	start := opts.Pagination.Offset()
	if start >= len(items) {
		return page, nil
	}
	end := min(start+opts.Pagination.Limit, len(items))
	page.Items = append(page.Items, items[start:end]...)
	return page, nil
}

// less mirrors store.WorkspaceOrderBy:
// <col> <dir> NULLS LAST, updated_at DESC, id ASC.
func less(a, b domain.Workspace, o domain.ListWorkspacesOptions) bool {
	asc := o.Order == domain.SortAsc
	cmpTime := func(x, y time.Time) int { return x.Compare(y) }
	var c int
	switch o.Sort {
	case domain.SortByUpdatedAt:
		c = cmpTime(a.UpdatedAt, b.UpdatedAt)
	case domain.SortByCreatedAt:
		c = cmpTime(a.CreatedAt, b.CreatedAt)
	case domain.SortByName:
		c = strings.Compare(a.Name, b.Name)
	default: // actions_updated_at, NULLS LAST in both directions
		switch {
		case a.LastActionAt == nil && b.LastActionAt == nil:
			c = 0
		case a.LastActionAt == nil:
			return false
		case b.LastActionAt == nil:
			return true
		default:
			c = cmpTime(*a.LastActionAt, *b.LastActionAt)
		}
	}
	if c != 0 {
		if asc {
			return c < 0
		}
		return c > 0
	}
	if c := cmpTime(a.UpdatedAt, b.UpdatedAt); c != 0 {
		return c > 0
	}
	return a.ID.String() < b.ID.String()
}

func (s *Store) hasAction(workspaceID uuid.UUID, t domain.ActionType) bool {
	for _, a := range s.actions {
		if a.WorkspaceID == workspaceID && !a.deleted && a.Type == t {
			return true
		}
	}
	return false
}

func (s *Store) UpdateWorkspace(_ context.Context, id uuid.UUID, p domain.WorkspacePatch) (domain.Workspace, error) {
	s.mu.Lock()
	w, err := s.liveWorkspace(id)
	if err != nil {
		s.mu.Unlock()
		return domain.Workspace{}, err
	}
	if p.Status != nil && !p.Status.Valid() {
		s.mu.Unlock()
		return domain.Workspace{}, fmt.Errorf("workspace status %q: %w", *p.Status, domain.ErrInvalid)
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&w.Name, p.Name)
	set(&w.VcsURL, p.VcsURL)
	set(&w.VcsKeyStr, p.VcsKeyStr)
	set(&w.VcsBranch, p.VcsBranch)
	set(&w.VcsDirectory, p.VcsDirectory)
	set(&w.Namespace, p.Namespace)
	set(&w.TofuVersion, p.TofuVersion)
	if p.Status != nil {
		w.Status = *p.Status
	}
	if p.EnvVars != nil {
		w.EnvVars = cloneEnv(*p.EnvVars)
	}
	w.UpdatedAt = s.now()
	s.mu.Unlock()
	return s.GetWorkspace(context.Background(), id)
}

func (s *Store) DeleteWorkspace(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := s.liveWorkspace(id)
	if err != nil {
		return err
	}
	w.deleted = true
	for _, a := range s.actions {
		if a.WorkspaceID == id {
			a.deleted = true
		}
	}
	for _, v := range s.variables {
		if v.WorkspaceID == id {
			v.deleted = true
		}
	}
	return nil
}

// ------------------------------------------------------------------- actions

func (s *Store) CreateAction(_ context.Context, in domain.Action) (domain.Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.liveWorkspace(in.WorkspaceID); err != nil {
		return domain.Action{}, err
	}
	if !in.Type.Valid() {
		return domain.Action{}, fmt.Errorf("action type %q: %w", in.Type, domain.ErrInvalid)
	}
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	in.CreatedAt = s.now()
	in.UpdatedAt = in.CreatedAt
	s.actions = append(s.actions, &actionRow{Action: in})
	return in, nil
}

func (s *Store) findAction(workspaceID, id uuid.UUID) (*actionRow, error) {
	for _, a := range s.actions {
		if a.ID == id && a.WorkspaceID == workspaceID && !a.deleted {
			return a, nil
		}
	}
	return nil, fmt.Errorf("action %s: %w", id, domain.ErrNotFound)
}

func (s *Store) GetAction(_ context.Context, workspaceID, id uuid.UUID) (domain.Action, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, err := s.findAction(workspaceID, id)
	if err != nil {
		return domain.Action{}, err
	}
	return a.Action, nil
}

func (s *Store) ListActions(_ context.Context, workspaceID uuid.UUID) ([]domain.Action, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.liveWorkspace(workspaceID); err != nil {
		return nil, err
	}
	return s.actionsOf(workspaceID), nil
}

func (s *Store) actionsOf(workspaceID uuid.UUID) []domain.Action {
	out := []domain.Action{}
	for _, a := range s.actions {
		if a.WorkspaceID == workspaceID && !a.deleted {
			out = append(out, a.Action)
		}
	}
	slices.SortStableFunc(out, func(x, y domain.Action) int {
		if c := y.UpdatedAt.Compare(x.UpdatedAt); c != 0 {
			return c
		}
		return strings.Compare(x.ID.String(), y.ID.String())
	})
	return out
}

func (s *Store) UpdateAction(_ context.Context, workspaceID, id uuid.UUID, p domain.ActionPatch) (domain.Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.findAction(workspaceID, id)
	if err != nil {
		return domain.Action{}, err
	}
	if p.Type != nil {
		if !p.Type.Valid() {
			return domain.Action{}, fmt.Errorf("action type %q: %w", *p.Type, domain.ErrInvalid)
		}
		a.Type = *p.Type
	}
	if p.Status != nil {
		a.Status = *p.Status
	}
	a.UpdatedAt = s.now()
	return a.Action, nil
}

func (s *Store) DeleteAction(_ context.Context, workspaceID, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.findAction(workspaceID, id)
	if err != nil {
		return err
	}
	a.deleted = true
	return nil
}

// ----------------------------------------------------------------- variables

func (s *Store) CreateVariable(_ context.Context, in domain.Variable) (domain.Variable, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.liveWorkspace(in.WorkspaceID); err != nil {
		return domain.Variable{}, err
	}
	for _, v := range s.variables {
		if v.Name == in.Name && !v.deleted {
			return domain.Variable{}, fmt.Errorf("variable %s already exists: %w", in.Name, domain.ErrConflict)
		}
	}
	in.CreatedAt = s.now()
	in.UpdatedAt = in.CreatedAt
	s.variables = append(s.variables, &variableRow{Variable: in})
	return in, nil
}

func (s *Store) findVariable(workspaceID uuid.UUID, name string) (*variableRow, error) {
	for _, v := range s.variables {
		if v.Name == name && v.WorkspaceID == workspaceID && !v.deleted {
			return v, nil
		}
	}
	return nil, fmt.Errorf("variable %s: %w", name, domain.ErrNotFound)
}

func (s *Store) GetVariable(_ context.Context, workspaceID uuid.UUID, name string) (domain.Variable, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, err := s.findVariable(workspaceID, name)
	if err != nil {
		return domain.Variable{}, err
	}
	return v.Variable, nil
}

func (s *Store) ListVariables(_ context.Context, workspaceID uuid.UUID) ([]domain.Variable, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.liveWorkspace(workspaceID); err != nil {
		return nil, err
	}
	return s.variablesOf(workspaceID), nil
}

func (s *Store) variablesOf(workspaceID uuid.UUID) []domain.Variable {
	out := []domain.Variable{}
	for _, v := range s.variables {
		if v.WorkspaceID == workspaceID && !v.deleted {
			out = append(out, v.Variable)
		}
	}
	slices.SortFunc(out, func(x, y domain.Variable) int { return strings.Compare(x.Name, y.Name) })
	return out
}

func (s *Store) UpdateVariable(_ context.Context, workspaceID uuid.UUID, name string, p domain.VariablePatch) (domain.Variable, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.findVariable(workspaceID, name)
	if err != nil {
		return domain.Variable{}, err
	}
	if p.Status != nil {
		v.Status = *p.Status
	}
	v.UpdatedAt = s.now()
	return v.Variable, nil
}

func (s *Store) DeleteVariable(_ context.Context, workspaceID uuid.UUID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.findVariable(workspaceID, name)
	if err != nil {
		return err
	}
	v.deleted = true
	return nil
}

func cloneEnv(in []domain.EnvVars) []domain.EnvVars {
	if in == nil {
		return []domain.EnvVars{}
	}
	return slices.Clone(in)
}
