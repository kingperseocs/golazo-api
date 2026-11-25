package handler

import (
	"net/http"
	"time"

	"github.com/kingperseocs/golazo-api/internal/domain"
)

type ticketSelectionResponse struct {
	SelectionID string    `json:"selection_id"`
	HomeTeam    string    `json:"home_team"`
	AwayTeam    string    `json:"away_team"`
	StartsAt    time.Time `json:"starts_at"`
	MarketType  string    `json:"market_type"`
	Code        string    `json:"code"`
	OddsMilli   int64     `json:"odds-milli"`
	Result      string    `json:"result"`
}

type ticketResponse struct {
	ID                   string                    `json:"id"`
	StakeCents           int64                     `json:"stake_cents"`
	TotalOddsMilli       int64                     `json:"total_odds_milli"`
	PotentialRetunrCents int64                     `json:"potential_return_cents"`
	Status               string                    `json:"status"`
	PlaceAt              time.Time                 `json:"placed_at"`
	Selections           []ticketSelectionResponse `json:"selections"`
}

type betsPage struct {
	Data       []ticketResponse `json:"data"`
	NextCursor *string          `json:"next_cursor"`
}

// MyBets atiende GET /bets/me?status=pending|settled&limit=20&cursor=...
// aqui se manejan los estados
func (h *BetHandler) MyBets(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		h.logger.Error("historial de apuestas sin usuario autenticado en el contexto")
		writeError(w, h.logger, http.StatusInternalServerError, "internal_error", "ocurrió un error en el servidor, intenta mas tarde")
	}

	// 1. ¿Qué apuestas? (todas, abiertas o liquidadas)
	filter, err := domain.ParseBetListFilter(r.URL.Query().Get("status"))
	if err != nil {
		writeError(w, h.logger, http.StatusBadRequest, "invalid_status", err.Error())
		return
	}

	// 2. ¿Cuántas por página"
	limit, err := parseLimit(r)
	if err != nil {
		writeError(w, h.logger, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}

	// 3. ¿Desde dónde? Sin cursor es la primera página
	var after *domain.TxCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		c, err := decodeCursor(raw)
		if err != nil {
			writeError(w, h.logger, http.StatusBadRequest, "invalid_cursor", "el cursor no es válido")
			return
		}
		after = &c
	}

	// 4. La página
	tickets, hasMore, err := h.svc.MyBets(r.Context(), userID, filter, after, limit)
	if err != nil {
		h.logger.Error("historial de apuestas falló", "error", err)
		writeError(w, h.logger, http.StatusInternalServerError, "internal_error", "ocurrió un error interno en el servidor, vovler más tarde")
		return
	}

	// 5. La respuesta
	page := betsPage{Data: make([]ticketResponse, 0, len(tickets))}
	for _, b := range tickets {
		tr := ticketResponse{
			ID:                   b.ID,
			StakeCents:           b.StakeCents,
			TotalOddsMilli:       b.TotalOddsMilli,
			PotentialRetunrCents: b.PotentialReturnCents,
			Status:               string(b.Status),
			PlaceAt:              b.PlacedAt,
			Selections:           make([]ticketSelectionResponse, 0, len(b.Selections)),
		}
		for _, s := range b.Selections {
			tr.Selections = append(tr.Selections, ticketSelectionResponse{
				SelectionID: s.SelectionID,
				HomeTeam:    s.HomeTeam,
				AwayTeam:    s.AwayTeam,
				StartsAt:    s.StartsAt,
				MarketType:  s.MarketType,
				Code:        s.Code,
				OddsMilli:   s.OddsMilli,
				Result:      s.Result,
			})
		}
		page.Data = append(page.Data, tr)
	}

	if hasMore {
		last := tickets[len(tickets)-1]
		next, err := encodeCursor(domain.TxCursor{CreatedAt: last.PlacedAt, ID: last.ID})
		if err != nil {
			h.logger.Error("codificar cursor falló", "error", err)
			writeError(w, h.logger, http.StatusInternalServerError, "internal_error", "ocurrió un error en el servidor, intentar mas tarde")
			return
		}
		page.NextCursor = &next
	}

	writeJSON(w, h.logger, http.StatusOK, page)

}
