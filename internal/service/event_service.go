package service

import (
	"context"

	"github.com/kingperseocs/golazo-api/internal/domain"
)

// EventRepository es lo que el servicio  necesita para leer el catalogo
type EventRepository interface {
	ListUpcoming(ctx context.Context, limit int) ([]domain.Event, error)
}

// EventService coordina la consulta del catálogo de partidos
type EventService struct {
	repo EventRepository
}

// NewEventService crea el servicio con su repositorio
func NewEventService(repo EventRepository) *EventService {
	return &EventService{repo: repo}
}

// ListUpcoming devuelve los próximos partidos en lso que se puede apostar
func (s *EventService) ListUpcoming(ctx context.Context, limit int) ([]domain.Event, error) {
	return s.repo.ListUpcoming(ctx, limit)
}
