package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// ── Test doubles ─────────────────────────────────────────────────────────────

// fakeTx is a minimal pgx.Tx test double. All operations are no-ops; tests
// override Commit/Rollback behaviour via callbacks.
type fakeTx struct {
	pgx.Tx
	committed  bool
	rolledBack bool
}

func (f *fakeTx) Commit(_ context.Context) error   { f.committed = true; return nil }
func (f *fakeTx) Rollback(_ context.Context) error { f.rolledBack = true; return nil }
func (f *fakeTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row { return nil }
func (f *fakeTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

// fakeDB is a test double for the pgxpool that can begin transactions.
type fakeDB struct {
	tx *fakeTx
}

func (db *fakeDB) Begin(_ context.Context) (pgx.Tx, error) {
	db.tx = &fakeTx{}

	return db.tx, nil
}

func (db *fakeDB) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row { return nil }
func (db *fakeDB) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

// fakeWalletRepo lets individual tests control what each method returns.
type fakeWalletRepo struct {
	wallets      map[uuid.UUID]domain.Wallet
	updateErr    error
	getForUpdate func(id uuid.UUID) (domain.Wallet, error)
}

func (r *fakeWalletRepo) Create(_ context.Context, _ repository.Querier, w domain.Wallet) error {
	if r.wallets == nil {
		r.wallets = make(map[uuid.UUID]domain.Wallet)
	}

	r.wallets[w.ID] = w

	return nil
}

func (r *fakeWalletRepo) GetByID(_ context.Context, _ repository.Querier, id uuid.UUID) (domain.Wallet, error) {
	if w, ok := r.wallets[id]; ok {
		return w, nil
	}

	return domain.Wallet{}, domain.ErrWalletNotFound
}

func (r *fakeWalletRepo) GetByIDForUpdate(_ context.Context, _ pgx.Tx, id uuid.UUID) (domain.Wallet, error) {
	if r.getForUpdate != nil {
		return r.getForUpdate(id)
	}

	return r.GetByID(context.Background(), nil, id)
}

func (r *fakeWalletRepo) UpdateBalance(_ context.Context, _ pgx.Tx, id uuid.UUID, newBalance domain.Amount) error {
	if r.updateErr != nil {
		return r.updateErr
	}

	if w, ok := r.wallets[id]; ok {
		w.Balance = newBalance
		r.wallets[id] = w
	}

	return nil
}

// fakeTransferRepo tracks created transfers and allows UpdateStatus override.
type fakeTransferRepo struct {
	transfers    map[uuid.UUID]domain.Transfer
	updateErr    error
}

func (r *fakeTransferRepo) Create(_ context.Context, _ repository.Querier, t domain.Transfer) error {
	if r.transfers == nil {
		r.transfers = make(map[uuid.UUID]domain.Transfer)
	}

	r.transfers[t.ID] = t

	return nil
}

func (r *fakeTransferRepo) GetByID(_ context.Context, _ repository.Querier, id uuid.UUID) (domain.Transfer, error) {
	if t, ok := r.transfers[id]; ok {
		return t, nil
	}

	return domain.Transfer{}, domain.ErrTransferNotFound
}

func (r *fakeTransferRepo) UpdateStatus(_ context.Context, _ pgx.Tx, id uuid.UUID, status domain.TransferStatus) error {
	if r.updateErr != nil {
		return r.updateErr
	}

	if t, ok := r.transfers[id]; ok {
		t.Status = status
		r.transfers[id] = t
	}

	return nil
}

// fakeLedgerRepo records the entries it received.
type fakeLedgerRepo struct {
	entries []domain.LedgerEntry
}

func (r *fakeLedgerRepo) CreateEntries(_ context.Context, _ pgx.Tx, debit, credit domain.LedgerEntry) error {
	r.entries = append(r.entries, debit, credit)

	return nil
}

// fakeIdempotencyRepo stores records in memory.
type fakeIdempotencyRepo struct {
	records map[string]repository.IdempotencyRecord
}

func (r *fakeIdempotencyRepo) Get(_ context.Context, _ repository.Querier, key string) (repository.IdempotencyRecord, error) {
	if rec, ok := r.records[key]; ok {
		return rec, nil
	}

	return repository.IdempotencyRecord{}, domain.ErrTransferNotFound
}

func (r *fakeIdempotencyRepo) Create(_ context.Context, _ pgx.Tx, rec repository.IdempotencyRecord) error {
	if r.records == nil {
		r.records = make(map[string]repository.IdempotencyRecord)
	}

	r.records[rec.Key] = rec

	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newServiceWithFakes() (
	*service.TransferService,
	*fakeDB,
	*fakeWalletRepo,
	*fakeTransferRepo,
	*fakeLedgerRepo,
	*fakeIdempotencyRepo,
) {
	db := &fakeDB{}
	wallets := &fakeWalletRepo{}
	transfers := &fakeTransferRepo{}
	ledger := &fakeLedgerRepo{}
	idem := &fakeIdempotencyRepo{}

	// Discard logs during tests.
	svc := service.NewTransferService(db, wallets, transfers, ledger, idem,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	return svc, db, wallets, transfers, ledger, idem
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestTransferService_Execute_HappyPath(t *testing.T) {
	t.Parallel()

	svc, _, wallets, transferRepo, ledger, idem := newServiceWithFakes()
	ctx := context.Background()

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()

	wallets.wallets = map[uuid.UUID]domain.Wallet{from.ID: from, to.ID: to}

	req := service.TransferRequest{
		IdempotencyKey: "happy-001",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100_00,
	}

	resp, err := svc.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Transfer must be PROCESSED.
	if resp.Status != domain.TransferStatusProcessed {
		t.Fatalf("expected PROCESSED, got %s", resp.Status)
	}

	// Exactly two ledger entries.
	if len(ledger.entries) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(ledger.entries))
	}

	var debitEntry, creditEntry domain.LedgerEntry
	for _, e := range ledger.entries {
		switch e.Type {
		case domain.EntryTypeDebit:
			debitEntry = e
		case domain.EntryTypeCredit:
			creditEntry = e
		}
	}

	if debitEntry.WalletID != from.ID {
		t.Fatal("debit must be from source wallet")
	}

	if creditEntry.WalletID != to.ID {
		t.Fatal("credit must be to destination wallet")
	}

	if debitEntry.Amount != req.Amount || creditEntry.Amount != req.Amount {
		t.Fatal("ledger entry amounts must match the transfer amount")
	}

	// Idempotency record must be stored.
	if _, err := idem.Get(ctx, nil, req.IdempotencyKey); err != nil {
		t.Fatal("idempotency record must be stored after successful transfer")
	}

	// Transfer must exist in repo.
	tr, err := transferRepo.GetByID(ctx, nil, resp.TransferID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	if tr.Status != domain.TransferStatusProcessed {
		t.Fatalf("stored transfer status: got %s, want PROCESSED", tr.Status)
	}
}

func TestTransferService_Execute_InsufficientFunds(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, _ := newServiceWithFakes()
	ctx := context.Background()

	from := domain.NewWallet()
	from.Balance = 50_00 // only 50
	to := domain.NewWallet()

	wallets.wallets = map[uuid.UUID]domain.Wallet{from.ID: from, to.ID: to}

	req := service.TransferRequest{
		IdempotencyKey: "insuf-001",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100_00, // requesting 100
	}

	_, err := svc.Execute(ctx, req)
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}
}

func TestTransferService_Execute_Idempotent(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, _ := newServiceWithFakes()
	ctx := context.Background()

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()

	wallets.wallets = map[uuid.UUID]domain.Wallet{from.ID: from, to.ID: to}

	req := service.TransferRequest{
		IdempotencyKey: "idem-svc-001",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100_00,
	}

	// First call — should process.
	resp1, err := svc.Execute(ctx, req)
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}

	// Second call with same key — must return identical response without re-running.
	resp2, err := svc.Execute(ctx, req)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	if resp1.TransferID != resp2.TransferID {
		t.Fatal("idempotent repeat must return the same transfer ID")
	}
}

func TestTransferService_Execute_SameWallet(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, _ := newServiceWithFakes()
	ctx := context.Background()

	w := domain.NewWallet()
	w.Balance = 500_00

	wallets.wallets = map[uuid.UUID]domain.Wallet{w.ID: w}

	req := service.TransferRequest{
		IdempotencyKey: "same-wallet-001",
		FromWalletID:   w.ID,
		ToWalletID:     w.ID, // same!
		Amount:         100_00,
	}

	_, err := svc.Execute(ctx, req)
	if !errors.Is(err, domain.ErrSameWallet) {
		t.Fatalf("expected ErrSameWallet, got %v", err)
	}
}

func TestTransferService_Execute_InvalidAmount(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, _ := newServiceWithFakes()
	ctx := context.Background()

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()

	wallets.wallets = map[uuid.UUID]domain.Wallet{from.ID: from, to.ID: to}

	cases := []domain.Amount{0, -1}

	for _, amount := range cases {
		req := service.TransferRequest{
			IdempotencyKey: "invalid-amount",
			FromWalletID:   from.ID,
			ToWalletID:     to.ID,
			Amount:         amount,
		}

		_, err := svc.Execute(ctx, req)
		if !errors.Is(err, domain.ErrInvalidAmount) {
			t.Fatalf("amount %d: expected ErrInvalidAmount, got %v", amount, err)
		}
	}
}

func TestTransferService_Execute_WalletNotFound(t *testing.T) {
	t.Parallel()

	svc, _, _, _, _, _ := newServiceWithFakes()
	ctx := context.Background()

	req := service.TransferRequest{
		IdempotencyKey: "notfound-001",
		FromWalletID:   domain.NewID(),
		ToWalletID:     domain.NewID(),
		Amount:         100_00,
	}

	_, err := svc.Execute(ctx, req)
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("expected ErrWalletNotFound, got %v", err)
	}
}

func TestTransferService_Execute_IdempotentResponse_JSON(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, idem := newServiceWithFakes()
	ctx := context.Background()

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()
	wallets.wallets = map[uuid.UUID]domain.Wallet{from.ID: from, to.ID: to}

	req := service.TransferRequest{
		IdempotencyKey: "json-idem-001",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         50_00,
	}

	// First call to populate the idempotency cache.
	resp1, err := svc.Execute(ctx, req)
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}

	// Verify the cached JSON decodes back to the same response.
	rec, err := idem.Get(ctx, nil, req.IdempotencyKey)
	if err != nil {
		t.Fatalf("Get idempotency record: %v", err)
	}

	var decoded service.TransferResponse
	if err := json.Unmarshal([]byte(rec.ResponseJSON), &decoded); err != nil {
		t.Fatalf("unmarshal cached JSON: %v", err)
	}

	if decoded.TransferID != resp1.TransferID {
		t.Fatal("cached JSON transfer ID must match original response")
	}
}
