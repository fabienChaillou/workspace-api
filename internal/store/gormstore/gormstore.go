// Package gormstore implements store.Store with gorm and its postgres driver.
package gormstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/example/workspace-api/internal/domain"
	"github.com/example/workspace-api/internal/migrations"
	"github.com/example/workspace-api/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// Store is the gorm implementation of store.Store.
type Store struct {
	db *gorm.DB
}

var _ store.Store = (*Store)(nil)

// Option customizes the gorm configuration.
type Option func(*gorm.Config)

// WithLogger sets the gorm logger (silent by default).
func WithLogger(l logger.Interface) Option { return func(c *gorm.Config) { c.Logger = l } }

// New opens the database and checks connectivity.
func New(ctx context.Context, dsn string, opts ...Option) (*Store, error) {
	cfg := &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true, // unique / FK / check violations -> gorm.Err*
		// PostgreSQL stores microseconds: truncate so values returned on
		// create are identical to the ones read back later.
		NowFunc: func() time.Time { return time.Now().UTC().Truncate(time.Microsecond) },
	}
	for _, o := range opts {
		o(cfg)
	}
	db, err := gorm.Open(postgres.Open(dsn), cfg)
	if err != nil {
		return nil, fmt.Errorf("gorm: open: %w", err)
	}
	s := &Store{db: db}
	if err := s.Ping(ctx); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("gorm: ping: %w", err)
	}
	return s, nil
}

// Migrate applies the shared SQL schema. AutoMigrate is deliberately not used
// so the pgx and gorm versions share exactly the same schema.
func (s *Store) Migrate(ctx context.Context) error {
	if err := s.db.WithContext(ctx).Exec(migrations.Schema).Error; err != nil {
		return fmt.Errorf("gorm: migrate: %w", err)
	}
	return nil
}

func (s *Store) Ping(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// ---------------------------------------------------------------- workspaces

func (s *Store) CreateWorkspace(ctx context.Context, in domain.Workspace) (domain.Workspace, error) {
	env, err := domain.EncodeEnvVars(in.EnvVars)
	if err != nil {
		return domain.Workspace{}, err
	}
	if in.Status == "" {
		in.Status = domain.WorkspaceStatusActive
	}
	m := Workspace{
		ID: in.ID, Name: in.Name, VcsUrl: in.VcsURL, VcsKeyStr: in.VcsKeyStr, VcsBranch: in.VcsBranch,
		VcsDirectory: in.VcsDirectory, Status: string(in.Status), EnvVars: env,
		Namespace: in.Namespace, TofuVersion: in.TofuVersion,
	}
	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		return domain.Workspace{}, mapErr(err, "workspace")
	}
	return toDomainWorkspace(m)
}

func (s *Store) GetWorkspace(ctx context.Context, id uuid.UUID) (domain.Workspace, error) {
	var m Workspace
	err := s.db.WithContext(ctx).
		Preload("Actions", func(db *gorm.DB) *gorm.DB { return db.Order("updated_at DESC, id") }).
		Preload("Variables", func(db *gorm.DB) *gorm.DB { return db.Order("name") }).
		Take(&m, "id = ?", id).Error
	if err != nil {
		return domain.Workspace{}, mapErr(err, "workspace "+id.String())
	}
	w, err := toDomainWorkspace(m)
	if err != nil {
		return domain.Workspace{}, err
	}
	if len(w.Actions) > 0 {
		last := w.Actions[0].UpdatedAt // actions are ordered by updated_at DESC
		w.LastActionAt = &last
	}
	return w, nil
}

// workspaceRow is a workspace plus the computed latest action timestamp.
type workspaceRow struct {
	Workspace
	LastActionAt *time.Time
}

func (s *Store) ListWorkspaces(ctx context.Context, opts domain.ListWorkspacesOptions) (domain.WorkspacePage, error) {
	where, args := store.WorkspaceWhere(opts.Filter)
	db := s.db.WithContext(ctx)

	var total int64
	if err := db.Table("workspaces AS w").Where(where, args...).Count(&total).Error; err != nil {
		return domain.WorkspacePage{}, fmt.Errorf("count workspaces: %w", err)
	}
	page := domain.WorkspacePage{Items: []domain.Workspace{}, Pagination: opts.Pagination.WithTotal(total)}
	if total == 0 || opts.Pagination.Offset() >= int(total) {
		return page, nil
	}

	var rows []workspaceRow
	err := db.Table("workspaces AS w").
		Select("w.*, la.last_action_at").
		Joins(store.LastActionJoin).
		Where(where, args...).
		Order(store.WorkspaceOrderBy(opts)).
		Limit(opts.Pagination.Limit).
		Offset(opts.Pagination.Offset()).
		Scan(&rows).Error
	if err != nil {
		return domain.WorkspacePage{}, fmt.Errorf("list workspaces: %w", err)
	}
	for _, r := range rows {
		w, err := toDomainWorkspace(r.Workspace)
		if err != nil {
			return domain.WorkspacePage{}, err
		}
		w.Actions, w.Variables = nil, nil
		w.LastActionAt = r.LastActionAt
		page.Items = append(page.Items, w)
	}
	return page, nil
}

func (s *Store) UpdateWorkspace(ctx context.Context, id uuid.UUID, p domain.WorkspacePatch) (domain.Workspace, error) {
	updates := map[string]any{"updated_at": s.db.NowFunc()}
	setIf := func(col string, v *string) {
		if v != nil {
			updates[col] = *v
		}
	}
	setIf("name", p.Name)
	setIf("vcs_url", p.VcsURL)
	setIf("vcs_key_str", p.VcsKeyStr)
	setIf("vcs_branch", p.VcsBranch)
	setIf("vcs_directory", p.VcsDirectory)
	setIf("namespace", p.Namespace)
	setIf("tofu_version", p.TofuVersion)
	if p.Status != nil {
		updates["status"] = string(*p.Status)
	}
	if p.EnvVars != nil {
		env, err := domain.EncodeEnvVars(*p.EnvVars)
		if err != nil {
			return domain.Workspace{}, err
		}
		updates["env_vars"] = env
	}

	res := s.db.WithContext(ctx).Model(&Workspace{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return domain.Workspace{}, mapErr(res.Error, "workspace "+id.String())
	}
	if res.RowsAffected == 0 {
		return domain.Workspace{}, fmt.Errorf("workspace %s: %w", id, domain.ErrNotFound)
	}
	return s.GetWorkspace(ctx, id)
}

func (s *Store) DeleteWorkspace(ctx context.Context, id uuid.UUID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ?", id).Delete(&Workspace{}) // soft delete
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("workspace %s: %w", id, domain.ErrNotFound)
		}
		if err := tx.Where("workspace_id = ?", id).Delete(&Action{}).Error; err != nil {
			return err
		}
		return tx.Where("workspace_id = ?", id).Delete(&Variable{}).Error
	})
}

// requireWorkspace returns ErrNotFound unless the workspace exists and is live.
// FOR SHARE blocks a concurrent DeleteWorkspace until the transaction ends.
func requireWorkspace(tx *gorm.DB, id uuid.UUID) error {
	var w Workspace
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Take(&w, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("workspace %s: %w", id, domain.ErrNotFound)
	}
	return err
}

// workspaceExists is the read-only variant used by list endpoints.
func workspaceExists(db *gorm.DB, id uuid.UUID) error {
	var n int64
	if err := db.Model(&Workspace{}).Where("id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("workspace %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// ------------------------------------------------------------------- actions

func (s *Store) CreateAction(ctx context.Context, in domain.Action) (domain.Action, error) {
	m := Action{ID: in.ID, WorkspaceID: in.WorkspaceID, Type: string(in.Type), Status: in.Status}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireWorkspace(tx, in.WorkspaceID); err != nil {
			return err
		}
		return tx.Create(&m).Error
	})
	if err != nil {
		return domain.Action{}, mapErr(err, "action")
	}
	return toDomainAction(m), nil
}

func (s *Store) GetAction(ctx context.Context, workspaceID, id uuid.UUID) (domain.Action, error) {
	var m Action
	if err := s.db.WithContext(ctx).Take(&m, "id = ? AND workspace_id = ?", id, workspaceID).Error; err != nil {
		return domain.Action{}, mapErr(err, "action "+id.String())
	}
	return toDomainAction(m), nil
}

func (s *Store) ListActions(ctx context.Context, workspaceID uuid.UUID) ([]domain.Action, error) {
	db := s.db.WithContext(ctx)
	if err := workspaceExists(db, workspaceID); err != nil {
		return nil, err
	}
	var ms []Action
	if err := db.Where("workspace_id = ?", workspaceID).Order("updated_at DESC, id").Find(&ms).Error; err != nil {
		return nil, fmt.Errorf("list actions: %w", err)
	}
	out := make([]domain.Action, 0, len(ms))
	for _, m := range ms {
		out = append(out, toDomainAction(m))
	}
	return out, nil
}

func (s *Store) UpdateAction(ctx context.Context, workspaceID, id uuid.UUID, p domain.ActionPatch) (domain.Action, error) {
	updates := map[string]any{"updated_at": s.db.NowFunc()}
	if p.Type != nil {
		updates["type"] = string(*p.Type)
	}
	if p.Status != nil {
		updates["status"] = *p.Status
	}
	var m Action
	res := s.db.WithContext(ctx).Model(&m).
		Clauses(clause.Returning{}).
		Where("id = ? AND workspace_id = ?", id, workspaceID).
		Updates(updates)
	if res.Error != nil {
		return domain.Action{}, mapErr(res.Error, "action "+id.String())
	}
	if res.RowsAffected == 0 {
		return domain.Action{}, fmt.Errorf("action %s: %w", id, domain.ErrNotFound)
	}
	return toDomainAction(m), nil
}

func (s *Store) DeleteAction(ctx context.Context, workspaceID, id uuid.UUID) error {
	res := s.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID).Delete(&Action{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("action %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// ----------------------------------------------------------------- variables

func (s *Store) CreateVariable(ctx context.Context, in domain.Variable) (domain.Variable, error) {
	m := Variable{WorkspaceID: in.WorkspaceID, Name: in.Name, Status: in.Status}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireWorkspace(tx, in.WorkspaceID); err != nil {
			return err
		}
		return tx.Create(&m).Error
	})
	if err != nil {
		return domain.Variable{}, mapErr(err, "variable "+in.Name)
	}
	return toDomainVariable(m), nil
}

func (s *Store) GetVariable(ctx context.Context, workspaceID uuid.UUID, name string) (domain.Variable, error) {
	var m Variable
	if err := s.db.WithContext(ctx).Take(&m, "workspace_id = ? AND name = ?", workspaceID, name).Error; err != nil {
		return domain.Variable{}, mapErr(err, "variable "+name)
	}
	return toDomainVariable(m), nil
}

func (s *Store) ListVariables(ctx context.Context, workspaceID uuid.UUID) ([]domain.Variable, error) {
	db := s.db.WithContext(ctx)
	if err := workspaceExists(db, workspaceID); err != nil {
		return nil, err
	}
	var ms []Variable
	if err := db.Where("workspace_id = ?", workspaceID).Order("name").Find(&ms).Error; err != nil {
		return nil, fmt.Errorf("list variables: %w", err)
	}
	out := make([]domain.Variable, 0, len(ms))
	for _, m := range ms {
		out = append(out, toDomainVariable(m))
	}
	return out, nil
}

func (s *Store) UpdateVariable(ctx context.Context, workspaceID uuid.UUID, name string, p domain.VariablePatch) (domain.Variable, error) {
	updates := map[string]any{"updated_at": s.db.NowFunc()}
	if p.Status != nil {
		updates["status"] = *p.Status
	}
	var m Variable
	res := s.db.WithContext(ctx).Model(&m).
		Clauses(clause.Returning{}).
		Where("workspace_id = ? AND name = ?", workspaceID, name).
		Updates(updates)
	if res.Error != nil {
		return domain.Variable{}, mapErr(res.Error, "variable "+name)
	}
	if res.RowsAffected == 0 {
		return domain.Variable{}, fmt.Errorf("variable %s: %w", name, domain.ErrNotFound)
	}
	return toDomainVariable(m), nil
}

func (s *Store) DeleteVariable(ctx context.Context, workspaceID uuid.UUID, name string) error {
	res := s.db.WithContext(ctx).Where("workspace_id = ? AND name = ?", workspaceID, name).Delete(&Variable{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("variable %s: %w", name, domain.ErrNotFound)
	}
	return nil
}

// ------------------------------------------------------------------- mapping

func toDomainWorkspace(m Workspace) (domain.Workspace, error) {
	env, err := domain.DecodeEnvVars(m.EnvVars)
	if err != nil {
		return domain.Workspace{}, err
	}
	w := domain.Workspace{
		ID: m.ID, Name: m.Name, VcsURL: m.VcsUrl, VcsKeyStr: m.VcsKeyStr, VcsBranch: m.VcsBranch,
		VcsDirectory: m.VcsDirectory, Status: domain.WorkspaceStatus(m.Status), EnvVars: env,
		Namespace: m.Namespace, TofuVersion: m.TofuVersion, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		Actions: make([]domain.Action, 0, len(m.Actions)), Variables: make([]domain.Variable, 0, len(m.Variables)),
	}
	for _, a := range m.Actions {
		w.Actions = append(w.Actions, toDomainAction(a))
	}
	for _, v := range m.Variables {
		w.Variables = append(w.Variables, toDomainVariable(v))
	}
	return w, nil
}

func toDomainAction(m Action) domain.Action {
	return domain.Action{
		ID: m.ID, WorkspaceID: m.WorkspaceID, Type: domain.ActionType(m.Type), Status: m.Status,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func toDomainVariable(m Variable) domain.Variable {
	return domain.Variable{
		WorkspaceID: m.WorkspaceID, Name: m.Name, Status: m.Status, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

// mapErr translates gorm/driver errors into domain errors.
func mapErr(err error, what string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrInvalid):
		return err
	case errors.Is(err, gorm.ErrRecordNotFound):
		return fmt.Errorf("%s: %w", what, domain.ErrNotFound)
	case errors.Is(err, gorm.ErrDuplicatedKey):
		return fmt.Errorf("%s already exists: %w", what, domain.ErrConflict)
	case errors.Is(err, gorm.ErrForeignKeyViolated):
		return fmt.Errorf("%s: parent workspace: %w", what, domain.ErrNotFound)
	case errors.Is(err, gorm.ErrCheckConstraintViolated):
		return fmt.Errorf("%s: %v: %w", what, err, domain.ErrInvalid)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
		return fmt.Errorf("%s: %s: %w", what, pgErr.Message, domain.ErrInvalid)
	}
	return fmt.Errorf("%s: %w", what, err)
}
