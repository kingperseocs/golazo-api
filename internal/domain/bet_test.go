package domain

import (
	"errors"
	"testing"
)

// sel crea una selección de prueba.
func sel(eventID string, oddsMilli int64) BetSelection {
	return BetSelection{SelectionID: "sel-" + eventID, EventID: eventID, OddsMilli: oddsMilli}
}

func TestNewBet(t *testing.T) {
	tests := []struct {
		name       string
		stake      int64
		selections []BetSelection
		wantErr    error
		wantOdds   int64
		wantReturn int64
	}{
		{"simple: S/ 20 a 2.10", 2_000, []BetSelection{sel("e1", 2_100)}, nil, 2_100, 4_200},
		{"combinada: 2.10 × 1.80", 2_000, []BetSelection{sel("e1", 2_100), sel("e2", 1_800)}, nil, 3_780, 7_560},
		{"redondeo del retorno hacia abajo", 100, []BetSelection{sel("e1", 1_333)}, nil, 1_333, 133},
		{"redondeo de la cuota hacia abajo", 100, []BetSelection{sel("e1", 1_333), sel("e2", 1_333)}, nil, 1_776, 177},
		{"mismo partido en la combinada", 2_000, []BetSelection{sel("e1", 2_100), sel("e1", 3_500)}, ErrSameEventInCombo, 0, 0},
		{"sin selecciones", 2_000, nil, ErrNoSelections, 0, 0},
		{"cuota de 1.000 o menos", 2_000, []BetSelection{sel("e1", 1_000)}, ErrInvalidOdds, 0, 0},
		{"monto bajo el mínimo", 99, []BetSelection{sel("e1", 2_100)}, ErrStakeTooLow, 0, 0},
		{"justo el mínimo", 100, []BetSelection{sel("e1", 2_100)}, nil, 2_100, 210},
		{"monto sobre el máximo", 500_001, []BetSelection{sel("e1", 2_100)}, ErrStakeTooHigh, 0, 0},
		{"cuota total demasiado alta", 100, []BetSelection{sel("e1", 50_000), sel("e2", 50_000)}, ErrOddsTooHigh, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bet, err := NewBet("user-1", "wallet-1", tt.stake, tt.selections, "bet-001")

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, se esperaba %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if bet.TotalOddsMilli != tt.wantOdds {
				t.Errorf("cuota total = %d, se esperaba %d", bet.TotalOddsMilli, tt.wantOdds)
			}
			if bet.PotentialReturnCents != tt.wantReturn {
				t.Errorf("retorno = %d, se esperaba %d", bet.PotentialReturnCents, tt.wantReturn)
			}
			if bet.Status != BetPending {
				t.Errorf("status = %q, se esperaba %q", bet.Status, BetPending)
			}
		})
	}
}

func TestNewBet_DemasiadasSelecciones(t *testing.T) {
	var selections []BetSelection
	for i := 0; i <= MaxSelections; i++ { // 11 selecciones de partidos distintos
		selections = append(selections, sel(string(rune('a'+i)), 1_100))
	}

	if _, err := NewBet("user-1", "wallet-1", 2_000, selections, "bet-001"); !errors.Is(err, ErrTooManySelections) {
		t.Errorf("error = %v, se esperaba ErrTooManySelections", err)
	}
}
