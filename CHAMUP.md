---
## Iniatl besoin

Postgres a crashé pendant le test — je regarde le log.

Les deux binaires répondent à l'identique sur la même base. J'écris maintenant le Makefile et le README.

Le projet est dans `workspace-api.tar.gz`. Tous les tests passent sur un vrai PostgreSQL 16, y compris avec `-race` :
- 13 cas de contrat, exécutés sur pgx, gorm et le store mémoire ;
- les tests unitaires de l'API ;
- un scénario HTTP de bout en bout pour chaque driver.

J'ai aussi lancé les deux binaires en même temps sur la même base. Ils renvoient exactement la même réponse.

**Pour démarrer :**
```bash
make deps && make db-up && make run-pgx   # ou run-gorm → http://localhost:8080/docs
make test-integration
```

**Ce que contient le code :**
- **Deux versions, un seul cœur.** L'API Huma passe par une interface `Store`, implémentée par `pgxstore` (SQL écrit à la main) et `gormstore` (tes structs avec les tags gorm). Le tri et les filtres sont un fragment SQL commun, donc les deux renvoient les mêmes résultats.
- **Filtres** : `name` (sous-chaîne, insensible à la casse), `status`, `namespace` et `actionType` (le workspace a au moins une action de ce type).
- **Pagination** : `page`, et `limit` à 10 par défaut. La réponse contient ta struct `Pagination` : `total_rows` et `total_pages`.
- **Tri par défaut** : le `UpdatedAt` le plus récent parmi les actions de chaque workspace. Les workspaces sans action sont toujours en fin de liste. D'autres tris sont possibles avec `sort`/`order`.
- **Actions et variables** n'existent que sous `/workspaces/{id}/…`. Un workspace absent ou supprimé renvoie 404.

**Points où j'ai tranché, à vérifier :**
- **`FAILLURE` → `FAILURE`** et **mot de passe `porstgres` → `postgres`** : j'ai supposé des fautes de frappe. C'est modifiable : l'enum est dans `domain.go` et `schema.sql`, le mot de passe se change avec `make db-up PG_PASSWORD=…`.
- **`EnvVars` est une liste** de `{name, value, secure, hidden}`, stockée en `jsonb`, et non un seul objet.
- **`Variable.Name` est unique sur toute la base**, pas par workspace, comme demandé. L'unicité ne compte que les variables non supprimées : un nom redevient disponible après suppression.
- **Les suppressions sont logiques** (`DeletedAt`). Supprimer un workspace supprime aussi ses actions et variables.
- **Schéma partagé** : la version gorm utilise le même `schema.sql` que pgx plutôt qu'`AutoMigrate`. C'est ce qui permet de lancer les deux versions sur la même base.

**Limites de la vérification ici :**
- Docker n'était pas disponible dans mon environnement. J'ai fait tourner les tests sur un PostgreSQL 16 local avec la même connexion. Les cibles `db-*` du Makefile n'ont été vérifiées qu'en dry-run.
- Mon environnement ne pouvait pas télécharger les modules `gorm.io/*`, donc leurs entrées manquent dans `go.sum`. `make deps` (`go mod tidy`) les ajoute.
- Les versions sont fixées pour Go 1.24 (huma v2.36, pgx v5.8). Avec Go 1.25 ou plus récent, tu peux les mettre à jour.

---

Je vais ajouter un jeu de fixtures réaliste (workspaces Terraform répartis sur plusieurs namespaces, avec actions et variables). Il sera chargé par une commande `seed` commune aux deux versions, puisque pgx et gorm partagent le même schéma. Il sera couvert par un test d'intégration qui vérifie que tri, filtres et pagination donnent les résultats attendus.

Le PostgreSQL de test s'est arrêté entre les deux tours (environnement redémarré) — je le relance.

Les données sont correctes, c'est mon décompte attendu qui était faux (`sandbox-alice` a aussi un `apply`). Je corrige l'assertion.

Tout est vert. J'ajoute les cibles Makefile et la doc.

Les fixtures sont ajoutées dans `workspace-api.tar.gz` :

```bash
make db-seed     # charge les fixtures, sans toucher aux données existantes
make db-reseed   # vide TOUTES les tables puis recharge, avec des dates fraîches
```

**Contenu.** Le jeu de données compte 11 workspaces Terraform visibles, répartis sur 5 namespaces (`platform`, `team-payments`, `team-data`, `sandbox`, `ops`). Il contient aussi un workspace supprimé, 20 actions et 14 variables. Les 4 statuts sont représentés, ainsi que les 4 types d'action, et des variables d'env `secure`/`hidden` avec la valeur factice `change-me`. Le fichier est un SQL unique (`internal/fixtures/fixtures.sql`), commun aux versions pgx et gorm puisqu'elles partagent le schéma.

**Choix de conception :**
- **UUID fixes** (`a0000000-…-000000000001` à `…12`), exportés en constantes Go. Tu peux les utiliser directement avec curl ou dans d'autres tests.
- **Dates relatives à `now()`.** Le tri par défaut est donc lisible juste après `make db-reseed` : `payments-db` (plan en cours, il y a 5 min) arrive en tête, et les deux workspaces sans action arrivent en fin. Le README contient le tableau de l'ordre attendu.
- **Deux pièges volontaires.** `old-vpn` est supprimé mais porte l'action la plus récente. `platform-dns` a un `destroy` supprimé il y a 1 minute. Aucun des deux ne doit apparaître, ni dans le tri ni dans `?actionType=destroy`.
- **Chargement idempotent.** Les lignes déjà présentes sont ignorées. `db-seed` n'écrase donc rien, mais garde les dates du premier chargement. C'est pour ça que `db-reseed` existe.

Un test d'intégration vérifie, sur pgx et sur gorm :
- l'ordre complet sur les deux pages ;
- les comptes de chaque filtre ;
- l'idempotence ;
- le fait que les éléments supprimés restent invisibles.

Toute la suite passe, y compris avec `-race`. `make db-seed` lance la commande Go `cmd/seed` : il faut donc avoir fait `make deps` au préalable.
