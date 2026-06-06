.PHONY: up down migrate build build-dev lint fmt test test-unit test-coverage

# ─── Variables ────────────────────────────────────────────────────────────────
COMPOSE       = docker compose
LINTER        = golangci-lint
# Runs a one-off command inside the dev container (source mounted at /app)
DOCKER_RUN    = $(COMPOSE) run --rm --no-deps dev

# ─── Lifecycle ────────────────────────────────────────────────────────────────

## Start postgres
up:
	$(COMPOSE) up -d postgres app
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

# ─── Build ────────────────────────────────────────────────────────────────────

## Build inside Docker
build:
	$(COMPOSE) build app

build-dev:
	$(COMPOSE) build dev

# ─── Quality ──────────────────────────────────────────────────────────────────

## Run golangci-lint inside the dev container
lint:
	$(DOCKER_RUN) $(LINTER) run ./...

## Format code with gofmt inside the dev container
fmt:
	$(DOCKER_RUN) gofmt -l -w .

# ─── Tests ────────────────────────────────────────────────────────────────────

## Run all tests with race detector and coverage inside the dev container
test:
	$(DOCKER_RUN) go test -race -count=1 -coverprofile=coverage.out ./...
	$(DOCKER_RUN) go tool cover -func=coverage.out | tail -1

## Run tests in short mode (no integration / testcontainers)
test-unit:
	$(DOCKER_RUN) go test -short -race -count=1 ./...

## Run tests and produce HTML coverage report
test-coverage:
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"
