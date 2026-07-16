GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOOSE ?= goose
COMPOSE ?= docker compose
DB_SERVICE ?= db
MIGRATIONS_DIR := migrations
DATABASE_URL ?= postgres://relay:relay@localhost:5434/relay?sslmode=disable

.PHONY: api check db-down db-logs db-reset db-shell db-up fmt lint migrate-down migrate-status migrate-up migrate-validate relayctl test test-integration test-race

api:
	DATABASE_URL="$(DATABASE_URL)" $(GO) run ./cmd/api $(ARGS)

check: test test-race lint

fmt:
	$(GOLANGCI_LINT) fmt ./...

lint:
	$(GOLANGCI_LINT) run ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

test-integration:
	DATABASE_URL="$(DATABASE_URL)" $(GO) test -tags=integration ./internal/postgres ./internal/workflow

relayctl:
	DATABASE_URL="$(DATABASE_URL)" $(GO) run ./cmd/relayctl $(ARGS)

db-up:
	$(COMPOSE) up -d --wait --remove-orphans $(DB_SERVICE)

db-down:
	$(COMPOSE) down --remove-orphans

db-logs:
	$(COMPOSE) logs --follow $(DB_SERVICE)

db-shell:
	$(COMPOSE) exec $(DB_SERVICE) psql -U relay -d relay

db-reset:
	$(COMPOSE) down --volumes --remove-orphans
	$(MAKE) db-up

migrate-validate:
	$(GOOSE) -dir $(MIGRATIONS_DIR) validate

migrate-status:
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" status

migrate-up:
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" up

migrate-down:
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" down
