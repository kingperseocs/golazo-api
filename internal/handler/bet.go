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

// BetService es lo que el handler necesita del servicio de apuestas.
type BetService interface {
	PlaceBet(ctx context.Context, userID string, stakeCents int64, picks []domain.BetPick, key string) (domain.Bet, bool, error)
	MyBets(ctx context.Context, userID string, filter domain.BetListFilter, after *domain.TxCursor, limit int) ([]domain.BetTicket, bool, error)
}

// BetHandler atiende las rutas HTTP de apuestas.
type BetHandler struct {
	svc    BetService
	logger *slog.Logger
}

// NewBetHandler crea el handler con sus dependencias.
func NewBetHandler(svc BetService, logger *slog.Logger) *BetHandler {
	return &BetHandler{svc: svc, logger: logger}
}

type placeBetRequest struct {
	StakeCents int64           `json:"stake_cents"`
	Selections []selectionOdds `json:"selections"`
}

// selectionOdds es una selección con su cuota. Se usa en el request
// (la cuota que vio el usuario) y en las respuestas (la cuota guardada).
type selectionOdds struct {
	SelectionID string `json:"selection_id"`
	OddsMilli   int64  `json:"odds_milli"`
}

type betResponse struct {
	ID                   string          `json:"id"`
	StakeCents           int64           `json:"stake_cents"`
	TotalOddsMilli       int64           `json:"total_odds_milli"`
	PotentialReturnCents int64           `json:"potential_return_cents"`
	Status               string          `json:"status"`
	PlacedAt             time.Time       `json:"placed_at"`
	Selections           []selectionOdds `json:"selections"`
}

// oddsChangedDetail es el error de siempre (code y message) más las cuotas nuevas.
// Al incrustar errorDetail, sus campos salen en el JSON al mismo nivel que changes.
type oddsChangedDetail struct {
	errorDetail
	Changes []selectionOdds `json:"changes"`
}

type oddsChangedResponse struct {
	Error oddsChangedDetail `json:"error"`
}

// PlaceBet atiende POST /bets.
func (h *BetHandler) PlaceBet(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		h.logger.Error("apuesta sin usuario autenticado en el contexto")
		writeError(w, h.logger, http.StatusInternalServerError, "internal_error", "ocurrió un error interno, intenta más tarde")
		return
	}

	key := r.Header.Get("Idempotency-Key")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req placeBetRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.logger.Warn("JSON inválido en apuesta", "error", err)
		writeError(w, h.logger, http.StatusBadRequest, "invalid_json", "el cuerpo debe ser un JSON válido")
		return
	}

	picks := make([]domain.BetPick, 0, len(req.Selections))
	for _, s := range req.Selections {
		picks = append(picks, domain.BetPick{SelectionID: s.SelectionID, OddsMilli: s.OddsMilli})
	}

	bet, created, err := h.svc.PlaceBet(r.Context(), userID, req.StakeCents, picks, key)
	if err != nil {
		h.writeBetError(w, err)
		return
	}

	status := http.StatusCreated
	if !created {
		status = http.StatusOK // reintento: la misma apuesta, sin cobrar de nuevo
	}

	resp := betResponse{
		ID:                   bet.ID,
		StakeCents:           bet.StakeCents,
		TotalOddsMilli:       bet.TotalOddsMilli,
		PotentialReturnCents: bet.PotentialReturnCents,
		Status:               string(bet.Status),
		PlacedAt:             bet.PlacedAt,
		Selections:           make([]selectionOdds, 0, len(bet.Selections)),
	}
	for _, s := range bet.Selections {
		resp.Selections = append(resp.Selections, selectionOdds{SelectionID: s.SelectionID, OddsMilli: s.OddsMilli})
	}
	writeJSON(w, h.logger, status, resp)
}

// writeBetError traduce los errores de una apuesta a respuestas HTTP.
func (h *BetHandler) writeBetError(w http.ResponseWriter, err error) {
	// La cuota cambió: un error con datos, así que se busca con errors.As.
	var oddsErr *domain.OddsChangedError
	if errors.As(err, &oddsErr) {
		changes := make([]selectionOdds, 0, len(oddsErr.Changes))
		for _, c := range oddsErr.Changes {
			changes = append(changes, selectionOdds{SelectionID: c.SelectionID, OddsMilli: c.OddsMilli})
		}
		writeJSON(w, h.logger, http.StatusConflict, oddsChangedResponse{
			Error: oddsChangedDetail{
				errorDetail: errorDetail{Code: "odds_changed", Message: err.Error()},
				Changes:     changes,
			},
		})
		return
	}

	switch {
	// 409: choca con el estado actual del servidor.
	case errors.Is(err, domain.ErrSelectionClosed):
		writeError(w, h.logger, http.StatusConflict, "selection_closed", err.Error())
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeError(w, h.logger, http.StatusConflict, "idempotency_conflict", err.Error())

	// 422: el request está bien formado, pero rompe una regla del negocio.
	case errors.Is(err, domain.ErrInsufficientFunds):
		writeError(w, h.logger, http.StatusUnprocessableEntity, "insufficient_funds", err.Error())
	case errors.Is(err, domain.ErrSelectionNotFound):
		writeError(w, h.logger, http.StatusUnprocessableEntity, "selection_not_found", err.Error())
	case errors.Is(err, domain.ErrSameEventInCombo):
		writeError(w, h.logger, http.StatusUnprocessableEntity, "same_event_in_combo", err.Error())
	case errors.Is(err, domain.ErrNoSelections):
		writeError(w, h.logger, http.StatusUnprocessableEntity, "no_selections", err.Error())
	case errors.Is(err, domain.ErrTooManySelections):
		writeError(w, h.logger, http.StatusUnprocessableEntity, "too_many_selections", err.Error())
	case errors.Is(err, domain.ErrStakeTooLow), errors.Is(err, domain.ErrStakeTooHigh):
		writeError(w, h.logger, http.StatusUnprocessableEntity, "invalid_stake", err.Error())
	case errors.Is(err, domain.ErrInvalidOdds):
		writeError(w, h.logger, http.StatusUnprocessableEntity, "invalid_odds", err.Error())
	case errors.Is(err, domain.ErrOddsTooHigh):
		writeError(w, h.logger, http.StatusUnprocessableEntity, "odds_too_high", err.Error())
	case errors.Is(err, domain.ErrInvalidIdempotency):
		writeError(w, h.logger, http.StatusUnprocessableEntity, "invalid_idempotency_key", err.Error())

	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, h.logger, http.StatusNotFound, "wallet_not_found", err.Error())
	default:
		h.logger.Error("apuesta falló", "error", err)
		writeError(w, h.logger, http.StatusInternalServerError, "internal_error", "ocurrió un error interno, intenta más tarde")
	}
}
