package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/handler"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// ── Test doubles ─────────────────────────────────────────────────────────────

type fakeTransferSvc struct {
	executeResp service.TransferResponse
	executeErr  error
	getTransfer domain.Transfer
	getTransErr error
	getWallet   domain.Wallet
	getWalErr   error
}

func (f *fakeTransferSvc) Execute(_ context.Context, _ service.TransferRequest) (service.TransferResponse, error) {
	return f.executeResp, f.executeErr
}

func (f *fakeTransferSvc) GetTransfer(_ context.Context, _ uuid.UUID) (domain.Transfer, error) {
	return f.getTransfer, f.getTransErr
}

func (f *fakeTransferSvc) GetWallet(_ context.Context, _ uuid.UUID) (domain.Wallet, error) {
	return f.getWallet, f.getWalErr
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newRouter(svc handler.TransferSvc) http.Handler {
	r := chi.NewRouter()
	h := handler.New(svc)
	h.Register(r)

	return r
}

func toJSON(t *testing.T, v any) []byte {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	return b
}

// ── POST /transfers ───────────────────────────────────────────────────────────

func TestHandler_CreateTransfer_201(t *testing.T) {
	t.Parallel()

	transferID := domain.NewID()
	svc := &fakeTransferSvc{
		executeResp: service.TransferResponse{
			TransferID: transferID,
			Status:     domain.TransferStatusProcessed,
			Amount:     100_00,
		},
	}

	body := handler.CreateTransferRequest{
		IdempotencyKey: "abc123",
		FromWalletID:   domain.NewID(),
		ToWalletID:     domain.NewID(),
		Amount:         100_00,
	}

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body)
	}

	var resp handler.CreateTransferResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ID != transferID {
		t.Fatalf("transfer ID mismatch: got %s, want %s", resp.ID, transferID)
	}
}

func TestHandler_CreateTransfer_200_Idempotent(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{
		executeResp: service.TransferResponse{
			TransferID: domain.NewID(),
			Status:     domain.TransferStatusProcessed,
		},
	}

	body := handler.CreateTransferRequest{
		IdempotencyKey: "abc123",
		FromWalletID:   domain.NewID(),
		ToWalletID:     domain.NewID(),
		Amount:         100_00,
	}

	// Simulate idempotent repeat by sending twice.
	for i := range 2 {
		req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(toJSON(t, body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		newRouter(svc).ServeHTTP(rec, req)

		// First call → 201, subsequent calls with same key → 200.
		want := http.StatusCreated
		if i > 0 {
			want = http.StatusOK
		}

		if rec.Code != want {
			t.Fatalf("call %d: expected %d, got %d", i, want, rec.Code)
		}
	}
}

func TestHandler_CreateTransfer_400_MissingIdempotencyKey(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{}
	body := handler.CreateTransferRequest{
		FromWalletID: domain.NewID(),
		ToWalletID:   domain.NewID(),
		Amount:       100_00,
		// IdempotencyKey intentionally omitted
	}

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandler_CreateTransfer_400_InvalidAmount(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{executeErr: domain.ErrInvalidAmount}
	body := handler.CreateTransferRequest{
		IdempotencyKey: "key",
		FromWalletID:   domain.NewID(),
		ToWalletID:     domain.NewID(),
		Amount:         0,
	}

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandler_CreateTransfer_422_InsufficientFunds(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{executeErr: domain.ErrInsufficientFunds}
	body := handler.CreateTransferRequest{
		IdempotencyKey: "key",
		FromWalletID:   domain.NewID(),
		ToWalletID:     domain.NewID(),
		Amount:         999_00,
	}

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rec.Code)
	}
}

func TestHandler_CreateTransfer_404_WalletNotFound(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{executeErr: domain.ErrWalletNotFound}
	body := handler.CreateTransferRequest{
		IdempotencyKey: "key",
		FromWalletID:   uuid.New(),
		ToWalletID:     uuid.New(),
		Amount:         100_00,
	}

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(toJSON(t, body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// ── GET /wallets/{id} ─────────────────────────────────────────────────────────

func TestHandler_GetWallet_200(t *testing.T) {
	t.Parallel()

	walletID := domain.NewID()
	svc := &fakeTransferSvc{
		getWallet: domain.Wallet{ID: walletID, Balance: 250_00},
	}

	req := httptest.NewRequest(http.MethodGet, "/wallets/"+walletID.String(), nil)
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}
}

func TestHandler_GetWallet_404(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{getWalErr: domain.ErrWalletNotFound}

	req := httptest.NewRequest(http.MethodGet, "/wallets/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// ── GET /transfers/{id} ───────────────────────────────────────────────────────

func TestHandler_GetTransfer_200(t *testing.T) {
	t.Parallel()

	transferID := domain.NewID()
	svc := &fakeTransferSvc{
		getTransfer: domain.Transfer{ID: transferID, Status: domain.TransferStatusProcessed},
	}

	req := httptest.NewRequest(http.MethodGet, "/transfers/"+transferID.String(), nil)
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}
}

func TestHandler_GetTransfer_404(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{getTransErr: domain.ErrTransferNotFound}

	req := httptest.NewRequest(http.MethodGet, "/transfers/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandler_CreateTransfer_400_BadJSON(t *testing.T) {
	t.Parallel()

	svc := &fakeTransferSvc{}

	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader([]byte(`{bad json}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", rec.Code)
	}
}
