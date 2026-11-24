package domain

import "time"

// Event es un partido del catálogo, con los mercados en los que se puede apostar
type Event struct {
	ID       string
	HomeTeam string
	AwayTeam string
	StartsAt time.Time
	Status   string
	Markets  []Market
}

// Market es un tipo de apuesta de un partido, como "resultado final".
// Status dice si acepta apuestas (open) o está bloqueado (suspended)
type Market struct {
	ID         string
	Type       string
	Status     string
	Selections []Selection
}

// Selection es una opción de un mercado, con su cuota actual.
type Selection struct {
	ID        string
	Code      string
	OddsMilli int64
}
