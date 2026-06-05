# Wallet Transfer Assignment Repository

This repository is a reusable coding assignment template for evaluating backend engineers on wallet transfers, idempotency, concurrency control, and double-entry ledger design.

## Included

- `ASSIGNMENT.md` - candidate-facing prompt
- `.github/pull_request_template.md` - required PR structure
- `.github/workflows/ci.yml` - lint, format, test placeholder workflow
- `.github/workflows/sonarqube.yml` - SonarQube pull request analysis
- `.github/copilot-instructions.md` - repository-level Copilot review guidance
- `evaluation_guide.md` - reviewer rubric
- `branch-protection-checklist.md` - GitHub setup checklist

## Intended use

1. Mark this repository as a GitHub template repository.
2. Create one private repository per candidate from the template.
3. Add the candidate as a collaborator.
4. Ask them to submit via a pull request into `main`.
5. Enable required checks, SonarQube, and Copilot review in GitHub.

## Notes

- Copilot automatic pull request review is configured in GitHub repository or organization settings, not purely through files in the repo.
- The `copilot-instructions.md` file included here provides repository-specific review guidance once Copilot review is enabled.
- The CI workflow is language-agnostic by default and expects you to set the `LINT_CMD`, `FORMAT_CHECK_CMD`, and `TEST_CMD` repository variables or replace the commands directly.

## How to Submit Assignment

1. **Fork this repository** to your own GitHub account.
2. Complete the assignment described in [`ASSIGNMENT.md`](./ASSIGNMENT.md).
3. **Raise a Pull Request** back to this repository (`main` branch) with your full solution.

Your PR branch should be named: `solution/<your-name>` (e.g., `solution/jane-doe`).

---

## Solution — Wallet Transfer Service (Go)

> Branch: `waseem`

### Prerequisites

- Go 1.24+
- Docker Desktop (for PostgreSQL and SonarQube)

### Quick start

```bash
# Start PostgreSQL + SonarQube
make up

# Run database migrations
make migrate

# Build the binary
make build

# Start the server (runs migrations automatically)
./wallet-transfer-server
```

### Development commands

| Command | Description |
|---------|-------------|
| `make up` | Start PostgreSQL + SonarQube via Docker Compose |
| `make down` | Stop and remove containers |
| `make migrate` | Run database migrations (requires running Postgres) |
| `make build` | Compile the server binary |
| `make fmt` | Run `gofmt` and `goimports` |
| `make lint` | Run `golangci-lint` |
| `make test` | Run all unit tests (no Docker needed) |
| `make sonar` | Upload coverage report to SonarQube |
| `make all` | fmt → lint → test → build |

### Running tests

```bash
# Unit tests only (domain, service, handler — no Docker)
go test ./internal/domain/... ./internal/handler/... ./internal/service/... -count=1

# Integration tests (requires Docker)
go test ./internal/repository/... ./internal/service/... -count=1 -run Integration

# All tests including concurrency
make test
```

### API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/transfers` | Initiate a transfer (idempotent via `idempotencyKey`) |
| `GET` | `/transfers/{id}` | Fetch a transfer by UUID |
| `GET` | `/wallets/{id}` | Fetch a wallet and its current balance |

#### POST /transfers

```json
{
  "idempotencyKey": "unique-client-key",
  "fromWalletId": "<uuid>",
  "toWalletId": "<uuid>",
  "amount": 10000
}
```

- `amount` is in minor units (cents). `10000` = $100.00.
- Returns `201 Created` for new transfers, `200 OK` for idempotent replays.

### Architecture

```
cmd/server/          — entrypoint, wiring, graceful shutdown
internal/domain/     — value types, entities, sentinel errors
internal/repository/ — PostgreSQL adapters (pgx v5)
internal/service/    — transfer workflow, idempotency, concurrency
internal/handler/    — HTTP handlers (chi router)
internal/db/         — connection pool, migration runner
migrations/          — SQL migrations (golang-migrate, embedded)
docs/                — design notes per layer
```

See `docs/` for per-layer design rationale.

