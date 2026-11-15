//go:build integration

package repository_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/kingperseocs/golazo-api/internal/domain"
	"github.com/kingperseocs/golazo-api/internal/repository"
)

func TestWalletPostgres_DepositoEIdempotencia(t *testing.T) {
	pool := newTestPool(t)
	userID, walletID := createUserWithWallet(t, pool, "ana@golazo.pe", "12345678")
	repo := repository.NewWalletPostgres(pool)
	ctx := context.Background()

	first, created, err := repo.Deposit(ctx, userID, walletID, 5_000, "yape-001")
	if err != nil {
		t.Fatalf("primer depósito: %v", err)
	}
	if !created || first.BalanceAfterCents != 5_000 {
		t.Fatalf("created = %v, balance_after = %d", created, first.BalanceAfterCents)
	}

	// Reintento con la misma clave y el mismo monto.
	again, created, err := repo.Deposit(ctx, userID, walletID, 5_000, "yape-001")
	if err != nil {
		t.Fatalf("reintento: %v", err)
	}
	if created {
		t.Error("el reintento no debería crear un movimiento nuevo")
	}
	if again.ID != first.ID {
		t.Errorf("id del reintento = %s, se esperaba %s", again.ID, first.ID)
	}

	// Misma clave, otro monto.
	_, _, err = repo.Deposit(ctx, userID, walletID, 9_000, "yape-001")
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Errorf("error = %v, se esperaba ErrIdempotencyConflict", err)
	}

	assertLedgerMatches(t, pool, walletID, 5_000)
}

func TestWalletPostgres_BilleteraInexistente(t *testing.T) {
	pool := newTestPool(t)
	repo := repository.NewWalletPostgres(pool)
	anyUser := "00000000-0000-0000-0000-000000000000"

	for _, id := range []string{"00000000-0000-0000-0000-000000000000", "no-es-un-uuid"} {
		_, _, err := repo.Deposit(context.Background(), anyUser, id, 5_000, "yape-001")
		if !errors.Is(err, domain.ErrWalletNotFound) {
			t.Errorf("id %q: error = %v, se esperaba ErrWalletNotFound", id, err)
		}
	}
}

func TestWalletPostgres_BilleteraAjena(t *testing.T) {
	pool := newTestPool(t)
	_, anaWallet := createUserWithWallet(t, pool, "ana@golazo.pe", "12345678")
	luisID, _ := createUserWithWallet(t, pool, "luis@golazo.pe", "87654321")
	repo := repository.NewWalletPostgres(pool)

	// Luis intenta depositar en la billetera de Ana.
	_, _, err := repo.Deposit(context.Background(), luisID, anaWallet, 5_000, "yape-001")
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("error = %v, se esperaba ErrWalletNotFound", err)
	}

	assertLedgerMatches(t, pool, anaWallet, 0)
}

func TestWalletPostgres_DepositosConcurrentes(t *testing.T) {
	pool := newTestPool(t)
	userID, walletID := createUserWithWallet(t, pool, "ana@golazo.pe", "12345678")
	repo := repository.NewWalletPostgres(pool)

	const workers = 50
	const amount int64 = 100 // S/ 1.00 cada uno

	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("concurrente-%d", i)
			if _, _, err := repo.Deposit(context.Background(), userID, walletID, amount, key); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("depósito concurrente falló: %v", err)
	}

	assertLedgerMatches(t, pool, walletID, workers*amount)
}

func TestWalletPostgres_FindByUser(t *testing.T) {
	pool := newTestPool(t)
	anaID, anaWallet := createUserWithWallet(t, pool, "ana@golazo.pe", "12345678")
	repo := repository.NewWalletPostgres(pool)
	ctx := context.Background()

	if _, _, err := repo.Deposit(ctx, anaID, anaWallet, 5_000, "yape-001"); err != nil {
		t.Fatalf("depósito: %v", err)
	}

	w, err := repo.FindByUser(ctx, anaID)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if w.ID != anaWallet || w.BalanceCents != 5_000 || w.Currency != "PEN" {
		t.Errorf("billetera = %+v", w)
	}

	// Un usuario que no existe no tiene billetera.
	_, err = repo.FindByUser(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Errorf("error = %v, se esperaba ErrWalletNotFound", err)
	}
}

func TestWalletPostgres_Retiro(t *testing.T) {
	pool := newTestPool(t)
	userID, walletID := createUserWithWallet(t, pool, "ana@golazo.pe", "12345678")
	repo := repository.NewWalletPostgres(pool)
	ctx := context.Background()

	if _, _, err := repo.Deposit(ctx, userID, walletID, 10_000, "yape-001"); err != nil {
		t.Fatalf("depósito: %v", err)
	}

	// Retiro válido: queda en el ledger con monto negativo.
	tx, created, err := repo.Withdraw(ctx, userID, walletID, 3_000, "retiro-001")
	if err != nil {
		t.Fatalf("retiro: %v", err)
	}
	if !created || tx.AmountCents != -3_000 || tx.BalanceAfterCents != 7_000 {
		t.Fatalf("created = %v, amount = %d, balance_after = %d", created, tx.AmountCents, tx.BalanceAfterCents)
	}

	// Reintento del mismo retiro: no descuenta de nuevo.
	again, created, err := repo.Withdraw(ctx, userID, walletID, 3_000, "retiro-001")
	if err != nil || created || again.ID != tx.ID {
		t.Fatalf("reintento: err = %v, created = %v, id = %s", err, created, again.ID)
	}

	// Saldo insuficiente: no cambia nada.
	_, _, err = repo.Withdraw(ctx, userID, walletID, 7_001, "retiro-002")
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Errorf("error = %v, se esperaba ErrInsufficientFunds", err)
	}

	// La clave de un depósito no sirve para un retiro.
	_, _, err = repo.Withdraw(ctx, userID, walletID, 10_000, "yape-001")
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Errorf("error = %v, se esperaba ErrIdempotencyConflict", err)
	}

	assertLedgerMatches(t, pool, walletID, 7_000)
}

func TestWalletPostgres_RetirosConcurrentes(t *testing.T) {
	pool := newTestPool(t)
	userID, walletID := createUserWithWallet(t, pool, "ana@golazo.pe", "12345678")
	repo := repository.NewWalletPostgres(pool)

	// Ana tiene S/ 10.00 y lanza 20 retiros de S/ 1.00 al mismo tiempo.
	if _, _, err := repo.Deposit(context.Background(), userID, walletID, 1_000, "yape-001"); err != nil {
		t.Fatalf("depósito: %v", err)
	}

	const workers = 20
	var wg sync.WaitGroup
	results := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := repo.Withdraw(context.Background(), userID, walletID, 100, fmt.Sprintf("retiro-%d", i))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)

	var ok, insufficient int
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, domain.ErrInsufficientFunds):
			insufficient++
		default:
			t.Errorf("error inesperado: %v", err)
		}
	}

	if ok != 10 || insufficient != 10 {
		t.Errorf("retiros exitosos = %d, rechazados = %d; se esperaba 10 y 10", ok, insufficient)
	}
	assertLedgerMatches(t, pool, walletID, 0)
}