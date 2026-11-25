//go:build integration

package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kingperseocs/golazo-api/internal/domain"
	"github.com/kingperseocs/golazo-api/internal/repository"
)

// testPick es una selección de una apuesta de prueba.
type testPick struct {
	selectionID string
	oddsMilli   int64
}

// insertBet guarda una apuesta directamente en la BD, con la hora y el estado
// que el test necesita. placedAgo dice hace cuánto se hizo.
func insertBet(t *testing.T, pool *pgxpool.Pool, userID, walletID, status string, placedAgo time.Duration, picks ...testPick) string {
	t.Helper()
	ctx := context.Background()

	total := int64(1000)
	for _, p := range picks {
		total = total * p.oddsMilli / 1000
	}
	var settledAt *time.Time
	if status != "pending" {
		now := time.Now()
		settledAt = &now
	}

	betID := insertReturningID(t, pool, `
		INSERT INTO bets (user_id, wallet_id, stake_cents, total_odds_milli, potential_return_cents,
		                  status, idempotency_key, placed_at, settled_at)
		VALUES ($1, $2, 1000, $3, $3, $4, gen_random_uuid()::text, now() - $5::interval, $6)
		RETURNING id`,
		userID, walletID, total, status, placedAgo.String(), settledAt)

	for _, p := range picks {
		if _, err := pool.Exec(ctx,
			"INSERT INTO bet_selections (bet_id, selection_id, odds_milli) VALUES ($1, $2, $3)",
			betID, p.selectionID, p.oddsMilli); err != nil {
			t.Fatalf("insertar selección de la apuesta: %v", err)
		}
	}
	return betID
}

func TestBetPostgres_ListBets(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := repository.NewBetPostgres(pool)

	userID, walletID := userWithBalance(t, pool, 10_000)
	otherID, otherWallet := createUserWithWallet(t, pool, "otro@golazo.pe", "87654321")
	clasico := createMatch(t, pool, "Alianza Lima", "Universitario")
	cristal := createMatch(t, pool, "Sporting Cristal", "Melgar")

	oldest := insertBet(t, pool, userID, walletID, "won", 3*time.Hour, testPick{clasico.home, 2_100})
	combo := insertBet(t, pool, userID, walletID, "pending", 2*time.Hour,
		testPick{clasico.draw, 3_200}, testPick{cristal.away, 3_500})
	newest := insertBet(t, pool, userID, walletID, "pending", 1*time.Hour, testPick{cristal.home, 2_100})
	insertBet(t, pool, otherID, otherWallet, "pending", 0, testPick{clasico.home, 2_100}) // de otro usuario

	// Página 1: las 2 más recientes.
	page1, hasMore, err := repo.ListBets(ctx, userID, domain.BetsAll, nil, 2)
	if err != nil {
		t.Fatalf("ListBets: %v", err)
	}
	if len(page1) != 2 || !hasMore {
		t.Fatalf("página 1: %d apuestas, hasMore = %v; se esperaba 2 y true", len(page1), hasMore)
	}
	if page1[0].ID != newest || page1[1].ID != combo {
		t.Errorf("orden incorrecto: se esperaba la más reciente primero")
	}

	// La combinada trae sus 2 selecciones, con equipos y cuota congelada.
	if len(page1[1].Selections) != 2 {
		t.Fatalf("selecciones de la combinada = %d, se esperaban 2", len(page1[1].Selections))
	}
	first := page1[1].Selections[0]
	if first.HomeTeam != "Alianza Lima" || first.Code != "draw" || first.OddsMilli != 3_200 || first.MarketType != "match_result" {
		t.Errorf("selección = %+v, se esperaba Alianza Lima, draw, 3200, match_result", first)
	}

	// Página 2: desde el cursor de la última apuesta de la página 1.
	last := page1[len(page1)-1]
	page2, hasMore, err := repo.ListBets(ctx, userID, domain.BetsAll, &domain.TxCursor{CreatedAt: last.PlacedAt, ID: last.ID}, 2)
	if err != nil {
		t.Fatalf("ListBets página 2: %v", err)
	}
	if len(page2) != 1 || hasMore || page2[0].ID != oldest {
		t.Errorf("página 2: %d apuestas, hasMore = %v; se esperaba solo la más antigua", len(page2), hasMore)
	}
}

func TestBetPostgres_ListBets_Filtro(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := repository.NewBetPostgres(pool)

	userID, walletID := userWithBalance(t, pool, 10_000)
	match := createMatch(t, pool, "Alianza Lima", "Universitario")
	insertBet(t, pool, userID, walletID, "pending", time.Hour, testPick{match.home, 2_100})
	insertBet(t, pool, userID, walletID, "won", 2*time.Hour, testPick{match.draw, 3_200})
	insertBet(t, pool, userID, walletID, "lost", 3*time.Hour, testPick{match.away, 3_500})

	tests := []struct {
		filter domain.BetListFilter
		want   int
	}{
		{domain.BetsAll, 3},
		{domain.BetsPending, 1},
		{domain.BetsSettled, 2},
	}
	for _, tt := range tests {
		tickets, _, err := repo.ListBets(ctx, userID, tt.filter, nil, 20)
		if err != nil {
			t.Fatalf("ListBets(%q): %v", tt.filter, err)
		}
		if len(tickets) != tt.want {
			t.Errorf("filtro %q: %d apuestas, se esperaban %d", tt.filter, len(tickets), tt.want)
		}
	}
}
