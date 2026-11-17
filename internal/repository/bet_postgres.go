package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kingperseocs/golazo-api/internal/domain"
)

// BetPostgres guarda apuestas en Postgresql
type BetPostgres struct {
	pool *pgxpool.Pool
}

// NewBetPostgres crea el repositorio con el pool de conexiones
func NewBetPostgres(pool *pgxpool.Pool) *BetPostgres {
	return &BetPostgres{pool: pool}
}

// establecemos las querys

const lockUserWalletSQL = `
SELECT id, user_id, type, currency, balance_cents, created_at, updated_at
FROM wallets
WHERE user_id = $1 AND type = 'real' AND currency = 'PEN'
FOR UPDATE
`

const findBetByKeySQL = `
SELECT id, user_id, wallet_id, stake_cents, total_odds_milli, potential_return_cents,
status, idempotency_key, placed_at
FROM bets
WHERE user_id = $1 AND idempotency_key = $2
`

const findBetSelectionsSQL = `
SELECT bs.selection_id, m.event_id, bs.odds_milli
FROM bet_selections bs
JOIN selections s ON s.id = bs.selection_id
JOIN markets m   ON m.id = s.market_id
WHERE bs.bet_id = $1
`

// Importante: Una selección acepta apuestas si su mercado está abierto y el partido todavía no empezó

const loadSelectionsSQL = `
SELECT s.id, m.event_id, s.odds_milli,
       (m.status = 'open' AND e.status = 'scheduled' AND e.starts_at > now()) AS available
FROM selections s
JOIN markets m ON m.id = s.market_id
JOIN events e  ON e.id = m.event_id
WHERE s.id = ANY($1::uuid[])`

const insertBetSQL = `
INSERT INTO bets (user_id, wallet_id, stake_cents, total_odds_milli, potential_return_cents, status, idempotency_key)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, placed_at`

const insertBetSelectionSQL = `
INSERT INTO bet_selections (bet_id, selection_id, odds_milli)
VALUES ($1, $2, $3)`

const insertLedgerWithReferenceSQL = `
INSERT INTO wallet_transactions (wallet_id, type, amount_cents, balance_after_cents, reference_id, idempotency_key)
VALUES ($1, $2, $3, $4, $5, $6)`

// PlaceBet registra una apuesta y descuenta el monto de la billetera real del usuario, todo en una sola transacción: o se guarda todo, o nada
// Si la clave de idempotencia ya se usó con los mismos datos, devuelve la apuesta original con created = false y no cobra de nuevo
func (r *BetPostgres) PlaceBet(
	ctx context.Context,
	userID string,
	stakeCents int64,
	picks []domain.BetPick,
	key string,
) (bet domain.Bet, created bool, err error) {
	key = strings.TrimSpace(key)

	err = pgx.BeginFunc(ctx, r.pool, func(dbTx pgx.Tx) error {

		// 1. Bloquear la billetera real del usuario
		var w domain.Wallet
		err := dbTx.QueryRow(ctx, lockUserWalletSQL, userID).Scan(
			&w.ID, &w.UserID, &w.Type, &w.Currency, &w.BalanceCents, &w.CreatedAt, &w.UpdatedAt,
		)
		if err != nil {
			return mapWalletError(err)
		}

		// 2. ¿Ya registramos esta apuesta? (después del bloqueo)
		existing, found, err := findBetKey(ctx, dbTx, userID, key)
		if err != nil {
			return err
		}
		if found {
			if existing.StakeCents != stakeCents || !samePicks(existing.Selections, picks) {
				return domain.ErrIdempotencyConflict
			}
			bet = existing
			return nil
		}

		// 3. leer las selecciones de la BD: ¿Existen, aceptan apuesta, cambió la cuota?
		selections, err := loadSelections(ctx, dbTx, picks)
		if err != nil {
			return err
		}

		// 4. Reglas de la apuesta: montos, combinadas, cuota total, retorno
		newBet, err := domain.NewBet(userID, w.ID, stakeCents, selections, key)
		if err != nil {
			return err
		}

		// 5. guardar la apuesta y sus selecciones (con la cuota congelada)
		err = dbTx.QueryRow(ctx, insertBetSQL,
			newBet.UserID, newBet.WalletID, newBet.StakeCents, newBet.TotalOddsMilli,
			newBet.PotentialReturnCents, string(newBet.Status), newBet.IdempotencyKey,
		).Scan(&newBet.ID, &newBet.PlaceAt)
		if err != nil {
			return fmt.Errorf("insertar apuesta: %w", err)
		}
		for _, s := range newBet.Selections {
			if _, err := dbTx.Exec(ctx, insertBetSelectionSQL, newBet.ID, s.SelectionID, s.OddsMilli); err != nil {
				return fmt.Errorf("insertar selección de la apuesta: %w", err)
			}
		}

		// 6. Cobrar. Si no alcanza el saldo, el error hace ROLLBACK y la apuesta del paso 5 desaparece
		stakeTx, err := w.PlaceStake(newBet.StakeCents, newBet.ID)
		if err != nil {
			return err
		}
		if _, err := dbTx.Exec(ctx, updateBalanceSQL, w.ID, w.BalanceCents); err != nil {
			return fmt.Errorf("actualizar saldo: %w", err)
		}
		_, err = dbTx.Exec(ctx, insertLedgerWithReferenceSQL,
			stakeTx.WalletID, string(stakeTx.Type), stakeTx.AmountCents,
			stakeTx.BalanceAfterCents, stakeTx.ReferenceID, stakeTx.IdempotencyKey,
		)
		if err != nil {
			return fmt.Errorf("insertar movimiento de la apuesta: %w", err)
		}

		bet = newBet
		created = true
		return nil
	})
	if err != nil {
		return domain.Bet{}, false, err
	}
	return bet, created, nil
}

// loadSelections busca en la BD las selecciones elegidas y comprueba que existan, que acepten apuestas y que la cuota sea la que vio el usuario.
// Devuelve las selecciones con la cuota de la BD, nunca con la del cliente
func loadSelections(ctx context.Context, dbTx pgx.Tx, picks []domain.BetPick) ([]domain.BetSelection, error) {
	ids := make([]string, 0, len(picks))
	for _, p := range picks {
		ids = append(ids, p.SelectionID)
	}

	rows, err := dbTx.Query(ctx, loadSelectionsSQL, ids)
	if err != nil {
		return nil, mapSelectionErr(err)
	}
	defer rows.Close()

	type dbSelection struct {
		eventID   string
		oddsMilli int64
		available bool
	}
	found := make(map[string]dbSelection, len(picks))
	for rows.Next() {
		var id string
		var s dbSelection
		if err := rows.Scan(&id, &s.eventID, &s.oddsMilli, &s.available); err != nil {
			return nil, fmt.Errorf("leer selección: %w", err)
		}
		found[id] = s
	}
	if err := rows.Err(); err != nil {
		return nil, mapSelectionErr(err)
	}

	selections := make([]domain.BetSelection, 0, len(picks))
	var changes []domain.OddsChange
	for _, p := range picks {
		s, ok := found[p.SelectionID]
		if !ok {
			return nil, domain.ErrSelectionNotFound
		}
		if !s.available {
			return nil, domain.ErrSelectionClosed
		}
		if s.oddsMilli != p.OddsMilli {
			changes = append(changes, domain.OddsChange{SelectionID: p.SelectionID, OddsMilli: s.oddsMilli})
		}
		selections = append(selections, domain.BetSelection{
			SelectionID: p.SelectionID,
			EventID:     s.eventID,
			OddsMilli:   s.oddsMilli,
		})
	}
	if len(changes) > 0 {
		return nil, &domain.OddsChangedError{Changes: changes}
	}
	return selections, nil
}

// funciones helper
// findBetByKey busca una apuesta del usuario por su clave de idempotencia.
func findBetKey(ctx context.Context, dbTx pgx.Tx, userID, key string) (domain.Bet, bool, error) {
	var b domain.Bet
	var status string
	err := dbTx.QueryRow(ctx, findBetByKeySQL, userID, key).Scan(
		&b.ID, &b.UserID, &b.WalletID, &b.StakeCents, &b.TotalOddsMilli, &b.PotentialReturnCents,
		&status, &b.IdempotencyKey, &b.PlaceAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Bet{}, false, nil
	}
	if err != nil {
		return domain.Bet{}, false, fmt.Errorf("buscar apuesta por clave: %w", err) // ← este te falta
	}
	b.Status = domain.BetStatus(status)

	rows, err := dbTx.Query(ctx, findBetSelectionsSQL, b.ID)
	if err != nil {
		return domain.Bet{}, false, fmt.Errorf("buscar selecciones de la apuesta: %w", err)
	}
	defer rows.Close() // cerramos la conexion
	for rows.Next() {
		var s domain.BetSelection
		if err := rows.Scan(&s.SelectionID, &s.EventID, &s.OddsMilli); err != nil {
			return domain.Bet{}, false, fmt.Errorf("leer selección de la apuesta: %w", err)
		}
		b.Selections = append(b.Selections, s)
	}
	if err := rows.Err(); err != nil {
		return domain.Bet{}, false, fmt.Errorf("recorrer selecciones de la apuesta: %w", err)
	}
	return b, true, nil
}

// samePicks dice si un reintento eligió las mismas selecciones que la apuesta guardad (sin importar el orden)
func samePicks(saved []domain.BetSelection, picks []domain.BetPick) bool {
	if len(saved) != len(picks) {
		return false
	}
	ids := make(map[string]bool, len(saved))
	for _, s := range saved {
		ids[s.SelectionID] = true
	}
	for _, p := range picks {
		if !ids[p.SelectionID] {

		}
	}
	return true
}

// mapSelectionError: un id no es un UUID válido es una selección inexistente.
func mapSelectionErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgInvalidTextRepresentation {
		return domain.ErrSelectionNotFound
	}
	return fmt.Errorf("buscar selecciones: %w", err)
}
