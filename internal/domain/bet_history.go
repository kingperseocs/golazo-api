package domain

import (
	"errors"
	"time"
)

// BetListFilter elige que apuesta lsitar en "Mis apuestas"
type BetListFilter string

const (
	BetsAll     BetListFilter = ""
	BetsPending BetListFilter = "pending"
	BetsSettled BetListFilter = "settled"
)

var ErrInvalidBetFilter = errors.New("status debe ser pending o settled")

// ParseBetListFilter convierte el texto de ?status en un filtro válido
func ParseBetListFilter(raw string) (BetListFilter, error) {
	switch f := BetListFilter(raw); f {
	case BetsAll, BetsPending, BetsSettled:
		return f, nil
	default:
		return "", ErrInvalidBetFilter
	}
}

// BetTicket es una apuesta tal como la ve el usuario en "Mis apuestas"
type BetTicket struct {
	ID                   string
	StakeCents           int64
	TotalOddsMilli       int64
	PotentialReturnCents int64
	Status               BetStatus
	PlacedAt             time.Time
	Selections           []TicketSelection
}

// TicketSelection es una selección de la apuesta con lo necesario para entenderla: el partido, el mercado, lo que se eligió y su cuota congelada
type TicketSelection struct {
	SelectionID string
	HomeTeam    string
	AwayTeam    string
	StartsAt    time.Time
	MarketType  string
	Code        string
	OddsMilli   int64
	Result      string
}
