package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kingperseocs/golazo-api/internal/domain"
)

// fakeBetHistory responde MyBets con los tickets indicados y guarda lo que recibió.
// Incrusta fakeBetService para heredar su PlaceBet y cumplir la interfaz completa.
type fakeBetHistory struct {
	fakeBetService

	tickets []domain.BetTicket
	hasMore bool

	gotFilter domain.BetListFilter
	gotAfter  *domain.TxCursor
	gotLimit  int
}

func (f *fakeBetHistory) MyBets(ctx context.Context, userID string, filter domain.BetListFilter, after *domain.TxCursor, limit int) ([]domain.BetTicket, bool, error) {
	f.gotFilter, f.gotAfter, f.gotLimit = filter, after, limit
	return f.tickets, f.hasMore, nil
}

func TestBetHandler_MyBets(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	ticket := domain.BetTicket{
		ID: "bet-1", StakeCents: 2_000, TotalOddsMilli: 2_100, PotentialReturnCents: 4_200,
		Status: domain.BetPending, PlacedAt: time.Date(2026, 11, 23, 22, 0, 0, 0, time.UTC),
		Selections: []domain.TicketSelection{{
			SelectionID: "sel-1", HomeTeam: "Alianza Lima", AwayTeam: "Universitario",
			MarketType: "match_result", Code: "home", OddsMilli: 2_100, Result: "pending",
		}},
	}
	validCursor, err := encodeCursor(domain.TxCursor{CreatedAt: ticket.PlacedAt, ID: ticket.ID})
	if err != nil {
		t.Fatalf("codificar cursor: %v", err)
	}

	tests := []struct {
		name       string
		query      string
		hasMore    bool
		wantStatus int
		wantFilter domain.BetListFilter
		wantAfter  bool
		wantNext   bool
		wantCode   string
	}{
		{"todas, primera página", "", false, http.StatusOK, domain.BetsAll, false, false, ""},
		{"solo abiertas", "?status=pending", false, http.StatusOK, domain.BetsPending, false, false, ""},
		{"solo liquidadas", "?status=settled", false, http.StatusOK, domain.BetsSettled, false, false, ""},
		{"hay otra página", "", true, http.StatusOK, domain.BetsAll, false, true, ""},
		{"con cursor", "?cursor=" + validCursor, false, http.StatusOK, domain.BetsAll, true, false, ""},
		{"status inválido", "?status=ganadas", false, http.StatusBadRequest, "", false, false, "invalid_status"},
		{"cursor inválido", "?cursor=basura!!", false, http.StatusBadRequest, "", false, false, "invalid_cursor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeBetHistory{tickets: []domain.BetTicket{ticket}, hasMore: tt.hasMore}
			h := NewBetHandler(svc, discard)

			req := httptest.NewRequest(http.MethodGet, "/bets/me"+tt.query, nil)
			req = req.WithContext(context.WithValue(req.Context(), userIDKey{}, "ana-id"))
			rec := httptest.NewRecorder()
			h.MyBets(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, se esperaba %d", rec.Code, tt.wantStatus)
			}
			if tt.wantCode != "" {
				var body errorResponse
				if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
					t.Fatalf("leer respuesta: %v", err)
				}
				if body.Error.Code != tt.wantCode {
					t.Errorf("code = %q, se esperaba %q", body.Error.Code, tt.wantCode)
				}
				return
			}

			if svc.gotFilter != tt.wantFilter {
				t.Errorf("filtro = %q, se esperaba %q", svc.gotFilter, tt.wantFilter)
			}
			if (svc.gotAfter != nil) != tt.wantAfter {
				t.Errorf("cursor recibido = %v, se esperaba cursor: %v", svc.gotAfter, tt.wantAfter)
			}

			var page betsPage
			if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
				t.Fatalf("leer respuesta: %v", err)
			}
			if (page.NextCursor != nil) != tt.wantNext {
				t.Errorf("next_cursor = %v, se esperaba cursor: %v", page.NextCursor, tt.wantNext)
			}
			if len(page.Data) != 1 || page.Data[0].ID != "bet-1" {
				t.Fatalf("data = %+v, se esperaba la apuesta bet-1", page.Data)
			}
			if sel := page.Data[0].Selections[0]; sel.HomeTeam != "Alianza Lima" || sel.Code != "home" || sel.OddsMilli != 2_100 {
				t.Errorf("selección = %+v, se esperaba Alianza Lima, home, 2100", sel)
			}
		})
	}
}
