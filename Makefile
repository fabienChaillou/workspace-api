# Workspace API — pgx & gorm versions.
# PostgreSQL runs in a local container managed with the docker CLI only.

SHELL := /bin/bash
.DEFAULT_GOAL := help

# ---- PostgreSQL (override on the command line, e.g. `make db-up PG_PORT=5433`)
PG_CONTAINER ?= workspace-api-postgres
PG_IMAGE     ?= postgres:16-alpine
PG_VOLUME    ?= workspace-api-pgdata
PG_HOST      ?= localhost
PG_PORT      ?= 5432
PG_USER      ?= postgres
PG_PASSWORD  ?= postgres
PG_DB        ?= workspace-api
PG_SSLMODE   ?= disable

DATABASE_URL ?= postgres://$(PG_USER):$(PG_PASSWORD)@$(PG_HOST):$(PG_PORT)/$(PG_DB)?sslmode=$(PG_SSLMODE)
export DATABASE_URL

# ---- Go
GO      ?= go
BIN_DIR ?= bin
ADDR    ?= :8080
export ADDR

.PHONY: help deps build build-pgx build-gorm run-pgx run-gorm \
        test test-unit test-integration test-race cover vet fmt clean \
        db-up db-wait db-down db-rm db-reset db-logs db-psql db-status db-url \
        db-seed db-reseed

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# --------------------------------------------------------------------- database

db-up: ## Start PostgreSQL in Docker (creates the container on first run)
	@if docker ps --format '{{.Names}}' | grep -qx '$(PG_CONTAINER)'; then \
	  echo "postgres container $(PG_CONTAINER) already running"; \
	elif docker ps -a --format '{{.Names}}' | grep -qx '$(PG_CONTAINER)'; then \
	  echo "starting existing container $(PG_CONTAINER)"; \
	  docker start $(PG_CONTAINER) >/dev/null; \
	else \
	  echo "creating container $(PG_CONTAINER) ($(PG_IMAGE)) on port $(PG_PORT)"; \
	  docker run -d --name $(PG_CONTAINER) \
	    -e POSTGRES_USER=$(PG_USER) \
	    -e POSTGRES_PASSWORD=$(PG_PASSWORD) \
	    -e POSTGRES_DB=$(PG_DB) \
	    -p $(PG_PORT):5432 \
	    -v $(PG_VOLUME):/var/lib/postgresql/data \
	    $(PG_IMAGE) >/dev/null; \
	fi
	@$(MAKE) --no-print-directory db-wait

db-wait: ## Wait until PostgreSQL accepts TCP connections
	@echo -n "waiting for postgres"; \
	for i in $$(seq 1 60); do \
	  if docker exec $(PG_CONTAINER) pg_isready -h 127.0.0.1 -U $(PG_USER) -d $(PG_DB) >/dev/null 2>&1; then \
	    echo " ready -> $(DATABASE_URL)"; exit 0; \
	  fi; \
	  echo -n "."; sleep 1; \
	done; \
	echo " timeout"; docker logs --tail 50 $(PG_CONTAINER); exit 1

db-down: ## Stop the container (data is kept)
	@docker stop $(PG_CONTAINER) >/dev/null 2>&1 && echo "stopped $(PG_CONTAINER)" || echo "$(PG_CONTAINER) not running"

db-rm: ## Remove the container and its volume (all data lost)
	@docker rm -f $(PG_CONTAINER) >/dev/null 2>&1 || true
	@docker volume rm $(PG_VOLUME) >/dev/null 2>&1 || true
	@echo "removed $(PG_CONTAINER) and volume $(PG_VOLUME)"

db-reset: db-rm db-up ## Recreate an empty database

db-logs: ## Follow PostgreSQL logs
	docker logs -f $(PG_CONTAINER)

db-psql: ## Open psql inside the container
	docker exec -it $(PG_CONTAINER) psql -U $(PG_USER) -d $(PG_DB)

db-status: ## Show the container status
	@docker ps -a --filter name=^/$(PG_CONTAINER)$$ --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'

db-url: ## Print the connection string
	@echo $(DATABASE_URL)

db-seed: db-up ## Load the fixtures (idempotent, keeps existing data)
	$(GO) run ./cmd/seed

db-reseed: db-up ## Empty ALL tables, then load the fixtures with fresh timestamps
	$(GO) run ./cmd/seed -reset

# ------------------------------------------------------------------------- build

deps: ## Download dependencies and complete go.sum
	$(GO) mod tidy

build: build-pgx build-gorm ## Build both binaries into ./bin

build-pgx: ## Build the pgx version
	$(GO) build -o $(BIN_DIR)/api-pgx ./cmd/api-pgx

build-gorm: ## Build the gorm version
	$(GO) build -o $(BIN_DIR)/api-gorm ./cmd/api-gorm

run-pgx: db-up ## Run the pgx version (docs on http://localhost:8080/docs)
	$(GO) run ./cmd/api-pgx

run-gorm: db-up ## Run the gorm version (docs on http://localhost:8080/docs)
	$(GO) run ./cmd/api-gorm

# ------------------------------------------------------------------------- tests

test: test-unit ## Alias of test-unit

test-unit: ## Unit tests (no database needed)
	$(GO) test -count=1 ./...

test-integration: db-up ## Unit + integration tests against the Docker PostgreSQL
	TEST_DATABASE_URL=$(DATABASE_URL) $(GO) test -tags=integration -count=1 ./...

test-race: db-up ## All tests with the race detector
	TEST_DATABASE_URL=$(DATABASE_URL) $(GO) test -race -tags=integration -count=1 ./...

cover: db-up ## Coverage report (coverage.html)
	TEST_DATABASE_URL=$(DATABASE_URL) $(GO) test -tags=integration -count=1 -coverpkg=./internal/... -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1
	$(GO) tool cover -html=coverage.out -o coverage.html

vet: ## go vet (with and without the integration tag)
	$(GO) vet ./...
	$(GO) vet -tags=integration ./...

fmt: ## gofmt the code
	gofmt -s -w .

clean: ## Remove build and coverage artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html
