-- Single source of truth for the schema, applied by both the pgx and the gorm
-- versions, so either binary can run against the same database.
-- Idempotent; the advisory lock serializes concurrent starts (the whole
-- script runs as one implicit transaction).
SELECT pg_advisory_xact_lock(7461001);

CREATE TABLE IF NOT EXISTS workspaces (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name          text        NOT NULL,
    vcs_url       text        NOT NULL DEFAULT '',
    vcs_key_str   text        NOT NULL DEFAULT '',
    vcs_branch    text        NOT NULL DEFAULT '',
    vcs_directory text        NOT NULL DEFAULT '',
    status        text        NOT NULL DEFAULT 'ACTIVE'
                  CONSTRAINT chk_workspaces_status CHECK (status IN ('ACTIVE', 'FAILURE', 'INACTIVE', 'DELETED')),
    env_vars      jsonb       NOT NULL DEFAULT '[]'::jsonb
                  CONSTRAINT chk_workspaces_env_vars CHECK (jsonb_typeof(env_vars) = 'array'),
    namespace     text        NOT NULL DEFAULT '',
    tofu_version  text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz
);

CREATE INDEX IF NOT EXISTS idx_workspaces_deleted_at ON workspaces (deleted_at);
CREATE INDEX IF NOT EXISTS idx_workspaces_namespace ON workspaces (namespace) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_workspaces_status ON workspaces (status) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS actions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid        NOT NULL
                 CONSTRAINT fk_workspaces_actions REFERENCES workspaces (id) ON DELETE CASCADE,
    type         text        NOT NULL
                 CONSTRAINT chk_actions_type CHECK (type IN ('init', 'plan', 'apply', 'destroy')),
    status       text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz
);

CREATE INDEX IF NOT EXISTS idx_actions_deleted_at ON actions (deleted_at);
-- Serves the default list ordering (latest action per workspace) and the
-- actionType filter.
CREATE INDEX IF NOT EXISTS idx_actions_workspace_updated ON actions (workspace_id, updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_actions_workspace_type ON actions (workspace_id, type) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS variables (
    workspace_id uuid        NOT NULL
                 CONSTRAINT fk_workspaces_variables REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    status       text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz
);

-- Name is the unique key of a variable. The index is partial so that a name
-- can be reused once the previous variable has been (soft) deleted.
CREATE UNIQUE INDEX IF NOT EXISTS uq_variables_name ON variables (name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_variables_workspace_id ON variables (workspace_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_variables_deleted_at ON variables (deleted_at);
