package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kingperseocs/golazo-api/internal/domain"
)

// fakeEventService devuelve los partidos indicados y guarda el limit que recibió.
type fakeEventService struct {
	events   []domain.Event
	err      error
	gotLimit int
}

func (f *fakeEventService) ListUpcoming(ctx context.Context, limit int) ([]domain.Event, error) {
	f.gotLimit = limit
	return f.events, f.err
}

func TestEventHandler_List(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	clasico := domain.Event{
		ID: "ev-1", HomeTeam: "Alianza Lima", AwayTeam: "Universitario",
		Markets: []domain.Market{{
			ID: "mk-1", Type: "match_result", Status: "open",
			Selections: []domain.Selection{
				{ID: "sel-1", Code: "home", OddsMilli: 2_100},
				{ID: "sel-2", Code: "draw", OddsMilli: 3_200},
				{ID: "sel-3", Code: "away", OddsMilli: 3_500},
			},
		}},
	}

	tests := []struct {
		name       string
		query      string
		err        error
		wantStatus int
		wantLimit  int
		wantCode   string
	}{
		{"sin limit usa 20", "", nil, http.StatusOK, 20, ""},
		{"limit mayor que el máximo", "?limit=500", nil, http.StatusOK, 100, ""},
		{"limit inválido", "?limit=abc", nil, http.StatusBadRequest, 0, "invalid_limit"},
		{"falla la base de datos", "", errors.New("se cayó la conexión"), http.StatusInternalServerError, 20, "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeEventService{events: []domain.Event{clasico}, err: tt.err}
			h := NewEventHandler(svc, discard)

			// Sin token: la ruta es pública.
			req := httptest.NewRequest(http.MethodGet, "/events"+tt.query, nil)
			rec := httptest.NewRecorder()
			h.List(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, se esperaba %d", rec.Code, tt.wantStatus)
			}
			if svc.gotLimit != tt.wantLimit {
				t.Errorf("limit = %d, se esperaba %d", svc.gotLimit, tt.wantLimit)
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

			var page eventsPage
			if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
				t.Fatalf("leer respuesta: %v", err)
			}
			if page.Data[0].ID != "ev-1" {
				t.Errorf("id del partido = %q, se esperaba ev-1", page.Data[0].ID)
			}
			if len(page.Data) != 1 || len(page.Data[0].Markets[0].Selections) != 3 {
				t.Fatalf("respuesta = %+v, se esperaba 1 partido con 3 selecciones", page)
			}
			if got := page.Data[0].Markets[0].Selections[0]; got.ID != "sel-1" || got.OddsMilli != 2_100 {
				t.Errorf("primera selección = %+v, se esperaba sel-1 a 2100", got)
			}
		})
	}
}
