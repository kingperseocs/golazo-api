package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/kingperseocs/golazo-api/internal/domain"
)

// aqui puras consultas
const listBetsSQL = `
WITH page AS (
	SELECT id, stake_cents, total_odds_milli, potential_return_cents, status, placed_at
	FROM bets
	WHERE user_id = $1
		AND ($2::text = ''
			OR ($2 = 'pending' AND status = 'pending')
			OR ($2 = 'settled' AND status <> 'pending')) 
		AND ($3::timestamptz IS NULL OR (placed_at, id) < ($3, $4::uuid))
	ORDER BY placed_at DESC, id DESC
	LIMIT $5
)
SELECT b.id, b.stake_cents, b.total_odds_milli, b.potential_return_cents, b.status, b.placed_at,
	bs.selection_id, home.name, away.name, e.starts_at, m.type, s.code, bs.odds_milli, bs.result
FROM page b
JOIN bet_selections bs ON bs.bet_id = b.id
JOIN selections s ON s.id = bs.selection_id
JOIN markets m ON m.id = s.market_id
JOIN events e ON e.id = m.event_id
JOIN teams home        ON home.id = e.home_team_id
JOIN teams away        ON away.id = e.away_team_id
ORDER BY b.placed_at DESC, b.id DESC, e.starts_at, e.id
`

// ListBets devuelve hasta el limit apuestas del usuario, de la más reciente a la más antigua, cada una con sus selecciones.
// Si after no es nil, empieza después de ese cursor. hasMore indica si quedan más apuestas.
func (r *BetPostgres) ListBets(
	ctx context.Context,
	userID string,
	filter domain.BetListFilter,
	after *domain.TxCursor,
	limit int,
) (tickets []domain.BetTicket, hasMore bool, err error) {
	// Sin cursor, los dos parámetros van como NULL
	var afterTime *time.Time
	var afterID *string
	if after != nil {
		afterTime = &after.CreatedAt
		afterID = &after.ID
	}

	// Pedimos una apuesya más de las necesarias: Si llega, hay otra página
	rows, err := r.pool.Query(ctx, listBetsSQL, userID, string(filter), afterTime, afterID, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("listar apuestas: %w", err)
	}
	defer rows.Close()

	tickets = make([]domain.BetTicket, 0, limit+1)
	betPos := make(map[string]int) // id de la apuesta -> su posición en tickets

	for rows.Next() {
		var b domain.BetTicket
		var s domain.TicketSelection
		var status string
		if err := rows.Scan(
			&b.ID, &b.StakeCents, &b.TotalOddsMilli, &b.PotentialReturnCents, &status, &b.PlacedAt,
			&s.SelectionID, &s.HomeTeam, &s.AwayTeam, &s.StartsAt, &s.MarketType, &s.Code, &s.OddsMilli, &s.Result,
		); err != nil {
			return nil, false, fmt.Errorf("leer apuestas: %w", err)
		}

		i, ok := betPos[b.ID]
		if !ok {
			b.Status = domain.BetStatus(status)
			tickets = append(tickets, b)
			i = len(tickets) - 1
			betPos[b.ID] = i
		}
		tickets[i].Selections = append(tickets[i].Selections, s)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("recorrer apuestas: %w", err)
	}
	if len(tickets) > limit {
		return tickets[:limit], true, nil
	}
	return tickets, false, nil
}
