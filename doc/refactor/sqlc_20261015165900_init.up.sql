-- Structure des 4 tables existantes, telle que sqlc doit la connaître.
-- Si ces tables sont déjà créées par tes propres migrations, ne rajoute pas
-- ce fichier : pointe simplement "schema" de sqlc.yaml vers ton dossier de migrations.
-- Dans le doute, compare avec : pg_dump --schema-only -t namespaces -t workspaces -t actions -t variables

-- gen_random_uuid() est natif depuis PostgreSQL 13.

CREATE TABLE namespaces (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL,
    status     text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

-- Unicité limitée aux lignes vivantes : un nom supprimé peut être réutilisé.
CREATE UNIQUE INDEX namespaces_name_key ON namespaces (name) WHERE deleted_at IS NULL;

CREATE TABLE workspaces (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name          text        NOT NULL,
    vcs_url       text        NOT NULL,
    vcs_key_str   text        NOT NULL DEFAULT '',
    vcs_branch    text        NOT NULL DEFAULT '',
    vcs_directory text        NOT NULL DEFAULT '',
    status        text        NOT NULL,
    env_vars      jsonb       NOT NULL DEFAULT '[]'::jsonb,
    namespace     text        NOT NULL, -- nom du namespace (pas de FK : l'unicité de namespaces.name est partielle)
    tofu_version  text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz
);

CREATE UNIQUE INDEX workspaces_namespace_name_key
    ON workspaces (namespace, name) WHERE deleted_at IS NULL;

CREATE TABLE actions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    type         text        NOT NULL,
    status       text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz
);

-- Index sur la FK (utile au ON DELETE CASCADE).
CREATE INDEX actions_workspace_id_idx ON actions (workspace_id);

CREATE TABLE variables (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL,
    value        text        NOT NULL,
    secure       boolean     NOT NULL DEFAULT false,
    hidden       boolean     NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz
);

CREATE INDEX variables_workspace_id_idx ON variables (workspace_id);
CREATE UNIQUE INDEX variables_workspace_name_key
    ON variables (workspace_id, name) WHERE deleted_at IS NULL;
