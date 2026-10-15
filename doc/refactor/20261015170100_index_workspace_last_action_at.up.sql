-- CREATE INDEX CONCURRENTLY ne peut pas tourner dans une transaction :
-- garde ce fichier séparé (et désactive la transaction si ton outil
-- de migration l'impose, ex. "-- +goose NO TRANSACTION" avec goose).

-- Tri + pagination de ListWorkspaces.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workspaces_last_action_at
    ON workspaces (last_action_at DESC NULLS LAST, id);

-- Filtre EXISTS sur le type d'action.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_actions_workspace_type
    ON actions (workspace_id, type);

-- Recalcul du max(updated_at) dans le trigger lors d'un delete.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_actions_workspace_updated_at
    ON actions (workspace_id, updated_at DESC);
