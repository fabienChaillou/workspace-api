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

---

`sqlc` est un excellent choix en Go si tu utilises PostgreSQL et que tu veux écrire du SQL directement, tout en générant automatiquement du code Go typé.

L'idée est simple : tu écris tes requêtes SQL, et `sqlc` génère les fonctions Go correspondantes. Tu évites ainsi de coder manuellement le scan des lignes, les conversions de types et une bonne partie du code répétitif.

## 1. Comment fonctionne sqlc ?

1\. Schéma PostgreSQL

schema.sql — tables et types

2\. Requêtes SQL

query.sql — SELECT, INSERT, UPDATE, DELETE

3\. Génération sqlc

sqlc generate

4\. Code Go généré

Modèles, méthodes et paramètres typés

Tu conserves le contrôle sur les requêtes SQL et tu bénéficies de la sécurité des types à la compilation. `sqlc` ne remplace pas PostgreSQL : il génère une couche d'accès aux données.

## 2. Installer sqlc sur macOS

Avec Homebrew :

```
brew install sqlc
```

Vérifie l'installation :

```
sqlc version
```

Tu peux aussi l'intégrer dans ton projet Go avec un `go.mod` classique. Pour PostgreSQL, je te conseille généralement le driver `pgx/v5`.

## 3. Créer un projet complet

Prenons un exemple avec PostgreSQL et une table `books`, en utilisant `pgx/v5`.

### Structure du projet

```
myapp/
├── go.mod
├── sqlc.yaml
├── sql/
│   ├── schema.sql
│   └── queries/
│       └── books.sql
└── internal/
    └── db/              # Code généré
```

### Étape 1 — Définir le schéma SQL

Dans `sql/schema.sql` :

```
CREATE TABLE books (
    id         BIGSERIAL PRIMARY KEY,
    title      TEXT NOT NULL,
    year       INT NOT NULL,
    available  BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Ce fichier décrit la structure que `sqlc` doit connaître. Il ne crée pas automatiquement la table dans ta base PostgreSQL.

### Étape 2 — Configurer sqlc

Dans `sqlc.yaml` :

```

version: "2"

sql:
  - engine: "postgresql"
    schema: "sql/schema.sql"
    queries: "sql/queries"
    gen:
      go:
        package: "db"
        out: "internal/db"
        sql_package: "pgx/v5"
        emit_interface: true
        emit_json_tags: true

```

La configuration indique à `sqlc` où trouver le schéma et les requêtes, et où générer le code Go. `emit_interface: true` demande également la génération d'une interface pour les requêtes.&#x20;

[image](https://www.google.com/s2/favicons?domain=https://github.com\&sz=32)

GitHub

+1



### Étape 3 — Écrire les requêtes SQL

Dans `sql/queries/books.sql` :

```
-- name: GetBook :one
SELECT id, title, year, available, created_at
FROM books
WHERE id = $1;

-- name: ListBooks :many
SELECT id, title, year, available, created_at
FROM books
ORDER BY id;

-- name: CreateBook :one
INSERT INTO books (title, year, available)
VALUES ($1, $2, $3)
RETURNING id, title, year, available, created_at;

-- name: UpdateBook :execrows
UPDATE books
SET title = $2,
    year = $3,
    available = $4
WHERE id = $1;

-- name: DeleteBook :exec
DELETE FROM books
WHERE id = $1;
```

Les annotations déterminent le type de fonction Go générée :

| Annotation  | Résultat                       |
| ----------- | ------------------------------ |
| `:one`      | Une ligne                      |
| `:many`     | Une liste de lignes            |
| `:exec`     | Exécution sans résultat métier |
| `:execrows` | Nombre de lignes affectées     |

Tu peux consulter la liste complète des annotations dans la [documentation officielle de sqlc](https://docs.sqlc.dev/).

### Étape 4 — Générer le code Go

À la racine du projet :

```
go mod init example.com/myapp

go get github.com/jackc/pgx/v5

sqlc generate

go mod tidy
```

Tu obtiens notamment :

- `internal/db/models.go` : structures Go correspondant aux tables ;
- `internal/db/books.sql.go` : fonctions générées pour tes requêtes ;
- `internal/db/db.go` : interface d'accès et initialisation.

Tu n'as normalement pas besoin de modifier ces fichiers générés à la main.

## 4. Utiliser le code généré dans Go

Voici comment initialiser un pool de connexions PostgreSQL, puis utiliser les méthodes générées.

```
package main

import (
    "context"
    "log"
    "os"

    "github.com/jackc/pgx/v5/pgxpool"

    "example.com/myapp/internal/db"
)

func main() {
    ctx := context.Background()

    pool, err := pgxpool.New(
        ctx,
        os.Getenv("DATABASE_URL"),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    if err := pool.Ping(ctx); err != nil {
        log.Fatal(err)
    }

    queries := db.New(pool)

    // Créer un livre.
    book, err := queries.CreateBook(ctx, db.CreateBookParams{
        Title:     "The Go Programming Language",
        Year:      2015,
        Available: true,
    })
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Livre créé : %+v", book)

    // Récupérer le livre.
    fetched, err := queries.GetBook(ctx, book.ID)
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Livre récupéré : %+v", fetched)

    // Lister tous les livres.
    books, err := queries.ListBooks(ctx)
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Nombre de livres : %d", len(books))
}
```

Configure ensuite ta variable d'environnement `DATABASE_URL` avec l'URL de ta base. Par exemple, pour une base PostgreSQL locale dont le serveur n'exige pas SSL :

```
export DATABASE_URL='postgres://app:secret@localhost:5432/myapp?sslmode=disable'
```

Attention : la table `books` doit déjà exister dans la base. Applique ton schéma avec ton outil de migration habituel avant d'exécuter le programme.

## 5. Gérer les transactions

`sqlc` fonctionne aussi avec les transactions PostgreSQL. C'est particulièrement utile lorsque plusieurs écritures doivent réussir ou échouer ensemble.

```
tx, err := pool.Begin(ctx)
if err != nil {
    return err
}
defer tx.Rollback(ctx)

qtx := queries.WithTx(tx)

book, err := qtx.CreateBook(ctx, db.CreateBookParams{
    Title:     "Effective Go",
    Year:      2025,
    Available: true,
})
if err != nil {
    return err
}

if err := tx.Commit(ctx); err != nil {
    return err
}

log.Printf("Livre créé : %+v", book)
```

Le principe est de créer une instance des requêtes liée à la transaction avec `WithTx`. Toutes les méthodes exécutées via `qtx` utilisent alors cette transaction.

## 6. Intégrer sqlc dans un projet professionnel

Pour un projet Go avec PostgreSQL, je recommande cette organisation :

`sql/` — Schéma et requêtes

SQL versionné, migrations et requêtes métier lisibles.

`internal/db/` — Code généré

Types et méthodes générés, sans modification manuelle.

`internal/repository/` — Couche d'accès métier

Interface métier, traduction des erreurs et assemblage des requêtes.

CI/CD — Validation

Générer le code, vérifier les différences Git et exécuter les tests.

Quelques bonnes pratiques :

- Ne modifie jamais le code généré manuellement : modifie le SQL puis relance `sqlc generate`.
- Teste tes requêtes sur PostgreSQL réel, par exemple avec Testcontainers, pour vérifier les comportements spécifiques au moteur.
- Utilise `WithTx` pour partager une transaction entre plusieurs requêtes.
- Versionne le fichier `sqlc.yaml` et les requêtes SQL.
- Vérifie en CI que le code généré est à jour, par exemple en exécutant `sqlc generate` puis `git diff --exit-code`.

Documentation : [sqlc — Getting started](https://docs.sqlc.dev/en/latest/tutorials/getting-started-postgresql.html) et [utilisation de pgx](https://docs.sqlc.dev/en/latest/guides/using-go-and-pgx.html).

---

Je te propose un projet Go directement exploitable, avec `sqlc`, PostgreSQL, `pgx/v5`, une architecture repository / service / handler HTTP et des tests d'intégration avec Testcontainers.

L'objectif est de garder SQL explicite, types Go générés, logique métier testable et accès à PostgreSQL réel dans les tests, sans ORM ni mocks de base de données.

On va construire une API de gestion de livres avec :

- CRUD complet.
- Filtres combinables : titre, mot-clé, année, disponibilité.
- Pagination avec `limit` et `offset`.
- Tri contrôlé par une liste de colonnes autorisées.
- Transactions PostgreSQL.
- Validation des paramètres HTTP et gestion des erreurs.
- Tests d'intégration avec une instance PostgreSQL éphémère.
- Génération `sqlc` reproductible en CI.

## 1. Architecture du projet

HTTP Handler — Gin

Parsing, validation, HTTP status codes

Service

Règles métier, validation et transactions

Repository

Interface métier et adaptation des requêtes sqlc

PostgreSQL — pgxpool

SQL généré, index et transactions

Le code généré reste isolé dans `internal/db/sqlc`. Le service ne dépend pas directement de `pgxpool`, et le handler ne connaît pas les détails SQL.

## 2. Projet complet prêt à récupérer

J'ai préparé une archive du projet avec les fichiers SQL, la configuration `sqlc`, les couches Go, le serveur HTTP, les transactions et un test d'intégration PostgreSQL avec Testcontainers.

sqlc-books-api.zip

Projet Go · SQL · PostgreSQL · Testcontainers

Télécharger le projet complet

L'archive contient notamment :

- `sqlc.yaml` : configuration de génération.
- `sql/schema.sql` : tables et index PostgreSQL.
- `sql/queries/books.sql` : CRUD, filtres, comptage et pagination.
- `internal/domain/` : structures métier.
- `internal/repository/` : implémentation PostgreSQL et test d'intégration.
- `internal/service/` : règles métier.
- `internal/handler/` : endpoints Gin.
- `cmd/api/main.go` : point d'entrée de l'application.
- `Makefile` et `README.md`.

Le répertoire `internal/db/sqlc/` est généré par `sqlc` : il faut donc exécuter la génération avant de compiler le projet. Les versions des dépendances seront résolues par Go lors de `go mod tidy`.

## 3. Installer et lancer le projet sur macOS

Après avoir décompressé l'archive :

```
cd sqlc-books-api

brew install sqlc

go mod tidy
sqlc generate
```

Il faut également que Docker soit démarré pour exécuter les tests d'intégration. Testcontainers démarre un vrai PostgreSQL dans un conteneur temporaire, plutôt que de simuler la base.&#x20;

[image](https://www.google.com/s2/favicons?domain=https://golang.testcontainers.org\&sz=32)

Testcontainers for Go

+1



Lance les tests :

```
go test -v ./...
```

Puis démarre l'API après avoir créé la base et appliqué `sql/schema.sql` :

```
export DATABASE_URL='postgres://app:secret@localhost:5432/books?sslmode=disable'

go run ./cmd/api
```

## 4. Tester les filtres, la pagination et le tri

Une fois l'API démarrée, tu peux tester une requête combinant plusieurs critères :

```
curl -G http://localhost:8080/books \
  --data-urlencode 'title=Go' \
  --data-urlencode 'keyword=programming' \
  --data-urlencode 'year=2015' \
  --data-urlencode 'available=true' \
  --data-urlencode 'limit=10' \
  --data-urlencode 'offset=0' \
  --data-urlencode 'sort_by=title' \
  --data-urlencode 'sort_dir=asc'
```

La réponse contient les livres correspondants et le nombre total de résultats avant pagination :

```
{
  "items": [],
  "total": 0,
  "limit": 10,
  "offset": 0
}
```

Les valeurs de `sort_by` sont limitées à `title`, `year` et `created_at`, et `sort_dir` à `asc` ou `desc`. Le tri est réalisé par des expressions SQL `CASE`, plutôt que par une concaténation dangereuse de paramètres dans la requête.

## 5. Points à renforcer avant la production

L'archive constitue une base de travail, mais je n'ai pas exécuté ici `sqlc generate`, la compilation Go ni les tests dans Docker. Il faudra valider ces étapes dans ton environnement.

Je recommande également d'ajouter avant une mise en production :

- Une distinction entre erreurs de validation HTTP `400` et erreurs internes `500` — le gestionnaire d'erreurs fourni reste à améliorer sur ce point.
- Des tests couvrant les échecs de transaction, les filtres combinés et les limites de pagination.
- Un outil de migration versionnée pour gérer les évolutions du schéma.
- Des tests HTTP du handler en plus des tests d'intégration du repository.
- Une stratégie de timeout et d'arrêt propre du serveur.

Enfin, pour les listes volumineuses, le couple `COUNT(*)` + requête paginée est simple et prévisible, mais peut coûter cher sur une table très importante. Il faudra mesurer les performances avec des données représentatives.

La documentation de référence reste [sqlc](https://docs.sqlc.dev/en/latest/) et le [module PostgreSQL de Testcontainers Go](https://golang.testcontainers.org/modules/postgres/).

---