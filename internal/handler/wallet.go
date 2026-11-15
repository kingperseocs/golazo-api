package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/kingperseocs/golazo-api/internal/domain"
)

// WalletService es lo que el handler necesita del servicio de billeteras.
type WalletService interface {
	Deposit(ctx context.Context, userID, walletID string, amountCents int64, key string) (domain.WalletTransaction, bool, error)
	Withdraw(ctx context.Context, userID, walletID string, amountCents int64, key string) (domain.WalletTransaction, bool, error)
	MyWallet(ctx context.Context, userID string) (domain.Wallet, error)
}

// movementFunc es la forma común de Deposit y Withdraw.
type movementFunc func(ctx context.Context, userID, walletID string, amountCents int64, key string) (domain.WalletTransaction, bool, error)

// WalletHandler atiende las rutas HTTP de billeteras.
type WalletHandler struct {
	svc    WalletService
	logger *slog.Logger
}

// NewWalletHandler crea el handler con sus dependencias.
func NewWalletHandler(svc WalletService, logger *slog.Logger) *WalletHandler {
	return &WalletHandler{svc: svc, logger: logger}
}

type movementRequest struct {
	AmountCents int64 `json:"amount_cents"`
}

type transactionResponse struct {
	ID                string    `json:"id"`
	WalletID          string    `json:"wallet_id"`
	Type              string    `json:"type"`
	AmountCents       int64     `json:"amount_cents"`
	BalanceAfterCents int64     `json:"balance_after_cents"`
	CreatedAt         time.Time `json:"created_at"`
}

// Deposit atiende POST /wallets/{id}/deposits.
func (h *WalletHandler) Deposit(w http.ResponseWriter, r *http.Request) {
	h.handleMovement(w, r, h.svc.Deposit)
}

// Withdraw atiende POST /wallets/{id}/withdrawals.
func (h *WalletHandler) Withdraw(w http.ResponseWriter, r *http.Request) {
	h.handleMovement(w, r, h.svc.Withdraw)
}

// handleMovement hace todo lo común a depósitos y retiros: leer el usuario,
// el id de la billetera, la clave y el JSON; llamar a la operación (move)
// y escribir la respuesta.
func (h *WalletHandler) handleMovement(w http.ResponseWriter, r *http.Request, move movementFunc) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		h.logger.Error("movimiento sin usuario autenticado en el contexto")
		writeError(w, h.logger, http.StatusInternalServerError, "internal_error", "ocurrió un error interno, intenta más tarde")
		return
	}

	walletID := r.PathValue("id")
	key := r.Header.Get("Idempotency-Key")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req movementRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.logger.Warn("JSON inválido en movimiento", "error", err)
		writeError(w, h.logger, http.StatusBadRequest, "invalid_json", "el cuerpo debe ser un JSON válido")
		return
	}

	tx, created, err := move(r.Context(), userID, walletID, req.AmountCents, key)
	if err != nil {
		h.writeMovementError(w, err)
		return
	}

	status := http.StatusCreated
	if !created {
		status = http.StatusOK // reintento: mismo resultado, sin aplicar de nuevo
	}

	writeJSON(w, h.logger, status, transactionResponse{
		ID:                tx.ID,
		WalletID:          tx.WalletID,
		Type:              string(tx.Type),
		AmountCents:       tx.AmountCents,
		BalanceAfterCents: tx.BalanceAfterCents,
		CreatedAt:         tx.CreatedAt,
	})
}

// writeMovementError traduce los errores de depósitos y retiros a respuestas HTTP.
func (h *WalletHandler) writeMovementError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, h.logger, http.StatusNotFound, "wallet_not_found", err.Error())
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeError(w, h.logger, http.StatusConflict, "idempotency_conflict", err.Error())
	case errors.Is(err, domain.ErrInvalidAmount):
		writeError(w, h.logger, http.StatusUnprocessableEntity, "invalid_amount", err.Error())
	case errors.Is(err, domain.ErrInsufficientFunds):
		writeError(w, h.logger, http.StatusUnprocessableEntity, "insufficient_funds", err.Error())
	case errors.Is(err, domain.ErrDepositLimit):
		writeError(w, h.logger, http.StatusUnprocessableEntity, "deposit_limit", err.Error())
	case errors.Is(err, domain.ErrInvalidIdempotency):
		writeError(w, h.logger, http.StatusUnprocessableEntity, "invalid_idempotency_key", err.Error())
	default:
		h.logger.Error("movimiento falló", "error", err)
		writeError(w, h.logger, http.StatusInternalServerError, "internal_error", "ocurrió un error interno, intenta más tarde")
	}
}

type walletResponse struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Currency     string    `json:"currency"`
	BalanceCents int64     `json:"balance_cents"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Me atiende GET /wallets/me: la billetera del usuario autenticado.
func (h *WalletHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		h.logger.Error("consulta de billetera sin usuario autenticado en el contexto")
		writeError(w, h.logger, http.StatusInternalServerError, "internal_error", "ocurrió un error interno, intenta más tarde")
		return
	}

	wallet, err := h.svc.MyWallet(r.Context(), userID)
	if errors.Is(err, domain.ErrWalletNotFound) {
		writeError(w, h.logger, http.StatusNotFound, "wallet_not_found", err.Error())
		return
	}
	if err != nil {
		h.logger.Error("consulta de billetera falló", "error", err)
		writeError(w, h.logger, http.StatusInternalServerError, "internal_error", "ocurrió un error interno, intenta más tarde")
		return
	}

	writeJSON(w, h.logger, http.StatusOK, walletResponse{
		ID:           wallet.ID,
		Type:         wallet.Type,
		Currency:     wallet.Currency,
		BalanceCents: wallet.BalanceCents,
		UpdatedAt:    wallet.UpdatedAt,
	})
}
