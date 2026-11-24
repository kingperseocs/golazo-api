package domain

import (
	"errors"
	"fmt"
	"time"
)

// OddsScale es la escala de las cuotas: 2.100 se guarda como 2100
const OddsScale int64 = 1000

// Establecemos los límite de una apuesta
const (
	MinStakeCents     int64 = 100       // S/. 1.00
	MaxStakeCents     int64 = 500_000   // S/. 5 000.00
	MaxSelections           = 10        // selecciones en una combinada
	MaxTotalOddsMilli int64 = 1_000_000 // cuota total máxima: 1000.000
)

// BetStatus es el estado de una apuesta.
type BetStatus string

const (
	BetPending BetStatus = "pending"
	BetWon     BetStatus = "won"
	BetLost    BetStatus = "lost"
	BetVoid    BetStatus = "void"
)

// Establecemos el mensaje de error
var (
	ErrStakeTooLow       = errors.New("el monto mínimo de apuesta es S/ 1.00")
	ErrStakeTooHigh      = errors.New("el monto máximo de apuesta es S/ 5 0000.00")
	ErrNoSelections      = errors.New("la apuesta debe tener al menos una selección")
	ErrTooManySelections = errors.New("una combinada puede tener como máximo 10 selecciones")
	ErrSameEventInCombo  = errors.New("no se pueden combinar dos selecciones del mismo partido")
	ErrInvalidOdds       = errors.New("cuato inválida")
	ErrOddsTooHigh       = errors.New("la cuota total supera el máximo permitido ")
	ErrSelectionNotFound = errors.New("selección no encontrada")
	ErrSelectionClosed   = errors.New("la selección ya no acepta apuestas")
)

// BetPick e sloq ue elige el usuario: una selección y la cuota que vio en pantanlla
type BetPick struct {
	SelectionID string
	OddsMilli   int64
}

// OddsChange es la cuota actual de una selección que cambió
type OddsChange struct {
	SelectionID string
	OddsMilli   int64
}

// OddsChangedError indica que una o más cuotas cambiaron desde que el usuario las vio. Tare las cuotas nuevas para mostrárselas
type OddsChangedError struct {
	Changes []OddsChange
}

func (e *OddsChangedError) Error() string {
	return fmt.Sprintf("cambió la cuota de %d selección(es)", len(e.Changes))
}

// BetSelection es una selección dentro de una apuesta, con la cuota congelada en el momento de apostar
type BetSelection struct {
	SelectionID string
	EventID     string
	OddsMilli   int64
}

// Bet es una apuesta simple (una selección) o combinada (varias).
type Bet struct {
	ID                   string
	UserID               string
	WalletID             string
	StakeCents           int64
	TotalOddsMilli       int64
	PotentialReturnCents int64
	Status               BetStatus
	IdempotencyKey       string
	Selections           []BetSelection
	PlacedAt             time.Time
}

// NewBet valida una nueva apuesta y calcula su total y su retorno posible
func NewBet(userID, walletID string, stakeCents int64, selections []BetSelection, idempotencyKey string) (Bet, error) {
	key, err := normalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return Bet{}, err
	}
	if stakeCents < MinStakeCents {
		return Bet{}, ErrStakeTooLow
	}
	if stakeCents > MaxStakeCents {
		return Bet{}, ErrStakeTooHigh
	}

	totalOdds, err := TotalOdds(selections)
	if err != nil {
		return Bet{}, err
	}

	return Bet{
		UserID:               userID,
		WalletID:             walletID,
		StakeCents:           stakeCents,
		TotalOddsMilli:       totalOdds,
		PotentialReturnCents: PotentialReturn(stakeCents, totalOdds),
		Status:               BetPending,
		IdempotencyKey:       key,
		Selections:           selections,
	}, nil

}

// funcion que multiplica las cuotas de las selecciones
// regla de redondeo: en cada multiplicación se descartan los decimales que pasen de la milésima (redondeo hacia abajo)
func TotalOdds(selections []BetSelection) (int64, error) {
	if len(selections) == 0 {
		return 0, ErrNoSelections
	}
	if len(selections) > MaxSelections {
		return 0, ErrTooManySelections
	}

	seenEvents := make(map[string]bool, len(selections))
	total := OddsScale // empieza en 1.000

	for _, s := range selections {
		if s.OddsMilli <= OddsScale || s.OddsMilli > MaxTotalOddsMilli {
			return 0, ErrInvalidOdds
		}
		if seenEvents[s.EventID] {
			return 0, ErrSameEventInCombo
		}
		seenEvents[s.EventID] = true

		total = total * s.OddsMilli / OddsScale
		if total > MaxTotalOddsMilli {
			return 0, ErrOddsTooHigh
		}
	}
	return total, nil
}

// PotentialReturn es lo que cobra el usuario si gana: monto x cuota
// redondeo hcia abajo al céntimo
func PotentialReturn(stakeCents, totalOddsMilli int64) int64 {
	return stakeCents * totalOddsMilli / OddsScale
}
