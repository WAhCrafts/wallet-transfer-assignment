-- 000001_init_schema.up.sql
-- Creates the initial schema for the wallet transfer service.

BEGIN;

-- ── wallets ──────────────────────────────────────────────────────────────────
-- Stores wallet identity and running balance.
-- balance is kept as a stored denormalised sum alongside ledger entries for fast
-- reads; it is always updated atomically within the transfer transaction.
-- version supports optimistic-locking checks in application code.
CREATE TABLE wallets (
    id         UUID        PRIMARY KEY,
    balance    NUMERIC(20, 4) NOT NULL DEFAULT 0
                           CHECK (balance >= 0),
    version    BIGINT      NOT NULL    DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL    DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL    DEFAULT NOW()
);

-- ── transfers ─────────────────────────────────────────────────────────────────
-- Records a transfer request and its lifecycle.
-- idempotency_key carries a UNIQUE constraint so that two concurrent first-time
-- requests with the same key race to insert; only one wins.
-- status is stored as a SMALLINT matching TransferStatus domain constants:
--   1 = PENDING, 2 = PROCESSED, 3 = FAILED
CREATE TABLE transfers (
    id               UUID           PRIMARY KEY,
    idempotency_key  TEXT           NOT NULL,
    from_wallet_id   UUID           NOT NULL REFERENCES wallets(id),
    to_wallet_id     UUID           NOT NULL REFERENCES wallets(id),
    amount           NUMERIC(20, 4) NOT NULL CHECK (amount > 0),
    status           SMALLINT       NOT NULL DEFAULT 1
                                    CHECK (status IN (1, 2, 3)),
    created_at       TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_transfers_idempotency_key UNIQUE (idempotency_key),
    CONSTRAINT chk_different_wallets CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX idx_transfers_from_wallet ON transfers (from_wallet_id);
CREATE INDEX idx_transfers_to_wallet   ON transfers (to_wallet_id);
CREATE INDEX idx_transfers_status      ON transfers (status);

-- ── ledger_entries ────────────────────────────────────────────────────────────
-- Double-entry bookkeeping: every transfer produces exactly two rows.
-- type is stored as a SMALLINT matching EntryType domain constants:
--   1 = DEBIT (money leaving a wallet)
--   2 = CREDIT (money entering a wallet)
CREATE TABLE ledger_entries (
    id          UUID           PRIMARY KEY,
    transfer_id UUID           NOT NULL REFERENCES transfers(id),
    wallet_id   UUID           NOT NULL REFERENCES wallets(id),
    type        SMALLINT       NOT NULL CHECK (type IN (1, 2)),
    amount      NUMERIC(20, 4) NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ledger_transfer ON ledger_entries (transfer_id);
CREATE INDEX idx_ledger_wallet   ON ledger_entries (wallet_id);

-- ── idempotency_records ───────────────────────────────────────────────────────
-- Caches the full HTTP response body and status code for each idempotency key.
-- A successful or failed transfer is written here within the same transaction
-- so that duplicate requests receive an identical response without re-executing
-- any business logic.
CREATE TABLE idempotency_records (
    key           TEXT        PRIMARY KEY,
    transfer_id   UUID        NOT NULL,
    response_json TEXT        NOT NULL,
    status_code   SMALLINT    NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMIT;
