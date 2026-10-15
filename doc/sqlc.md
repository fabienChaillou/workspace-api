## doc to use sqlc cli

Je prépare le schéma des 4 tables, la config `sqlc.yaml` et les requêtes, puis je vérifie que `sqlc generate` passe.

Le schéma passe, mais l'override `timestamptz` n'est pas pris (sqlc génère `pgtype.Timestamptz`) et `vcs_url` devient `VcsUrl`. Je corrige les deux.

Les types sont bons. Je vérifie maintenant que le code généré compile, avec un petit wrapper de pagination.

Le projet sqlc est prêt. `sqlc generate` (v1.30.0) passe sur le schéma et toutes les requêtes. Je n'ai pas pu compiler le code Go ici, car mon environnement n'a pas accès aux dépendances (pgx, uuid). Lance un `go build ./...` chez toi.

## Ce que contient `sqlc-project.zip`

- **`sqlc.yaml`** :
  - génère en pgx/v5 dans `internal/db` ;
  - `uuid.UUID` pour les UUID, `time.Time` / `*time.Time` pour les dates ;
  - le champ `VcsURL` est nommé correctement ;
  - les slices vides sont renvoyées comme `[]` ;
  - une interface `Querier` est générée pour les mocks.
- **`db/migrations/`** :
  - le schéma des 4 tables ;
  - la migration `last_action_at` ;
  - les index.
- **`db/queries/`** : les requêtes de chaque table (liste, get, create, update, soft delete), dont `ListWorkspaces` et `CountWorkspaces` avec leurs filtres optionnels.
- **`internal/store/workspace_store.go`** : un petit wrapper autour du code généré, qui gère la normalisation de la page, l'échappement de la recherche et le total quand la page est vide.
- **`internal/db/`** : le code déjà généré, pour que tu voies le résultat sans lancer sqlc.

## Choix à vérifier par rapport à ta base réelle

J'ai reconstruit le schéma depuis tes structs. Compare-le avec ta base, par exemple avec `pg_dump --schema-only`.

- **Si ces tables viennent déjà de tes migrations**, ne garde pas `20261015165900_init.up.sql` : pointe `schema` de `sqlc.yaml` vers ton propre dossier de migrations.
- **`variables`** : ta struct n'a pas d'`ID`, j'ai ajouté un `id uuid` en clé primaire. L'unicité est sur `(workspace_id, name)`, limitée aux variables non supprimées.
- **Unicité partielle** (`WHERE deleted_at IS NULL`) aussi sur `namespaces.name` et sur `(namespace, name)` des workspaces : un nom supprimé peut être réutilisé.
- **Pas de clé étrangère sur `workspaces.namespace`** : Postgres refuse une FK vers une unicité partielle. Si tu veux une vraie FK, il faut passer à une colonne `namespace_id`.
- **`DeletedAt`** : dans tes structs c'est un `time.Time`, mais une valeur NULL signifie « non supprimé ». sqlc génère donc `*time.Time`, et c'est ce que je te conseille de garder.

## Changement sur la migration `last_action_at`

Je l'ai mise à jour pour le soft delete, puisque tes tables ont un `deleted_at` :

- seules les actions non supprimées comptent pour `last_action_at` ;
- un soft delete ou une restauration d'action déclenche un recalcul complet du workspace ;
- les index sont partiels (`WHERE deleted_at IS NULL`).

## Tes types métier

`status`, `type` et `env_vars` sortent pour l'instant en `string` et `[]byte`. Pour obtenir directement `WorkspaceStatus`, `ActionType` et `[]EnvVars`, décommente les overrides en bas de `sqlc.yaml` et remplace le chemin d'import par celui de ton package domaine.
