package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// idempotencyRepo is the PostgreSQL implementation of IdempotencyRepository.
type idempotencyRepo struct{}

// NewIdempotencyRepo returns an IdempotencyRepository backed by PostgreSQL.
func NewIdempotencyRepo(_ interface{}) IdempotencyRepository {
	return &idempotencyRepo{}
}

func (r *idempotencyRepo) Get(ctx context.Context, q Querier, key string) (IdempotencyRecord, error) {
	const query = `
		SELECT key, transfer_id, response_json, status_code
		FROM idempotency_records WHERE key = $1`

	var rec IdempotencyRecord

	err := q.QueryRow(ctx, query, key).Scan(
		&rec.Key, &rec.TransferID, &rec.ResponseJSON, &rec.StatusCode,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IdempotencyRecord{}, domain.ErrTransferNotFound
		}

		slog.Error("idempotency get failed", "layer", "repo", "error", err, "key", key)

		return IdempotencyRecord{}, fmt.Errorf("repo: get idempotency record: %w", err)
	}

	return rec, nil
}

func (r *idempotencyRepo) Create(ctx context.Context, tx pgx.Tx, rec IdempotencyRecord) error {
	const query = `
		INSERT INTO idempotency_records (key, transfer_id, response_json, status_code)
		VALUES ($1, $2, $3, $4)`

	if _, err := tx.Exec(ctx, query, rec.Key, rec.TransferID, rec.ResponseJSON, rec.StatusCode); err != nil {
		slog.Error("idempotency create failed", "layer", "repo", "error", err, "key", rec.Key)

		return fmt.Errorf("repo: create idempotency record: %w", err)
	}

	return nil
}
