//go:build integration

package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kingperseocs/golazo-api/internal/domain"
	"github.com/kingperseocs/golazo-api/internal/repository"
)

// testMatch es un partido de prueba con su mercado 1X2.
type testMatch struct {
	marketID string
	home     string // id de la selección "gana local" (cuota 2.100)
	draw     string // id de la selección "empate"     (cuota 3.200)
	away     string // id de la selección "gana visita" (cuota 3.500)
}

// insertReturningID ejecuta un INSERT ... RETURNING id y devuelve el id.
func insertReturningID(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&id); err != nil {
		t.Fatalf("insertar: %v\nSQL: %s", err, sql)
	}
	return id
}

// createMatch crea dos equipos, un partido para mañana y su mercado 1X2 abierto.
func createMatch(t *testing.T, pool *pgxpool.Pool, home, away string) testMatch {
	t.Helper()
	const insertTeam = "INSERT INTO teams (name, short_name) VALUES ($1, $1) RETURNING id"
	const insertSelection = "INSERT INTO selections (market_id, code, odds_milli) VALUES ($1, $2, $3) RETURNING id"

	homeID := insertReturningID(t, pool, insertTeam, home)
	awayID := insertReturningID(t, pool, insertTeam, away)
	eventID := insertReturningID(t, pool,
		"INSERT INTO events (home_team_id, away_team_id, starts_at) VALUES ($1, $2, now() + interval '1 day') RETURNING id",
		homeID, awayID)

	var m testMatch
	m.marketID = insertReturningID(t, pool,
		"INSERT INTO markets (event_id, type) VALUES ($1, 'match_result') RETURNING id", eventID)
	m.home = insertReturningID(t, pool, insertSelection, m.marketID, "home", 2_100)
	m.draw = insertReturningID(t, pool, insertSelection, m.marketID, "draw", 3_200)
	m.away = insertReturningID(t, pool, insertSelection, m.marketID, "away", 3_500)
	return m
}

// userWithBalance crea un usuario y le deposita amountCents.
func userWithBalance(t *testing.T, pool *pgxpool.Pool, amountCents int64) (userID, walletID string) {
	t.Helper()
	userID, walletID = createUserWithWallet(t, pool, "apostador@golazo.pe", "12345678")
	_, _, err := repository.NewWalletPostgres(pool).Deposit(context.Background(), userID, walletID, amountCents, "deposito-inicial")
	if err != nil {
		t.Fatalf("depositar: %v", err)
	}
	return userID, walletID
}

// countBets cuenta las apuestas guardadas.
func countBets(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM bets").Scan(&n); err != nil {
		t.Fatalf("contar apuestas: %v", err)
	}
	return n
}

func TestBetPostgres_Combinada(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := repository.NewBetPostgres(pool)

	userID, walletID := userWithBalance(t, pool, 10_000) // S/ 100
	alianza := createMatch(t, pool, "Alianza Lima", "Universitario")
	cristal := createMatch(t, pool, "Sporting Cristal", "Melgar")

	picks := []domain.BetPick{
		{SelectionID: alianza.home, OddsMilli: 2_100},
		{SelectionID: cristal.draw, OddsMilli: 3_200},
	}
	bet, created, err := repo.PlaceBet(ctx, userID, 2_000, picks, "apuesta-1")
	if err != nil {
		t.Fatalf("PlaceBet: %v", err)
	}
	if !created {
		t.Error("created = false, se esperaba true")
	}
	if bet.TotalOddsMilli != 6_720 || bet.PotentialReturnCents != 13_440 { // 2.10 × 3.20 = 6.72
		t.Errorf("cuota = %d, retorno = %d; se esperaba 6720 y 13440", bet.TotalOddsMilli, bet.PotentialReturnCents)
	}

	// S/ 100 - S/ 20 = S/ 80, y el ledger cuadra.
	assertLedgerMatches(t, pool, walletID, 8_000)

	// El movimiento del ledger apunta a la apuesta.
	var refID string
	err = pool.QueryRow(ctx,
		"SELECT reference_id FROM wallet_transactions WHERE wallet_id = $1 AND type = 'bet_stake'", walletID,
	).Scan(&refID)
	if err != nil {
		t.Fatalf("buscar movimiento bet_stake: %v", err)
	}
	if refID != bet.ID {
		t.Errorf("reference_id = %s, se esperaba %s", refID, bet.ID)
	}
}

func TestBetPostgres_CuotaCambio(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := repository.NewBetPostgres(pool)

	userID, walletID := userWithBalance(t, pool, 10_000)
	match := createMatch(t, pool, "Alianza Lima", "Universitario")

	// La casa baja la cuota a 1.95 después de que el usuario vio 2.10.
	if _, err := pool.Exec(ctx, "UPDATE selections SET odds_milli = 1950 WHERE id = $1", match.home); err != nil {
		t.Fatalf("cambiar cuota: %v", err)
	}

	picks := []domain.BetPick{{SelectionID: match.home, OddsMilli: 2_100}}
	_, _, err := repo.PlaceBet(ctx, userID, 2_000, picks, "apuesta-1")

	var oddsErr *domain.OddsChangedError
	if !errors.As(err, &oddsErr) {
		t.Fatalf("error = %v, se esperaba OddsChangedError", err)
	}
	if len(oddsErr.Changes) != 1 || oddsErr.Changes[0].OddsMilli != 1_950 {
		t.Errorf("cambios = %+v, se esperaba la cuota nueva 1950", oddsErr.Changes)
	}
	if n := countBets(t, pool); n != 0 {
		t.Errorf("apuestas guardadas = %d, se esperaba 0", n)
	}
	assertLedgerMatches(t, pool, walletID, 10_000)
}

func TestBetPostgres_SaldoInsuficiente(t *testing.T) {
	pool := newTestPool(t)
	repo := repository.NewBetPostgres(pool)

	userID, walletID := userWithBalance(t, pool, 1_000) // S/ 10
	match := createMatch(t, pool, "Alianza Lima", "Universitario")

	picks := []domain.BetPick{{SelectionID: match.home, OddsMilli: 2_100}}
	_, _, err := repo.PlaceBet(context.Background(), userID, 2_000, picks, "apuesta-1")
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("error = %v, se esperaba ErrInsufficientFunds", err)
	}

	// El INSERT de la apuesta se hizo ANTES de cobrar, pero el ROLLBACK lo deshizo.
	if n := countBets(t, pool); n != 0 {
		t.Errorf("apuestas guardadas = %d, se esperaba 0", n)
	}
	assertLedgerMatches(t, pool, walletID, 1_000)
}

func TestBetPostgres_Idempotencia(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := repository.NewBetPostgres(pool)

	userID, walletID := userWithBalance(t, pool, 10_000)
	match := createMatch(t, pool, "Alianza Lima", "Universitario")
	picks := []domain.BetPick{{SelectionID: match.home, OddsMilli: 2_100}}

	first, _, err := repo.PlaceBet(ctx, userID, 2_000, picks, "apuesta-1")
	if err != nil {
		t.Fatalf("primera apuesta: %v", err)
	}

	// Reintento idéntico: devuelve la misma apuesta y no cobra otra vez.
	second, created, err := repo.PlaceBet(ctx, userID, 2_000, picks, "apuesta-1")
	if err != nil {
		t.Fatalf("reintento: %v", err)
	}
	if created || second.ID != first.ID {
		t.Errorf("reintento: created = %v, id = %s; se esperaba false y %s", created, second.ID, first.ID)
	}
	assertLedgerMatches(t, pool, walletID, 8_000)

	// Misma clave con otro monto: conflicto.
	_, _, err = repo.PlaceBet(ctx, userID, 5_000, picks, "apuesta-1")
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Errorf("error = %v, se esperaba ErrIdempotencyConflict", err)
	}
}

func TestBetPostgres_SeleccionNoDisponible(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := repository.NewBetPostgres(pool)

	userID, _ := userWithBalance(t, pool, 10_000)
	match := createMatch(t, pool, "Alianza Lima", "Universitario")
	if _, err := pool.Exec(ctx, "UPDATE markets SET status = 'suspended' WHERE id = $1", match.marketID); err != nil {
		t.Fatalf("suspender mercado: %v", err)
	}

	tests := []struct {
		name        string
		selectionID string
		wantErr     error
	}{
		{"mercado suspendido", match.home, domain.ErrSelectionClosed},
		{"id que no existe", "00000000-0000-0000-0000-000000000000", domain.ErrSelectionNotFound},
		{"id que no es UUID", "abc", domain.ErrSelectionNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			picks := []domain.BetPick{{SelectionID: tt.selectionID, OddsMilli: 2_100}}
			_, _, err := repo.PlaceBet(ctx, userID, 2_000, picks, "apuesta-"+tt.name)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, se esperaba %v", err, tt.wantErr)
			}
		})
	}
}