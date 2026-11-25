package service

import (
	"context"
	"github.com/kingperseocs/golazo-api/internal/domain"
)

// BetRepository es lo que el servicio necesita para registrar apuestas.
type BetRepository interface {
	PlaceBet(ctx context.Context, userId string, stakeCents int64, picks []domain.BetPick, key string) (domain.Bet, bool, error)
	ListBets(ctx context.Context, userID string, filter domain.BetListFilter, after *domain.TxCursor, limit int) ([]domain.BetTicket, bool, error)
}

// BetService coordina las operaciones de apuesta
type BetService struct {
	repo BetRepository
}

// NewBetService crea el servicio con su repositorio
func NewBetService(repo BetRepository) *BetService {
	return &BetService{repo: repo}
}

// PlaceBet registra una apuesta y cobra el monto. created indica si la apuesta es nueva (true) o si es un reintento de una ya regsitrada (false)
func (s *BetService) PlaceBet(ctx context.Context, userID string, stakeCents int64, picks []domain.BetPick, key string) (domain.Bet, bool, error) {
	return s.repo.PlaceBet(ctx, userID, stakeCents, picks, key)
}

// My BetsList devuelve una página de las apuestas del usuario autenticado
func (s *BetService) MyBets(ctx context.Context, userID string, filter domain.BetListFilter, after *domain.TxCursor, limit int) ([]domain.BetTicket, bool, error) {
	return s.repo.ListBets(ctx, userID, filter, after, limit)
}
