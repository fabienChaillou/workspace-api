# Workspace API

API REST en Go ([Huma v2](https://huma.rocks)) pour gérer des **Workspaces**, leurs **Actions** et leurs **Variables**, stockés dans PostgreSQL.
Deux versions partagent exactement la même API, le même schéma et la même suite de tests :

| Binaire | Accès base | Code |
|---|---|---|
| `api-pgx` | driver `pgx/v5` + SQL écrit à la main | `internal/store/pgxstore` |
| `api-gorm` | `gorm` + driver postgres | `internal/store/gormstore` |

Les deux peuvent tourner en même temps sur la même base.

## Démarrage

```bash
make deps        # go mod tidy (complète go.sum)
make db-up       # PostgreSQL 16 dans Docker
make run-pgx     # ou : make run-gorm
open http://localhost:8080/docs
```

Connexion par défaut (surchargeable : `make db-up PG_PORT=5433`, ou variable `DATABASE_URL`) :

```
postgres://postgres:postgres@localhost:5432/workspace-api?sslmode=disable
```

Le schéma (`internal/migrations/schema.sql`) est appliqué au démarrage, il est idempotent.

## Fixtures

```bash
make db-seed     # charge les fixtures (idempotent, garde les données existantes)
make db-reseed   # vide TOUTES les tables puis recharge, avec des dates fraîches
```

`internal/fixtures/fixtures.sql` contient 11 workspaces actifs, plus un supprimé (`old-vpn`), 20 actions et 14 variables. Il est commun aux deux versions. Les UUID sont fixes (constantes dans `internal/fixtures/fixtures.go`) et les dates sont relatives à `now()`.

Ordre par défaut de `GET /workspaces` juste après `make db-reseed` :

| # | Workspace | Namespace | Status | Dernière action |
|---|---|---|---|---|
| 1 | payments-db | team-payments | ACTIVE | plan `running`, il y a 5 min |
| 2 | platform-k8s-cluster | platform | FAILURE | apply `failed`, 10 min |
| 3 | data-airflow | team-data | ACTIVE | plan `queued`, 30 min |
| 4 | platform-network | platform | ACTIVE | apply, 1h45 |
| 5 | payments-api | team-payments | ACTIVE | apply, 23 h |
| 6 | data-warehouse | team-data | ACTIVE | apply, 2 j |
| 7 | platform-dns | platform | ACTIVE | apply, 3 j |
| 8 | sandbox-alice | sandbox | INACTIVE | destroy, 6 j |
| 9 | legacy-monitoring | ops | DELETED | destroy, 45 j |
| 10 | data-lake-new | team-data | ACTIVE | aucune |
| 11 | payments-sandbox | team-payments | INACTIVE | aucune |

Cas limites couverts : `old-vpn` est supprimé alors qu'il porte l'action la plus récente, et `platform-dns` a un `destroy` supprimé il y a 1 min. Aucun des deux ne doit remonter, ni dans le tri ni dans `?actionType=destroy`. Les variables d'env `secure`/`hidden` ont la valeur `change-me`.

```bash
curl 'localhost:8080/workspaces?namespace=team-payments&actionType=apply'          # payments-api
curl  localhost:8080/workspaces/a0000000-0000-4000-8000-000000000003               # platform-k8s-cluster
```

Ces résultats sont vérifiés par `internal/fixtures/fixtures_integration_test.go`, sur pgx et gorm.

## Endpoints

| Méthode | Chemin | |
|---|---|---|
| `POST` | `/workspaces` | 201 + `Location` |
| `GET` | `/workspaces` | liste filtrée, triée, paginée |
| `GET` | `/workspaces/{workspaceId}` | avec `actions` et `variables` |
| `PATCH` | `/workspaces/{workspaceId}` | mise à jour partielle |
| `DELETE` | `/workspaces/{workspaceId}` | 204, supprime aussi actions et variables |
| `POST` `GET` | `/workspaces/{workspaceId}/actions` | |
| `GET` `PATCH` `DELETE` | `/workspaces/{workspaceId}/actions/{actionId}` | |
| `POST` `GET` | `/workspaces/{workspaceId}/variables` | |
| `GET` `PATCH` `DELETE` | `/workspaces/{workspaceId}/variables/{name}` | |
| `GET` | `/healthz`, `/docs`, `/openapi.json` | |

Les actions et variables n'existent que sous un workspace : impossible d'en créer sans `workspaceId`, et un workspace inexistant ou supprimé renvoie **404**.

### Liste des workspaces

```
GET /workspaces?name=net&status=ACTIVE&namespace=team-a&actionType=apply&page=2&limit=10
```

| Paramètre | Effet |
|---|---|
| `name` | sous-chaîne, insensible à la casse (`%` et `_` pris littéralement) |
| `status` | `ACTIVE`, `FAILURE`, `INACTIVE`, `DELETED` |
| `namespace` | égalité stricte |
| `actionType` | workspaces ayant au moins une action `init`, `plan`, `apply` ou `destroy` |
| `sort` | `actions_updated_at` (défaut), `updated_at`, `created_at`, `name` |
| `order` | `desc` (défaut), `asc` |
| `page` / `limit` | 1 / 10 par défaut, `limit` ≤ 100 |

Tri par défaut : `UpdatedAt` le plus récent parmi les actions du workspace ; les workspaces sans action viennent toujours en dernier. Chaque élément expose cette date dans `lastActionAt`.

```json
{
  "items": [ { "id": "…", "name": "network", "lastActionAt": "…", "…": "…" } ],
  "pagination": { "page": 2, "limit": 10, "total_rows": 23, "total_pages": 3 }
}
```

## Règles métier

- **Suppressions logiques** (`deleted_at`). Supprimer un workspace supprime ses actions et variables dans la même transaction.
- **`Variable.Name` est une clé unique globale** (pas par workspace) : un doublon renvoie **409**. L'index est partiel (`WHERE deleted_at IS NULL`), donc un nom est réutilisable après suppression.
- **`EnvVars`** est stocké en `jsonb` (liste de `{name, value, secure, hidden}`) ; les noms doivent être uniques dans un workspace. Un `PATCH` avec `envVars` remplace la liste entière.
- `Workspace.Status` vaut `ACTIVE` par défaut ; `Action.Type` est obligatoire. Les deux enums sont validés par l'API **et** par des contraintes `CHECK` en base.
- Création d'action/variable et suppression de workspace sont sérialisées (`SELECT … FOR SHARE`) : pas d'action orpheline sur un workspace en cours de suppression.

## Tests

```bash
make test               # unitaires, sans base
make test-integration   # + intégration sur le PostgreSQL Docker
make test-race          # idem avec -race
make cover              # rapport coverage.html
```

- **Contrat de stockage** (`internal/store/storetest`) : une suite unique exécutée sur les 3 implémentations (mémoire en unitaire, pgx et gorm en intégration), qui garantit qu'elles se comportent à l'identique.
- **API** (`internal/api`) : tests `humatest` sur le store mémoire (validation, codes HTTP, filtres, pagination), et un scénario de bout en bout sur PostgreSQL pour chaque driver, dont un test de concurrence sur l'unicité des variables.
- Les tests d'intégration (build tag `integration`) créent une base jetable par package (`wsapi_test_*`), supprimée à la fin. Serveur cible : `TEST_DATABASE_URL`.

## Arborescence

```
cmd/api-pgx, cmd/api-gorm     points d'entrée
cmd/seed                      chargement des fixtures
internal/api                  handlers Huma, DTO, mapping des erreurs
internal/domain               types métier, enums, filtres, pagination
internal/store                interface Store + fragments SQL partagés (tri, filtres)
internal/store/pgxstore       implémentation pgx
internal/store/gormstore      implémentation gorm (modèles gorm)
internal/store/memstore       implémentation mémoire (tests unitaires)
internal/store/storetest      suite de contrat
internal/migrations           schema.sql embarqué
internal/fixtures             fixtures.sql + chargeur
internal/testutil             bases de test jetables
```
