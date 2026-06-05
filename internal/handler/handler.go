package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// TransferSvc is the service interface the handler depends on.
// Keeping it here (not in the service package) avoids an import cycle and
// allows the handler to be tested with any conforming test double.
type TransferSvc interface {
	Execute(ctx context.Context, req service.TransferRequest) (service.TransferResponse, error)
	GetTransfer(ctx context.Context, id uuid.UUID) (domain.Transfer, error)
	GetWallet(ctx context.Context, id uuid.UUID) (domain.Wallet, error)
}

// Handler holds HTTP handler methods and their dependencies.
type Handler struct {
	svc TransferSvc
}

// New constructs a Handler.
func New(svc TransferSvc) *Handler {
	return &Handler{svc: svc}
}

// Register mounts all routes onto the supplied router.
func (h *Handler) Register(r chi.Router) {
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Post("/transfers", h.createTransfer)
	r.Get("/transfers/{id}", h.getTransfer)
	r.Get("/wallets/{id}", h.getWallet)
}

// ── Request / Response types ──────────────────────────────────────────────────

// CreateTransferRequest is the JSON body for POST /transfers.
type CreateTransferRequest struct {
	IdempotencyKey string        `json:"idempotencyKey"`
	FromWalletID   uuid.UUID     `json:"fromWalletId"`
	ToWalletID     uuid.UUID     `json:"toWalletId"`
	Amount         domain.Amount `json:"amount"`
}

// CreateTransferResponse is the JSON body returned on 201 / 200.
type CreateTransferResponse struct {
	ID     uuid.UUID              `json:"id"`
	Status domain.TransferStatus  `json:"status"`
	Amount domain.Amount          `json:"amount"`
}

// WalletResponse is the JSON body returned by GET /wallets/{id}.
type WalletResponse struct {
	ID      uuid.UUID     `json:"id"`
	Balance domain.Amount `json:"balance"`
}

// TransferResponse is the JSON body returned by GET /transfers/{id}.
type TransferResponse struct {
	ID     uuid.UUID              `json:"id"`
	Status domain.TransferStatus  `json:"status"`
	Amount domain.Amount          `json:"amount"`
}

// errorResponse is the JSON body for all error responses.
type errorResponse struct {
	RequestID string `json:"requestId,omitempty"`
	Error     string `json:"error"`
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// createTransfer handles POST /transfers.
func (h *Handler) createTransfer(w http.ResponseWriter, r *http.Request) {
	var body CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON body")

		return
	}

	if body.IdempotencyKey == "" {
		writeError(w, r, http.StatusBadRequest, "idempotencyKey is required")

		return
	}

	if len(body.IdempotencyKey) > service.MaxIdempotencyKeyLength {
		writeError(w, r, http.StatusBadRequest, "idempotencyKey exceeds maximum length")

		return
	}

	req := service.TransferRequest{
		IdempotencyKey: body.IdempotencyKey,
		FromWalletID:   body.FromWalletID,
		ToWalletID:     body.ToWalletID,
		Amount:         body.Amount,
	}

	resp, err := h.svc.Execute(r.Context(), req)
	if err != nil {
		slog.Error("create transfer failed", "layer", "handler", "error", err,
			"idempotencyKey", body.IdempotencyKey)
		writeServiceError(w, r, err)

		return
	}

	// Return 200 for idempotent replays, 201 for freshly created transfers.
	statusCode := http.StatusCreated
	if resp.FromCache {
		statusCode = http.StatusOK
	}

	writeJSON(w, statusCode, CreateTransferResponse{
		ID:     resp.TransferID,
		Status: resp.Status,
		Amount: resp.Amount,
	})
}

// getTransfer handles GET /transfers/{id}.
func (h *Handler) getTransfer(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid transfer ID")

		return
	}

	t, err := h.svc.GetTransfer(r.Context(), id)
	if err != nil {
		slog.Error("get transfer failed", "layer", "handler", "error", err, "transferID", id)
		writeServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, TransferResponse{
		ID:     t.ID,
		Status: t.Status,
		Amount: t.Amount,
	})
}

// getWallet handles GET /wallets/{id}.
func (h *Handler) getWallet(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid wallet ID")

		return
	}

	wallet, err := h.svc.GetWallet(r.Context(), id)
	if err != nil {
		slog.Error("get wallet failed", "layer", "handler", "error", err, "walletID", id)
		writeServiceError(w, r, err)

		return
	}

	writeJSON(w, http.StatusOK, WalletResponse{
		ID:      wallet.ID,
		Balance: wallet.Balance,
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrWalletNotFound),
		errors.Is(err, domain.ErrTransferNotFound):
		writeError(w, r, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrInsufficientFunds):
		writeError(w, r, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet):
		writeError(w, r, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrDuplicateIdempotencyKey):
		writeError(w, r, http.StatusConflict, err.Error())
	default:
		writeError(w, r, http.StatusInternalServerError, "internal server error")
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response failed", "layer", "handler", "error", err)
	}
}

// writeError writes a JSON error response including the chi request ID for traceability.
func writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	writeJSON(w, status, errorResponse{
		RequestID: middleware.GetReqID(r.Context()),
		Error:     msg,
	})
}
