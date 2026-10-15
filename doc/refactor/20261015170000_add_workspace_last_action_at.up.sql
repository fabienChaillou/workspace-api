-- 1. Colonne nullable sans valeur par défaut :
--    simple changement de métadonnées, instantané même sur une grosse table.
ALTER TABLE workspaces ADD COLUMN last_action_at timestamptz;

-- 2. Trigger qui maintient workspaces.last_action_at à jour.
--    Créé AVANT le backfill pour ne rater aucune écriture concurrente.
CREATE OR REPLACE FUNCTION sync_workspace_last_action_at() RETURNS trigger AS $$
BEGIN
    -- Insert / update : on avance la date seulement si elle est plus récente.
    -- Le "<" évite des écritures inutiles sur workspaces.
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        UPDATE workspaces
        SET last_action_at = NEW.updated_at
        WHERE id = NEW.workspace_id
          AND (last_action_at IS NULL OR last_action_at < NEW.updated_at);
    END IF;

    -- Delete (ou action déplacée vers un autre workspace) :
    -- l'ancien max a peut-être disparu, on recalcule.
    IF TG_OP = 'DELETE'
       OR (TG_OP = 'UPDATE' AND OLD.workspace_id <> NEW.workspace_id) THEN
        UPDATE workspaces
        SET last_action_at = (
            SELECT max(updated_at) FROM actions WHERE workspace_id = OLD.workspace_id
        )
        WHERE id = OLD.workspace_id;
    END IF;

    -- Si tu fais du soft delete (deleted_at) : ajoute "deleted_at" dans le
    -- UPDATE OF du trigger, traite "OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL"
    -- comme un DELETE, et filtre "deleted_at IS NULL" dans les sous-requêtes max().

    RETURN NULL; -- trigger AFTER : valeur de retour ignorée
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER actions_sync_last_action_at
AFTER INSERT OR UPDATE OF updated_at, workspace_id OR DELETE ON actions
FOR EACH ROW EXECUTE FUNCTION sync_workspace_last_action_at();

-- 3. Backfill des workspaces existants.
--    Le GREATEST implicite (condition "<") ne fait pas régresser une valeur
--    déjà écrite par le trigger.
UPDATE workspaces w
SET last_action_at = la.max_updated_at
FROM (
    SELECT workspace_id, max(updated_at) AS max_updated_at
    FROM actions
    GROUP BY workspace_id
) la
WHERE la.workspace_id = w.id
  AND (w.last_action_at IS NULL OR w.last_action_at < la.max_updated_at);
