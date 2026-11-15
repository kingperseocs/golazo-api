package domain

import (
	"errors"
	"strings"
	"time"
)

// MaxDepositCents es el depósito máximo permitido: S/ 5 000.00
const MaxDepositCents int64 = 500_000

// MMaxIdempotencyKeyLength es el largo máximo de una clave de idempotencia.
const MaxIdempotencyKeyLength = 100

// TransactionType es el tipo de movimiento en el ledger.
type TransactionType string

const (
	TxDeposit    TransactionType = "deposit"
	TxWithdrawal TransactionType = "withdrawal"
	TxBetStake   TransactionType = "bet_stake"
	TxBetWin     TransactionType = "bet_win"
	TxBetRefund  TransactionType = "bet_refund"
	TxBonus      TransactionType = "bonus"
	TxAdjustment TransactionType = "adjustment"
)

var (
	ErrInvalidAmount       = errors.New("el monto debe ser mayor que cero")
	ErrDepositLimit        = errors.New("el depósito máximo es de S/ 5 000.00")
	ErrInvalidIdempotency  = errors.New("la clave de idempotencia es obligatoria (máximo 100 caracteres)")
	ErrWalletNotFound      = errors.New("billetera no enontrada")
	ErrIdempotencyConflict = errors.New("la clave de idempotencia ya se usó con otros datos")
	ErrInsufficientFunds   = errors.New("saldo insuficiente")
)

// Wallet es la billetera de un usuario. Los montos van en céntimos.
type Wallet struct {
	ID           string
	UserID       string
	Type         string
	Currency     string
	BalanceCents int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// WalletTransaction es un movimiento del ledger. Nunca se modifica
type WalletTransaction struct {
	ID                string
	WalletID          string
	Type              TransactionType
	AmountCents       int64
	BalanceAfterCents int64
	IdempotencyKey    string
	CreatedAt         time.Time
}

// TxCursor marca la posición del último movimiento visto en el historial
// CreatedAt ordena; ID desempata cuando dos movimientos tienen la misma hora
type TxCursor struct {
	CreatedAt time.Time
	ID        string
}

// Deposit suma amountCents al saldo y devuelve el movimiento que lo registra.
// si algo no es válido, el saldo no cambia.
func (w *Wallet) Deposit(amountCents int64, idempotencyKey string) (WalletTransaction, error) {
	if amountCents <= 0 {
		return WalletTransaction{}, ErrInvalidAmount
	}
	if amountCents > MaxDepositCents {
		return WalletTransaction{}, ErrDepositLimit
	}
	key, err := normalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return WalletTransaction{}, err
	}

	w.BalanceCents += amountCents

	return WalletTransaction{
		WalletID:          w.ID,
		Type:              TxDeposit,
		AmountCents:       amountCents,
		BalanceAfterCents: w.BalanceCents,
		IdempotencyKey:    key,
	}, nil

}

// Withdraw resta amountCents del saldo y devuelve el movimiento que lo registra.
// En el ledger, el monto de un retiro se guarda en negativo.
// Si algo no es válido o no alcanza el saldo, el sado no cambia.
func (w *Wallet) Withdraw(amountCents int64, idempotencyKey string) (WalletTransaction, error) {

	// agregamos la regla
	if amountCents <= 0 {
		return WalletTransaction{}, ErrInvalidAmount
	}
	key, err := normalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return WalletTransaction{}, err
	}
	if amountCents > w.BalanceCents {
		return WalletTransaction{}, ErrInsufficientFunds
	}
	w.BalanceCents -= amountCents

	return WalletTransaction{
		WalletID:          w.ID,
		Type:              TxWithdrawal,
		AmountCents:       -amountCents,
		BalanceAfterCents: w.BalanceCents,
		IdempotencyKey:    key,
	}, nil
}

// normalizeIdempotencyKey limpiar la clave y revisa que sea válida
func normalizeIdempotencyKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" || len(key) > MaxIdempotencyKeyLength {
		return "", ErrInvalidIdempotency
	}
	return key, nil
}
