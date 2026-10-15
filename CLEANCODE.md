## helper to cleancode

Le plus simple est **une seule requête SQL** : un CTE qui calcule le `max(updated_at)` des actions par workspace, une jointure, puis `count(*) OVER()` pour obtenir le total dans la même requête que la page.

## La requête

```sql
WITH last_action AS (
    SELECT workspace_id, max(updated_at) AS last_action_at
    FROM actions
    WHERE ($3::text IS NULL OR type = $3)
      -- AND deleted_at IS NULL   -- si "live" = non supprimées (soft delete)
    GROUP BY workspace_id
)
SELECT
    w.id, w.name, w.vcs_url, w.vcs_key_str, w.vcs_branch, w.vcs_directory,
    w.status, w.env_vars, w.namespace, w.tofu_version,
    la.last_action_at,
    w.created_at, w.updated_at,
    count(*) OVER() AS total
FROM workspaces w
LEFT JOIN last_action la ON la.workspace_id = w.id
WHERE ($1::text IS NULL OR w.name ILIKE '%' || $1 || '%')
  AND ($2::text IS NULL OR w.namespace = $2)
  AND ($3::text IS NULL OR la.workspace_id IS NOT NULL)   -- filtre sur le type d'action
ORDER BY la.last_action_at DESC NULLS LAST, w.id
LIMIT $4 OFFSET $5;
```

Quelques points sur ce choix :

- **Paramètres optionnels** : `$1::text IS NULL OR ...` évite de construire le SQL dynamiquement. On passe `nil` quand le query param est absent.
- **Filtre `type`** : il garde les workspaces qui ont au moins une action de ce type. Le tri se fait alors sur la dernière action *de ce type*. Si tu veux trier sur la dernière action tous types confondus, sors le filtre du CTE et ajoute un `EXISTS (SELECT 1 FROM actions a WHERE a.workspace_id = w.id AND a.type = $3)`.
- **`w.id` en second critère de tri** : sans lui, la pagination n'est pas stable quand deux workspaces ont la même date.
- **`LEFT JOIN` + `NULLS LAST`** : les workspaces sans action apparaissent en fin de liste. Ils sont exclus si un filtre `type` est fourni.
- **Limite de `count(*) OVER()`** : si on demande une page au-delà de la fin, aucune ligne ne revient, donc le total vaut 0. Si ça gêne côté front, fais une seconde requête `SELECT count(*)` avec le même `WHERE`.

## Le code Go (pgx v5)

```go
type ListWorkspacesFilter struct {
	Name       *string
	ActionType *string
	Namespace  *string
	Page       int // à partir de 1
	Limit      int
}

type Pagination struct {
	TotalWorkspaces int `json:"totalWorkspaces"`
	Page            int `json:"page"`
	Limit           int `json:"limit"`
}

func (r *Repo) ListWorkspaces(ctx context.Context, f ListWorkspacesFilter) ([]Workspace, Pagination, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 20
	}
	if f.Page < 1 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.Limit

	rows, err := r.pool.Query(ctx, listWorkspacesSQL,
		f.Name, f.Namespace, f.ActionType, f.Limit, offset)
	if err != nil {
		return nil, Pagination{}, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()

	var (
		workspaces []Workspace
		total      int
	)
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(
			&w.ID, &w.Name, &w.VcsURL, &w.VcsKeyStr, &w.VcsBranch, &w.VcsDirectory,
			&w.Status, &w.EnvVars, &w.Namespace, &w.TofuVersion,
			&w.LastActionAt,
			&w.CreatedAt, &w.UpdatedAt,
			&total,
		); err != nil {
			return nil, Pagination{}, fmt.Errorf("scan workspace: %w", err)
		}
		workspaces = append(workspaces, w)
	}
	if err := rows.Err(); err != nil {
		return nil, Pagination{}, err
	}

	return workspaces, Pagination{TotalWorkspaces: total, Page: f.Page, Limit: f.Limit}, nil
}
```

Remarques sur le scan :

- `LastActionAt *time.Time` reçoit directement le `NULL`.
- `EnvVars []EnvVars` se scanne tout seul si la colonne est en `jsonb`, à condition que la struct ait des tags `json`.
- pgx v5 scanne aussi les types nommés dont le type sous-jacent est `string`, comme `WorkspaceStatus`.

## Les index utiles

```sql
CREATE INDEX ON actions (workspace_id, updated_at DESC);
CREATE INDEX ON actions (type, workspace_id);
CREATE INDEX ON workspaces (namespace);
-- pour ILIKE '%...%' sur le nom :
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX ON workspaces USING gin (name gin_trgm_ops);
```

Si la table `actions` devient très grosse, il vaut mieux stocker `last_action_at` dans `workspaces`. Tu la mets à jour à chaque update d'action, dans la même transaction ou par trigger. Ton tri devient alors un simple index sur `workspaces`, sans agrégation.

## pgx ou GORM ?

**pgx**, sans hésiter, pour ce cas :

- **Performance** : pgx parle le protocole Postgres directement (binaire, prepared statements en cache, pool natif). GORM passe par `database/sql` et fait beaucoup de réflexion à chaque requête, avec nettement plus d'allocations.
- **Pas d'avantage de GORM ici** : pour un CTE avec agrégat, window function et `NULLS LAST`, tu finirais par écrire du `Raw()` dans GORM. Tu aurais le coût de l'ORM sans ses bénéfices.
- **Types Postgres** : `uuid`, `jsonb`, `timestamptz` et les tableaux sont mieux gérés nativement par pgx.

Si tu veux éviter d'écrire les `Scan` à la main, regarde **sqlc**. Il génère du code pgx typé à partir de tes fichiers `.sql`, ce qui garde les perfs de pgx avec le confort d'un code généré. C'est souvent le meilleur compromis.

---

Je prépare le fichier Go du repository et les migrations SQL.

## Les fonctions pgx (`workspace_repo.go`)

- **`ListWorkspaces`** : une seule requête qui renvoie la page et le total via `count(*) OVER()`. Elle trie directement sur `w.last_action_at`, il n'y a donc plus de CTE ni d'agrégat.
- **`CountWorkspaces`** : réutilise les mêmes filtres. Elle n'est appelée que si on demande une page au-delà de la fin, pour que le total ne retombe pas à 0.
- **Filtres** : un champ `nil` ou vide n'est pas appliqué.
  - `Name` fait une recherche partielle. Les `%` et `_` saisis par l'utilisateur sont échappés pour ne pas servir de jokers.
  - `ActionType` passe par un `EXISTS`.

**Changement de comportement par rapport à ma première version** : le tri se fait maintenant sur la dernière action **tous types confondus**, même quand on filtre par type. Si tu veux trier sur la dernière action du type filtré, il faut garder l'agrégat sur `actions` pour ce cas-là.

## Ajouter `last_action_at` : mon avis

Je te recommande un **trigger Postgres sur `actions`** plutôt qu'une mise à jour côté Go :

- **Pourquoi le trigger** : la colonne reste cohérente quel que soit le chemin d'écriture (repo, worker Temporal, script, correction SQL manuelle). Côté Go, il suffit d'oublier une seule fois de la mettre à jour pour qu'elle dérive en silence.
- **Coût en écriture** :
  - À l'insert ou l'update d'une action, on ne touche le workspace que si la date avance. C'est la condition `last_action_at < NEW.updated_at`.
  - Le recalcul complet du `max()` ne se fait qu'au delete.
- **Alternative côté Go** : mettre à jour `workspaces` dans la même transaction que l'écriture de l'action, avec la même condition. C'est plus explicite et plus facile à tester, mais seulement si **toutes** les écritures sur `actions` passent par ton repo.

## Ordre de la migration

1. **`ADD COLUMN` nullable sans défaut** : c'est un changement de métadonnées, instantané même sur une grosse table.
2. **Trigger créé avant le backfill** : aucune action écrite pendant la migration n'est perdue.
3. **Backfill** : il utilise la même condition « seulement si plus récent ». Il ne peut donc pas écraser une valeur déjà posée par le trigger.
4. **Index dans une migration séparée** : `CREATE INDEX CONCURRENTLY` ne peut pas tourner dans une transaction.

Trois points à vérifier chez toi :

- **Taille de la table** : la première migration tourne dans une transaction, et `ALTER TABLE` garde un verrou exclusif sur `workspaces` pendant tout le backfill. Si la table est grosse, sors le backfill dans une migration à part, ou fais-le par lots.
- **Soft delete** : ton commentaire parle d'actions « live ». Si tu as un `deleted_at`, adapte le trigger comme indiqué dans le commentaire du fichier SQL.
- **Trigger existant sur `workspaces.updated_at`** (type `moddatetime`) : il serait déclenché à chaque nouvelle action. Si tu ne veux pas ça, fais-lui ignorer les updates qui ne touchent que `last_action_at`.


---

Je modifie les deux points dans les trois stores (pgx, gorm, mémoire), puis je relance toute la suite de tests. Concrètement :
- `lastActionAt` est remplacé par un objet `lastAction` complet ;
- le create accepte `variables`, créées dans la même transaction que le workspace.

The changes start with the domain types and the shared SQL fragment.

Next, the pgx store: workspace creation with its variables in one transaction, and the last action read from the join.

The project's `go.mod` doesn't have the replaces this sandbox needs, so builds run in the verification copy. I'm adding a sync script for that.

Now the gorm store. I'm not using gorm's association save on purpose: it inserts with `ON CONFLICT DO NOTHING` and would silently swallow a duplicate variable name.

Now the in-memory store, which has to follow the same contract.

Now the contract suite: the existing assertions now check the full action, and there are new cases for creating a workspace with variables.

The local PostgreSQL went down with the environment again (nothing to do with the code). I'm restarting it.

All three stores pass the contract, new cases included. On to the API layer.

Now the API tests.

The end-to-end test and the fixtures test now check the full `lastAction`, including the soft-delete edge cases.

That last script ran from the wrong directory: the end-to-end and fixtures test edits weren't applied, so the green run above doesn't cover them. I'm re-applying them and rerunning.

Everything passes. Now a real check: both binaries against the fixtures.

Les deux changements sont faits et toute la suite passe, avec `-race`, sur pgx, gorm et le store mémoire. J'ai aussi lancé les deux binaires sur les fixtures : ils renvoient la même `lastAction`.

**1. `lastAction` dans la liste des workspaces**

`lastActionAt` est remplacé par l'action complète :

```json
"lastAction": {
  "id": "b0000000-…-000000000502", "workspaceId": "a0000000-…-000000000005",
  "type": "plan", "status": "running", "createdAt": "…", "updatedAt": "…"
}
```

- **Cohérence du tri** : c'est l'action avec le `updatedAt` le plus récent, donc celle qui sert déjà au tri par défaut. En cas d'égalité, le plus petit `id` l'emporte, comme dans `GET …/actions`.
- **Requête** : en SQL, c'est un `LEFT JOIN LATERAL … LIMIT 1` qui s'appuie sur l'index existant. Ça reste une seule requête par page, pas une par workspace.
- **Champ absent** : sans action, `lastAction` n'apparaît pas dans le JSON. Une action supprimée ne compte jamais. Dans les fixtures, `platform-dns` renvoie donc son `apply` et non le `destroy` supprimé.
- **Détail** : `GET /workspaces/{id}` renvoie aussi `lastAction`.

**2. Création d'un workspace avec des variables**

```json
POST /workspaces
{ "name": "network", "variables": [ { "name": "vpc_cidr", "status": "set" }, { "name": "az_count" } ] }
```

- **Création atomique** : le workspace et ses variables sont créés dans une seule transaction. Si un nom est déjà pris ailleurs, la réponse est **409** et elle nomme la variable fautive (`Key (name)=(vpc_cidr) already exists`). Dans ce cas, rien n'est créé, ni le workspace ni les autres variables.
- **Validation** : un nom en double dans la requête, ou un nom invalide, donne **422** avant tout accès à la base.

**Changements de comportement :**
- **Rupture de contrat** : un client qui lisait `lastActionAt` doit maintenant lire `lastAction.updatedAt`.
- **gorm** : les variables sont insérées explicitement, sans passer par l'enregistrement automatique des associations. Avec ce mécanisme, gorm insère en `ON CONFLICT DO NOTHING` et aurait ignoré un doublon sans erreur. Le nouveau test de contrat vérifie justement ce cas.
