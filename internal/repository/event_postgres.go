package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kingperseocs/golazo-api/internal/domain"
)

// EventPostgres lee el catálogo de partidos desde PostgreSQL.
type EventPostgres struct {
	pool *pgxpool.Pool
}

// newEventPostgres crea el repositorio con el pool de conexiones.
func NewEventPostgres(pool *pgxpool.Pool) *EventPostgres {
	return &EventPostgres{pool: pool}
}

// El CTE "upcoming" elige primero los partidos (con su LIMIT) y recién después se unen sus mercados y selecciones. Si el LIMIT fuera al final,
// limitaria FILAS (una por selección) y cortaría partidos por la mitad
const listUpcomingEventSQL = `
WITH upcoming AS (
	SELECT id, home_team_id, away_team_id, starts_at, status
	FROM events
	WHERE status = 'scheduled' AND starts_at > now()
	ORDER BY starts_at, id
	LIMIT $1
)
SELECT e.id, home.name, away.name, e.starts_at, e.status,
	   m.id, m.type, m.status,
	   s.id, s.code, s.odds_milli
FROM upcoming e
JOIN teams home        ON home.id = e.home_team_id
JOIN teams away        ON away.id = e.away_team_id
JOIN markets m    ON m.event_id = e.id AND m.status IN ('open', 'suspended')
JOIN selections s ON s.market_id = m.id
ORDER BY e.starts_at, e.id, m.type,
	array_position(ARRAY['home', 'draw', 'away', 'over', 'under'], s.code)
`

// ListUpcoming devuelve hasta limit partidos que aún no empiezan, del más próximo al más lejano , cada uno con sus mercados y selecciones.
func (r *EventPostgres) ListUpcoming(ctx context.Context, limit int) ([]domain.Event, error) {
	rows, err := r.pool.Query(ctx, listUpcomingEventSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("listar partidos: %w", err)
	}
	defer rows.Close()

	events := make([]domain.Event, 0, limit)
	eventPos := make(map[string]int)  // ide del partido -> su posición en events
	marketPos := make(map[string]int) // id del marcado -> su posición en los mercados de su partido

	for rows.Next() {
		var e domain.Event
		var m domain.Market
		var s domain.Selection
		if err := rows.Scan(
			&e.ID, &e.HomeTeam, &e.AwayTeam, &e.StartsAt, &e.Status,
			&m.ID, &m.Type, &m.Status,
			&s.ID, &s.Code, &s.OddsMilli,
		); err != nil {
			return nil, fmt.Errorf("leer partido: %w", err)
		}

		// Primera vez que vemos este partido? lo agregamos
		i, ok := eventPos[e.ID]
		if !ok {
			events = append(events, e)
			i = len(events) - 1
			eventPos[e.ID] = i
		}
		event := &events[i]

		// Primera vez que vemos este mercado? Lo agregamos a su partido
		j, ok := marketPos[m.ID]
		if !ok {
			event.Markets = append(event.Markets, m)
			j = len(event.Markets) - 1
			marketPos[m.ID] = j
		}

		// Cada fila trae una selección: va a su mercado
		event.Markets[j].Selections = append(event.Markets[j].Selections, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer partidos: %w", err)
	}
	return events, nil
}
