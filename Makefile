-include .env
export

MOCKGEN_VERSION := 0.6.0
GOLANGCI_VERSION := 2.13.1
GOOSE_VERSION := 3.28.0

BIN_DIR := bin
GOLANGCI := $(BIN_DIR)/golangci-lint
MOCKGEN := $(BIN_DIR)/mockgen
GOOSE := $(BIN_DIR)/goose
GOOSE_DRIVER := postgres
GOOSE_MIGRATION_DIR := migrations/sql
GOOSE_DBSTRING := user=$(PG_USER) password=$(PG_PASS) host=$(PG_HOST) port=$(PG_PORT) \
	dbname=$(PG_NAME) sslmode=$(PG_SSL)

export PATH := $(PATH):$(CURDIR)/$(BIN_DIR)

.PHONY: prepare
prepare:
	@if [ ! -e .env ]; then cp .env.example .env; fi

.PHONY: run-api
run-api:
	@go run ./cmd/api

.PHONY: run-worker
run-worker:
	@go run ./cmd/worker

.PHONY: test
test:
	@go test -short ./...

.PHONY: test-full
test-full:
	@go test -race -p=2 -parallel=4 ./...

.PHONY: coverage
coverage:
	@go test -coverprofile=coverage.out \
	    -coverpkg=$$(go list ./... | grep -v /.*test$ | paste -sd,) ./...
	@go tool cover -func=coverage.out | awk '/^total:/ {print $3}'
	@go tool cover -html=coverage.out
	@rm coverage.out

.PHONY: lint
lint: $(GOLANGCI)
	@$(GOLANGCI) run

.PHONY: format
format: $(GOLANGCI)
	@$(GOLANGCI) fmt

.PHONY: gen
gen: $(MOCKGEN)
	@go generate ./...

.PHONY: migrate-create
migrate-create: $(GOOSE)
	@mkdir -p $(GOOSE_MIGRATION_DIR)
	@read -p "Enter migration name: " name; \
	    $(GOOSE) -s create $$name sql

.PHONY: migrate-up
migrate-up: $(GOOSE)
	@$(GOOSE) up

.PHONY: migrate-down
migrate-down: $(GOOSE)
	@$(GOOSE) down

.PHONY: migrate-status
migrate-status: $(GOOSE)
	@$(GOOSE) status

.PHONY: migrate-validate
migrate-validate: $(GOOSE)
	@$(GOOSE) validate

.PHONY: docker-up
docker-up:
	@docker compose up -d --build --wait

.PHONY: docker-deps
docker-deps:
	@docker compose up -d --wait postgres redis

.PHONY: docker-down
docker-down:
	@docker compose down -v

.PHONY: docker-logs
docker-logs:
	@docker compose logs

$(GOLANGCI):
	@mkdir -p $(BIN_DIR)
	@curl -sSfL https://golangci-lint.run/install.sh | \
        sh -s -- -b $(BIN_DIR) v$(GOLANGCI_VERSION)

$(MOCKGEN):
	@mkdir -p $(BIN_DIR)
	@GOBIN="$(CURDIR)/$(BIN_DIR)" go install go.uber.org/mock/mockgen@v$(MOCKGEN_VERSION)

$(GOOSE):
	@mkdir -p $(BIN_DIR)
	@GOBIN="$(CURDIR)/$(BIN_DIR)" go install github.com/pressly/goose/v3/cmd/goose@v$(GOOSE_VERSION)
