package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

// DB is the subset of pgxpool.Pool the service needs — only Begin.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	repository.Querier
}

// TransferRequest holds the input for a transfer operation.
type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         domain.Amount
}

// TransferResponse is the result returned to the caller and cached for
// idempotency replay.
type TransferResponse struct {
	TransferID uuid.UUID             `json:"id"`
	Status     domain.TransferStatus `json:"status"`
	Amount     domain.Amount         `json:"amount"`
	FromCache  bool                  `json:"-"` // true when served from idempotency cache
}

// TransferService orchestrates the wallet transfer workflow.
type TransferService struct {
	db        DB
	wallets   repository.WalletRepository
	transfers repository.TransferRepository
	ledger    repository.LedgerRepository
	idem      repository.IdempotencyRepository
	log       *slog.Logger
}

// NewTransferService constructs a TransferService with the supplied
// dependencies. All parameters are required.
func NewTransferService(
	db DB,
	wallets repository.WalletRepository,
	transfers repository.TransferRepository,
	ledger repository.LedgerRepository,
	idem repository.IdempotencyRepository,
	log *slog.Logger,
) *TransferService {
	return &TransferService{
		db:        db,
		wallets:   wallets,
		transfers: transfers,
		ledger:    ledger,
		idem:      idem,
		log:       log,
	}
}

// Execute performs a wallet-to-wallet transfer or returns the cached result
// if the idempotency key was already used.
//
// Workflow:
//  1. Validate inputs (same wallet, non-positive amount).
//  2. Look up idempotency record → return early if found.
//  3. BEGIN transaction.
//  4. Lock wallets FOR UPDATE in deterministic UUID order (prevents deadlocks).
//  5. Validate source balance.
//  6. Create transfer record (PENDING).
//  7. Update wallet balances.
//  8. Insert two ledger entries (DEBIT + CREDIT).
//  9. Update transfer status → PROCESSED.
//  10. Write idempotency record.
//  11. COMMIT.
func (s *TransferService) Execute(ctx context.Context, req TransferRequest) (TransferResponse, error) {
	if err := s.validate(req); err != nil {
		return TransferResponse{}, err
	}

	// ── 1. Idempotency fast-path ────────────────────────────────────────────
	if cached, err := s.idem.Get(ctx, s.db, req.IdempotencyKey); err == nil {
		s.log.Info("idempotency cache hit",
			"layer", "svc",
			"idempotencyKey", req.IdempotencyKey,
			"transferID", cached.TransferID,
		)

		var resp TransferResponse
		if jsonErr := json.Unmarshal([]byte(cached.ResponseJSON), &resp); jsonErr != nil {
			return TransferResponse{}, fmt.Errorf("svc: unmarshal cached response: %w", jsonErr)
		}

		resp.FromCache = true

		return resp, nil
	}

	// ── 2. Transactional transfer ───────────────────────────────────────────
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return TransferResponse{}, fmt.Errorf("svc: begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// Lock wallets in ascending UUID order to prevent deadlocks.
	fromW, toW, lockErr := s.lockWallets(ctx, tx, req.FromWalletID, req.ToWalletID)
	if lockErr != nil {
		err = lockErr

		return TransferResponse{}, err
	}

	// Balance check.
	if fromW.Balance < req.Amount {
		err = domain.ErrInsufficientFunds
		s.log.Warn("insufficient funds",
			"layer", "svc",
			"idempotencyKey", req.IdempotencyKey,
			"walletID", fromW.ID,
			"balance", fromW.Balance,
			"requested", req.Amount,
		)

		return TransferResponse{}, err
	}

	// Create transfer in PENDING state.
	transfer := domain.NewTransfer(req.FromWalletID, req.ToWalletID, req.IdempotencyKey, req.Amount)

	if createErr := s.transfers.Create(ctx, tx, transfer); createErr != nil {
		err = fmt.Errorf("svc: create transfer: %w", createErr)

		return TransferResponse{}, err
	}

	s.log.Info("transfer state transition",
		"layer", "svc",
		"idempotencyKey", req.IdempotencyKey,
		"transferID", transfer.ID,
		"from", domain.TransferStatusPending.String(),
		"to", "executing",
	)

	// Update balances.
	if updateErr := s.wallets.UpdateBalance(ctx, tx, fromW.ID, fromW.Balance-req.Amount); updateErr != nil {
		err = fmt.Errorf("svc: debit wallet: %w", updateErr)

		return TransferResponse{}, err
	}

	if updateErr := s.wallets.UpdateBalance(ctx, tx, toW.ID, toW.Balance+req.Amount); updateErr != nil {
		err = fmt.Errorf("svc: credit wallet: %w", updateErr)

		return TransferResponse{}, err
	}

	// Insert double-entry ledger records.
	debit := domain.NewLedgerEntry(transfer.ID, fromW.ID, domain.EntryTypeDebit, req.Amount)
	credit := domain.NewLedgerEntry(transfer.ID, toW.ID, domain.EntryTypeCredit, req.Amount)

	if ledgerErr := s.ledger.CreateEntries(ctx, tx, debit, credit); ledgerErr != nil {
		err = fmt.Errorf("svc: create ledger entries: %w", ledgerErr)

		return TransferResponse{}, err
	}

	// Transition transfer to PROCESSED.
	if statusErr := s.transfers.UpdateStatus(ctx, tx, transfer.ID, domain.TransferStatusProcessed); statusErr != nil {
		err = fmt.Errorf("svc: update transfer status: %w", statusErr)

		return TransferResponse{}, err
	}

	s.log.Info("transfer state transition",
		"layer", "svc",
		"idempotencyKey", req.IdempotencyKey,
		"transferID", transfer.ID,
		"fromWalletID", fromW.ID,
		"toWalletID", toW.ID,
		"amount", req.Amount,
		"from", domain.TransferStatusPending.String(),
		"to", domain.TransferStatusProcessed.String(),
	)

	// Build and cache the response JSON atomically within the transaction.
	resp := TransferResponse{
		TransferID: transfer.ID,
		Status:     domain.TransferStatusProcessed,
		Amount:     req.Amount,
	}

	respJSON, marshalErr := json.Marshal(resp)
	if marshalErr != nil {
		err = fmt.Errorf("svc: marshal response: %w", marshalErr)

		return TransferResponse{}, err
	}

	idemRecord := repository.IdempotencyRecord{
		Key:          req.IdempotencyKey,
		TransferID:   transfer.ID,
		ResponseJSON: string(respJSON),
		StatusCode:   200,
	}

	if idemErr := s.idem.Create(ctx, tx, idemRecord); idemErr != nil {
		err = fmt.Errorf("svc: store idempotency record: %w", idemErr)

		return TransferResponse{}, err
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		err = fmt.Errorf("svc: commit transaction: %w", commitErr)

		return TransferResponse{}, err
	}

	return resp, nil
}

// GetTransfer retrieves a transfer by ID.
func (s *TransferService) GetTransfer(ctx context.Context, id uuid.UUID) (domain.Transfer, error) {
	t, err := s.transfers.GetByID(ctx, s.db, id)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("svc: get transfer: %w", err)
	}

	return t, nil
}

// GetWallet retrieves a wallet by ID.
func (s *TransferService) GetWallet(ctx context.Context, id uuid.UUID) (domain.Wallet, error) {
	w, err := s.wallets.GetByID(ctx, s.db, id)
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("svc: get wallet: %w", err)
	}

	return w, nil
}

// validate performs cheap, stateless input validation.
func (s *TransferService) validate(req TransferRequest) error {
	if req.Amount <= 0 {
		return domain.ErrInvalidAmount
	}

	if req.FromWalletID == req.ToWalletID {
		return domain.ErrSameWallet
	}

	return nil
}

// lockWallets acquires FOR UPDATE locks on both wallets in ascending UUID byte
// order to prevent deadlocks when two concurrent transfers target the same pair.
func (s *TransferService) lockWallets(
	ctx context.Context,
	tx pgx.Tx,
	fromID, toID uuid.UUID,
) (fromW, toW domain.Wallet, err error) {
	first, second := fromID, toID
	if bytes.Compare(toID[:], fromID[:]) < 0 {
		first, second = toID, fromID
	}

	w1, err := s.wallets.GetByIDForUpdate(ctx, tx, first)
	if err != nil {
		s.log.Error("failed to lock wallet", "layer", "svc", "walletID", first, "error", err)

		return domain.Wallet{}, domain.Wallet{}, fmt.Errorf("svc: lock wallet: %w", err)
	}

	w2, err := s.wallets.GetByIDForUpdate(ctx, tx, second)
	if err != nil {
		s.log.Error("failed to lock wallet", "layer", "svc", "walletID", second, "error", err)

		return domain.Wallet{}, domain.Wallet{}, fmt.Errorf("svc: lock wallet: %w", err)
	}

	// Re-assign so fromW and toW match the caller's perspective.
	if first == fromID {
		return w1, w2, nil
	}

	return w2, w1, nil
}

// isUniqueViolation checks whether err is a PostgreSQL unique constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
