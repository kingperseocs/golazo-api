//go:build integration

package repository_test

import (
	"context"
	"testing"

	"github.com/kingperseocs/golazo-api/internal/repository"
)

func TestEventPostgres_ListUpcoming(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := repository.NewEventPostgres(pool)

	clasico := createMatch(t, pool, "Alianza Lima", "Universitario")
	cristal := createMatch(t, pool, "Sporting Cristal", "Melgar")
	enVivo := createMatch(t, pool, "Cienciano", "Cusco FC")

	// El clásico se juega antes que el de Cristal.
	if _, err := pool.Exec(ctx,
		"UPDATE events SET starts_at = now() + interval '2 hours' WHERE id = (SELECT event_id FROM markets WHERE id = $1)",
		clasico.marketID); err != nil {
		t.Fatalf("adelantar clásico: %v", err)
	}
	// El de Cristal tiene su mercado suspendido: se muestra, bloqueado.
	if _, err := pool.Exec(ctx, "UPDATE markets SET status = 'suspended' WHERE id = $1", cristal.marketID); err != nil {
		t.Fatalf("suspender mercado: %v", err)
	}
	// Este ya empezó: no debe aparecer.
	if _, err := pool.Exec(ctx,
		"UPDATE events SET status = 'live', starts_at = now() - interval '30 minutes' WHERE id = (SELECT event_id FROM markets WHERE id = $1)",
		enVivo.marketID); err != nil {
		t.Fatalf("poner en vivo: %v", err)
	}

	events, err := repo.ListUpcoming(ctx, 20)
	if err != nil {
		t.Fatalf("ListUpcoming: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("partidos = %d, se esperaban 2 (el que está en vivo no se lista)", len(events))
	}
	if events[0].HomeTeam != "Alianza Lima" || events[1].HomeTeam != "Sporting Cristal" {
		t.Errorf("orden = %s, %s; se esperaba primero el clásico", events[0].HomeTeam, events[1].HomeTeam)
	}

	// Cada partido: un mercado con sus 3 selecciones, en orden 1X2.
	clasicoMarket := events[0].Markets[0]
	if len(events[0].Markets) != 1 || len(clasicoMarket.Selections) != 3 {
		t.Fatalf("mercados = %d, selecciones = %d; se esperaba 1 y 3", len(events[0].Markets), len(clasicoMarket.Selections))
	}
	codes := clasicoMarket.Selections[0].Code + "," + clasicoMarket.Selections[1].Code + "," + clasicoMarket.Selections[2].Code
	if codes != "home,draw,away" {
		t.Errorf("orden de selecciones = %s, se esperaba home,draw,away", codes)
	}
	if clasicoMarket.Selections[0].OddsMilli != 2_100 {
		t.Errorf("cuota local = %d, se esperaba 2100", clasicoMarket.Selections[0].OddsMilli)
	}
	if events[1].Markets[0].Status != "suspended" {
		t.Errorf("status del mercado = %q, se esperaba suspended", events[1].Markets[0].Status)
	}
}

func TestEventPostgres_ListUpcoming_LimitePorPartido(t *testing.T) {
	pool := newTestPool(t)
	repo := repository.NewEventPostgres(pool)

	createMatch(t, pool, "Alianza Lima", "Universitario")
	createMatch(t, pool, "Sporting Cristal", "Melgar")

	// limit = 1 debe traer UN partido completo, no 1 fila (1 selección).
	events, err := repo.ListUpcoming(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListUpcoming: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("partidos = %d, se esperaba 1", len(events))
	}
	if n := len(events[0].Markets[0].Selections); n != 3 {
		t.Errorf("selecciones = %d, se esperaban las 3 del partido", n)
	}
}
