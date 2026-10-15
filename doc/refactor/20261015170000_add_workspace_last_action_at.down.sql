DROP TRIGGER IF EXISTS actions_sync_last_action_at ON actions;
DROP FUNCTION IF EXISTS sync_workspace_last_action_at();
ALTER TABLE workspaces DROP COLUMN IF EXISTS last_action_at;
