# Wallet Transfer Assignment Repository

> Branch: `solution/waseem`

## Prerequisites

- Go 1.24+
- Docker Desktop (for PostgreSQL and SonarQube)

## Quick start

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

## Development commands

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

## Running tests

```bash
# Unit tests only (domain, service, handler — no Docker)
go test ./internal/domain/... ./internal/handler/... ./internal/service/... -count=1

# Integration tests (requires Docker)
go test ./internal/repository/... ./internal/service/... -count=1 -run Integration

# All tests including concurrency
make test
```

## API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/transfers` | Initiate a transfer (idempotent via `idempotencyKey`) |
| `GET` | `/transfers/{id}` | Fetch a transfer by UUID |
| `GET` | `/wallets/{id}` | Fetch a wallet and its current balance |

### POST /transfers

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
- Successful response looks like:
   ```json
  {
    "id": "<uuid>",
    "status": "PROCESSED",
    "amount" 10000
  }
  ```

### Sample Requests

```bash
# 1) Create transfer (first call: 201 Created)
curl -i -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "demo-key-001",
    "fromWalletId": "11111111-1111-1111-1111-111111111111",
    "toWalletId": "22222222-2222-2222-2222-222222222222",
    "amount": 10000
  }'

# 2) Replay same transfer request (same idempotencyKey: 200 OK)
curl -i -X POST http://localhost:8080/transfers \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "demo-key-001",
    "fromWalletId": "11111111-1111-1111-1111-111111111111",
    "toWalletId": "22222222-2222-2222-2222-222222222222",
    "amount": 10000
  }'

# 3) Get transfer by ID
curl -i http://localhost:8080/transfers/<transfer-uuid>

# 4) Get wallet by ID
curl -i http://localhost:8080/wallets/<wallet-uuid>
```

## Architecture

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

See `docs/` for per-layer design rationale, prompts history and 
future improvements plan.
