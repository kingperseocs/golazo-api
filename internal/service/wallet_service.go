package service

import (
	"context"

	"github.com/kingperseocs/golazo-api/internal/domain"
)

// WalletRepository es lo que el servicio necesita para mover dinero.
type WalletRepository interface {
	Deposit(ctx context.Context, userID, walletID string, amountCents int64, key string) (domain.WalletTransaction, bool, error)
	Withdraw(ctx context.Context, userID, walletID string, amountCents int64, key string) (domain.WalletTransaction, bool, error)
	FindByUser(ctx context.Context, userID string) (domain.Wallet, error)
}

// WalletService
type WalletService struct {
	repo WalletRepository
}

// NewWalletService crea el servicio con su repositorio.
func NewWalletService(repo WalletRepository) *WalletService {
	return &WalletService{repo: repo}
}

// Deposit acredita dinero en la billetera. created indica si el movimiento es nuevo (true) o si es un rendimiento de uno ya procesado (false)
func (s *WalletService) Deposit(ctx context.Context, userID, walletID string, amountCents int64, key string) (domain.WalletTransaction, bool, error) {
	return s.repo.Deposit(ctx, userID, walletID, amountCents, key)
}

// MyWallet devuelte la billetera del usuario autenticado
func (s *WalletService) MyWallet(ctx context.Context, userID string) (domain.Wallet, error) {
	return s.repo.FindByUser(ctx, userID)
}

// Withdraw descuenta de la billetera. cerated indica si el movimiento es nuevo (true) o si es un reintento de uno ya procesado (false)
func (s *WalletService) Withdraw(ctx context.Context, userID, walletID string, amountCents int64, key string) (domain.WalletTransaction, bool, error) {
	return s.repo.Withdraw(ctx, userID, walletID, amountCents, key)
}