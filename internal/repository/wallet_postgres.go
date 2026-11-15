package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kingperseocs/golazo-api/internal/domain"
)

// Código de error de Postgres para  "texto con formato" (por ejemplo, un UUID mal escrito)
const pgInvalidTextRepresentation = "22P02"

// WalletPostgres guarda billeteras y movimientos en PostgreSQL.
type WalletPostgres struct {
	pool *pgxpool.Pool
}

// NewWalletPostgres crea el repositorio con el pool de conexios.
func NewWalletPostgres(pool *pgxpool.Pool) *WalletPostgres {
	return &WalletPostgres{pool: pool}
}

// Consultas SQL
const lockWalletSQL = `
SELECT id, user_id, type, currency, balance_cents, created_at, updated_at
FROM wallets
WHERE id = $1 AND user_id = $2
FOR UPDATE`

const findTransactionByKeySQL = `
SELECT id, wallet_id, type, amount_cents, balance_after_cents, idempotency_key, created_at
FROM wallet_transactions
WHERE wallet_id = $1 AND idempotency_key = $2`

const updateBalanceSQL = `
UPDATE wallets
SET balance_cents = $2, updated_at = now()
WHERE id = $1`

const insertTransactionSQL = `
INSERT INTO wallet_transactions (wallet_id, type, amount_cents, balance_after_cents, idempotency_key)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, created_at`

const findWalletByUserSQL = `
SELECT id, user_id, type, currency, balance_cents, created_at, updated_at
FROM wallets
WHERE user_id = $1 AND type = 'real' AND currency = 'PEN'
`

// Primera página: los movimientos más recientes de la billetera real del usuario.
const listTransactionsFirstPageSQL = `
SELECT t.id, t.wallet_id, t.type, t.amount_cents, t.balance_after_cents, t.idempotency_key, t.created_at
FROM wallet_transactions t
JOIN wallets w ON w.id = t.wallet_id 
WHERE w.user_id = $1 AND w.type = 'real' AND w.currency = 'PEN'
ORDER BY t.created_at DESC, t.id DESC 
LIMIT $2 
`

// Páginas siguientes: los que vienen DESPUÉS del cursor (mas antiguos)
const listTransactionsAfterCursorSQL = `
SELECT t.id, t.wallet_id, t.type, t.amount_cents, t.balance_after_cents, t.idempotency_key, t.created_at
FROM wallet_transactions t
JOIN wallets w ON w.id = t.wallet_id 
WHERE w.user_id = $1 AND w.type = 'real' AND w.currency = 'PEN'
 AND (t.created_at, t.id) < ($3, $4)
ORDER BY t.created_at DESC, t.id DESC
LIMIT $2
`

// todo aqui se consulta a la bd
// función que lista las transacciones devuelve hasta limit movimientos del usuario, del asmas reciente al mas antiguo
func (r *WalletPostgres) ListTransactions(ctx context.Context, userID string, after *domain.TxCursor, limit int) (items[]domain.WalletTransaction, hasMore bool, err error) {
	// Pedimos uno más de los necesarios: si llega, sabemos que hay otra página
	fetch := limit + 1

	var rows pgx.Rows
	if after == nil {
		rows, err = r.pool.Query(ctx, listTransactionsFirstPageSQL, userID, fetch)
	} else {
		rows, err = r.pool.Query(ctx, listTransactionsAfterCursorSQL, userID, fetch, after.CreatedAt, after.ID)
	}
	if err != nil {
		return nil, false, fmt.Errorf("listar movimientos: %w", err)
	}
	defer rows.Close()

	items = make([]domain.WalletTransaction, 0, fetch)
	for rows.Next() {
		var t domain.WalletTransaction
		var txType string
		if err := rows.Scan(&t.ID, &t.WalletID, &txType, &t.AmountCents, &t.BalanceAfterCents, &t.IdempotencyKey, &t.CreatedAt); err != nil {
			return nil, false, fmt.Errorf("leer movimiento: %w", err)
		}
		t.Type = domain.TransactionType(txType)
		items = append(items, t)
	}	
	if err := rows.Err(); err != nil {
		return nil,  false, fmt.Errorf("recorrer movimientos: %w", err)
	}
	if len(items) >limit {
		return items[:limit], true, nil
	}
	return items, false, nil
}

// FinfByUser devuelve la billetera real en soles del usuario
// Si no existe, devuelve domain.ErrWalletNotFound
func (r *WalletPostgres) FindByUser(ctx context.Context, userID string) (domain.Wallet, error) {

	var w domain.Wallet
	err := r.pool.QueryRow(ctx, findWalletByUserSQL, userID).Scan(
		&w.ID, &w.UserID, &w.Type, &w.Currency, &w.BalanceCents, &w.CreatedAt, &w.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Wallet{}, domain.ErrWalletNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgInvalidTextRepresentation {
		return domain.Wallet{}, domain.ErrWalletNotFound
	}
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("buscar billetera del usuario: %w", err)
	}
	return w, nil
}

// Deposit acredita amountCents en la billetera del usuario.
func (r *WalletPostgres) Deposit(ctx context.Context, userID, WalletID string, amountCents int64, key string) (domain.WalletTransaction, bool, error) {
	return r.applyMovement(ctx, userID, WalletID, key, domain.TxDeposit, amountCents,
		func(w *domain.Wallet) (domain.WalletTransaction, error) {
			return w.Deposit(amountCents, key)
		})
}

// Withdraw descuenta amountCents de la billetera del usuario.
// En el ledger, el retiro queda con monto negativo
func (r *WalletPostgres) Withdraw(ctx context.Context, userID, walletID string, amountCents int64, key string) (domain.WalletTransaction, bool, error) {
	return r.applyMovement(ctx, userID, walletID, key, domain.TxWithdrawal, -amountCents,
		func(w *domain.Wallet) (domain.WalletTransaction, error) {
			return w.Withdraw(amountCents, key)
		})
}

// applyMovement ejecuta un movimiento de dinero dentro de una transacción:
// bloquea la billetera, revisa la idempotencia, aplica la regla del dominio
// (apply), guarda el nuevo saldo y registra el movimiento en el ledger.
//
// ledgerAmount es el monto tal como queda en el ledger (negativo en retiros);
// se usa para reconocer un reintento con la misma clave.
// Si la clave ya se usó con los mismos datos, devuelve el movimiento original
// con created = false y no toca el saldo.
func (r *WalletPostgres) applyMovement(
	ctx context.Context,
	userID, walletID, key string,
	txType domain.TransactionType,
	ledgerAmount int64,
	apply func(w *domain.Wallet) (domain.WalletTransaction, error),
) (tx domain.WalletTransaction, created bool, err error) {
	err = pgx.BeginFunc(ctx, r.pool, func(dbTx pgx.Tx) error {

		// 1.Bloquear la billetera, solo si pertenece al usuario.
		var w domain.Wallet
		err := dbTx.QueryRow(ctx, lockWalletSQL, walletID, userID).Scan(
			&w.ID, &w.UserID, &w.Type, &w.Currency, &w.BalanceCents, &w.CreatedAt, &w.UpdatedAt,
		)
		if err != nil {
			return mapWalletError(err)
		}

		// 2. ¿Ya procesamos esta clave? (se revisa DESPUÉS del bloqueo)
		existing, found, err := findTransactionByKey(ctx, dbTx, walletID, key)
		if err != nil {
			return err
		}
		if found {
			if existing.Type != txType || existing.AmountCents != ledgerAmount {
				return domain.ErrIdempotencyConflict
			}
			tx = existing
			return nil
		}

		//3. La regla del negocio la decide el dominio (depositar, retirar)
		newTx, err := apply(&w)
		if err != nil {
			return err
		}

		// 4. Guardar el nuevo saldo
		if _, err := dbTx.Exec(ctx, updateBalanceSQL, w.ID, w.BalanceCents); err != nil {
			return fmt.Errorf("actualizar saldo: %w", err)
		}

		// 5. Registrar el movimiento en el ledger
		err = dbTx.QueryRow(ctx, insertTransactionSQL,
			newTx.WalletID, string(newTx.Type), newTx.AmountCents, newTx.BalanceAfterCents, newTx.IdempotencyKey,
		).Scan(&newTx.ID, &newTx.CreatedAt)
		if err != nil {
			return fmt.Errorf("insertar movimiento: %w", err)
		}

		tx = newTx
		created = true
		return nil
	})
	if err != nil {
		return domain.WalletTransaction{}, false, err
	}
	return tx, created, nil
}

// findTransactionByKey busca un movimiento por su calve de idempotencia.
func findTransactionByKey(ctx context.Context, dbTx pgx.Tx, walletID, key string) (domain.WalletTransaction, bool, error) {
	var t domain.WalletTransaction
	var txType string
	err := dbTx.QueryRow(ctx, findTransactionByKeySQL, walletID, key).Scan(
		&t.ID, &t.WalletID, &txType, &t.AmountCents, &t.BalanceAfterCents, &t.IdempotencyKey, &t.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.WalletTransaction{}, false, nil
	}
	if err != nil {
		return domain.WalletTransaction{}, false, fmt.Errorf("buscar movimiento por claves: %w", err)
	}
	t.Type = domain.TransactionType(txType)
	return t, true, nil
}

func mapWalletError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrWalletNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgInvalidTextRepresentation {
		return domain.ErrWalletNotFound
	}
	return fmt.Errorf("bloquear billetera: %w", err)
}
