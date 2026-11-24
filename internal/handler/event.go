package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/kingperseocs/golazo-api/internal/domain"
)

// EventService e slo que el hanlder necesita del catálogo de partidos
type EventService interface {
	ListUpcoming(ctx context.Context, limit int) ([]domain.Event, error)
}

// EventHandler atiende las rutas HTTP del catálogo
type EventHandler struct {
	svc    EventService
	logger *slog.Logger
}

// NewEventHandler crea el hanlder con sus dependencias
func NewEventHandler(svc EventService, logger *slog.Logger) *EventHandler {
	return &EventHandler{svc: svc, logger: logger}
}

type selectionResponse struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	OddsMilli int64  `json:"odds_milli"`
}

type marketResponse struct {
	ID         string              `json:"id"`
	Type       string              `json:"type"`
	Status     string              `json:"status"`
	Selections []selectionResponse `json:"selections"`
}

type eventResponse struct {
	ID       string           `json:"id"`
	HomeTeam string           `json:"home_team"`
	AwayTeam string           `json:"away_team"`
	StartsAt time.Time        `json:"starts_at"`
	Markets  []marketResponse `json:"markets"`
}

type eventsPage struct {
	Data []eventResponse `json:"data"`
}

// List atiende GET /events?limit=20. Es pública: no pide token
func (h *EventHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r)
	if err != nil {
		writeError(w, h.logger, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}

	events, err := h.svc.ListUpcoming(r.Context(), limit)
	if err != nil {
		h.logger.Error("listar partidos falló", "error", err)
		writeError(w, h.logger, http.StatusInternalServerError, "internal_error", "ocurrió un error interno, intenta más tarde")
		return
	}

	page := eventsPage{Data: make([]eventResponse, 0, len(events))}
	for _, e := range events {
		er := eventResponse{
			ID:       e.ID,
			HomeTeam: e.HomeTeam,
			AwayTeam: e.AwayTeam,
			StartsAt: e.StartsAt,
			Markets:  make([]marketResponse, 0, len(e.Markets)),
		}
		for _, m := range e.Markets {
			mr := marketResponse{
				ID:         m.ID,
				Type:       m.Type,
				Status:     m.Status,
				Selections: make([]selectionResponse, 0, len(m.Selections)),
			}
			for _, s := range m.Selections {
				mr.Selections = append(mr.Selections, selectionResponse{ID: s.ID, Code: s.Code, OddsMilli: s.OddsMilli})
			}
			er.Markets = append(er.Markets, mr)
		}
		page.Data = append(page.Data, er)
	}
	writeJSON(w, h.logger, http.StatusOK, page)
}
