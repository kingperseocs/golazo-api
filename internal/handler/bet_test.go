package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kingperseocs/golazo-api/internal/domain"
)

// fakeBetService guarda lo que recibió y devuelve lo que se le indique.
type fakeBetService struct {
	created bool
	err     error

	gotUserID string
	gotStake  int64
	gotPicks  []domain.BetPick
	gotKey    string
}

func (f *fakeBetService) PlaceBet(ctx context.Context, userID string, stakeCents int64, picks []domain.BetPick, key string) (domain.Bet, bool, error) {
	f.gotUserID, f.gotStake, f.gotPicks, f.gotKey = userID, stakeCents, picks, key
	if f.err != nil {
		return domain.Bet{}, false, f.err
	}
	return domain.Bet{
		ID:                   "bet-1",
		StakeCents:           stakeCents,
		TotalOddsMilli:       2_100,
		PotentialReturnCents: 4_200,
		Status:               domain.BetPending,
		Selections:           []domain.BetSelection{{SelectionID: "sel-1", OddsMilli: 2_100}},
	}, f.created, nil
}

func TestBetHandler_PlaceBet(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	validBody := `{"stake_cents":2000,"selections":[{"selection_id":"sel-1","odds_milli":2100}]}`
	oddsChanged := &domain.OddsChangedError{Changes: []domain.OddsChange{{SelectionID: "sel-1", OddsMilli: 1_950}}}

	tests := []struct {
		name       string
		body       string
		created    bool
		err        error
		wantStatus int
		wantCode   string
	}{
		{"apuesta nueva", validBody, true, nil, http.StatusCreated, ""},
		{"reintento con la misma clave", validBody, false, nil, http.StatusOK, ""},
		{"la cuota cambió", validBody, false, oddsChanged, http.StatusConflict, "odds_changed"},
		{"mercado suspendido", validBody, false, domain.ErrSelectionClosed, http.StatusConflict, "selection_closed"},
		{"saldo insuficiente", validBody, false, domain.ErrInsufficientFunds, http.StatusUnprocessableEntity, "insufficient_funds"},
		{"selección inexistente", validBody, false, domain.ErrSelectionNotFound, http.StatusUnprocessableEntity, "selection_not_found"},
		{"JSON roto", `{"stake_cents":`, false, nil, http.StatusBadRequest, "invalid_json"},
		{"campo desconocido (user_id)", `{"stake_cents":2000,"user_id":"otro"}`, false, nil, http.StatusBadRequest, "invalid_json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeBetService{created: tt.created, err: tt.err}
			h := NewBetHandler(svc, discard)

			req := httptest.NewRequest(http.MethodPost, "/bets", strings.NewReader(tt.body))
			req.Header.Set("Idempotency-Key", "apuesta-1")
			req = req.WithContext(context.WithValue(req.Context(), userIDKey{}, "ana-id"))
			rec := httptest.NewRecorder()

			h.PlaceBet(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, se esperaba %d", rec.Code, tt.wantStatus)
			}
			if tt.wantCode == "" {
				return
			}
			var body errorResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("leer respuesta: %v", err)
			}
			if body.Error.Code != tt.wantCode {
				t.Errorf("code = %q, se esperaba %q", body.Error.Code, tt.wantCode)
			}
		})
	}
}

func TestBetHandler_PlaceBet_PasaLosDatos(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &fakeBetService{created: true}
	h := NewBetHandler(svc, discard)

	body := `{"stake_cents":2000,"selections":[{"selection_id":"sel-1","odds_milli":2100},{"selection_id":"sel-2","odds_milli":3200}]}`
	req := httptest.NewRequest(http.MethodPost, "/bets", strings.NewReader(body))
	req.Header.Set("Idempotency-Key", "apuesta-1")
	req = req.WithContext(context.WithValue(req.Context(), userIDKey{}, "ana-id"))
	h.PlaceBet(httptest.NewRecorder(), req)

	if svc.gotUserID != "ana-id" {
		t.Errorf("userID = %q, se esperaba el del token (ana-id)", svc.gotUserID)
	}
	if svc.gotStake != 2_000 || svc.gotKey != "apuesta-1" {
		t.Errorf("stake = %d, key = %q; se esperaba 2000 y apuesta-1", svc.gotStake, svc.gotKey)
	}
	want := []domain.BetPick{{SelectionID: "sel-1", OddsMilli: 2_100}, {SelectionID: "sel-2", OddsMilli: 3_200}}
	if len(svc.gotPicks) != len(want) {
		t.Fatalf("picks = %+v, se esperaba %+v", svc.gotPicks, want)
	}
	for i := range want {
		if svc.gotPicks[i] != want[i] {
			t.Errorf("pick %d = %+v, se esperaba %+v", i, svc.gotPicks[i], want[i])
		}
	}
}

func TestBetHandler_CuotaCambio_DevuelveLasNuevas(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &fakeBetService{err: &domain.OddsChangedError{Changes: []domain.OddsChange{{SelectionID: "sel-1", OddsMilli: 1_950}}}}
	h := NewBetHandler(svc, discard)

	req := httptest.NewRequest(http.MethodPost, "/bets", strings.NewReader(`{"stake_cents":2000,"selections":[{"selection_id":"sel-1","odds_milli":2100}]}`))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey{}, "ana-id"))
	rec := httptest.NewRecorder()
	h.PlaceBet(rec, req)

	var body oddsChangedResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("leer respuesta: %v", err)
	}
	if len(body.Error.Changes) != 1 || body.Error.Changes[0].OddsMilli != 1_950 {
		t.Errorf("changes = %+v, se esperaba la cuota nueva 1950", body.Error.Changes)
	}
}
