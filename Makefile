.PHONY: all up down migrate build lint fmt test sonar

# ─── Variables ────────────────────────────────────────────────────────────────
COMPOSE       = docker compose
LINTER        = golangci-lint
SONAR_TOKEN   ?=
SONAR_HOST    ?= http://localhost:9000

# ─── Lifecycle ────────────────────────────────────────────────────────────────

## Start postgres + sonarqube containers
up:
	$(COMPOSE) up -d postgres sonarqube
	@echo "Waiting for postgres..."
	@$(COMPOSE) exec postgres sh -c 'until pg_isready -U wallet -d wallet_db; do sleep 1; done'

## Stop and remove all containers
down:
	$(COMPOSE) down -v

## Apply database migrations (requires postgres running)
migrate:
	$(COMPOSE) run --rm \
		-e DATABASE_URL=postgres://wallet:wallet@postgres:5432/wallet_db?sslmode=disable \
		app ./wallet-service migrate || \
	go run ./cmd/server migrate

## Apply migrations locally (requires local postgres or DATABASE_URL)
migrate-local:
	DATABASE_URL=$${DATABASE_URL:-postgres://wallet:wallet@localhost:5432/wallet_db?sslmode=disable} \
		go run ./cmd/server migrate

# ─── Build ────────────────────────────────────────────────────────────────────

## Build the Go binary
build:
	CGO_ENABLED=0 go build -o bin/wallet-service ./cmd/server

## Build inside Docker
build-docker:
	$(COMPOSE) build app

# ─── Quality ──────────────────────────────────────────────────────────────────

## Run golangci-lint
lint:
	$(LINTER) run ./...

## Format code with gofmt
fmt:
	go fmt -x

# ─── Tests ────────────────────────────────────────────────────────────────────

## Run all tests with race detector and coverage
test:
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

## Run tests in short mode (no integration / testcontainers)
test-unit:
	go test -short -race -count=1 ./...

## Run tests and produce HTML coverage report
test-coverage:
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# ─── SonarQube ────────────────────────────────────────────────────────────────

## Run sonar-scanner against local SonarQube CE
sonar:
	@if [ -z "$(SONAR_TOKEN)" ]; then \
		echo "SONAR_TOKEN is not set. Running without auth (default admin:admin)..."; \
		$(COMPOSE) run --rm sonar-scanner \
			sonar-scanner \
			-Dsonar.host.url=$(SONAR_HOST) \
			-Dsonar.login=admin \
			-Dsonar.password=admin; \
	else \
		$(COMPOSE) run --rm sonar-scanner \
			sonar-scanner \
			-Dsonar.host.url=$(SONAR_HOST) \
			-Dsonar.token=$(SONAR_TOKEN); \
	fi

# ─── All-in-one ───────────────────────────────────────────────────────────────

## Full quality pipeline: fmt → lint → test → build
all: fmt lint test build
