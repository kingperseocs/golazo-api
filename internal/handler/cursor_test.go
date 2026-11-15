package handler

import (
	"errors"
	"testing"
	"time"

	"github.com/kingperseocs/golazo-api/internal/domain"
)

func TestCursor_IdaYVuelta(t *testing.T) {
	original := domain.TxCursor{
		CreatedAt: time.Date(2026, 11, 14, 22, 10, 23, 333919000, time.UTC),
		ID:        "26f4d7a4-a982-4ef5-8c4c-b0e2c84f8294",
	}

	encoded, err := encodeCursor(original)
	if err != nil {
		t.Fatalf("codificar: %v", err)
	}

	decoded, err := decodeCursor(encoded)
	if err != nil {
		t.Fatalf("decodificar: %v", err)
	}

	// time.Equal compara el instante, sin importar la zona horaria.
	if !decoded.CreatedAt.Equal(original.CreatedAt) || decoded.ID != original.ID {
		t.Errorf("cursor = %+v, se esperaba %+v", decoded, original)
	}
}

func TestCursor_Invalidos(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"no es base64", "esto no es base64!!"},
		// "hola" en base64: se decodifica, pero no es JSON.
		{"base64 pero no JSON", "aG9sYQ"},
		// {"t":"2026-11-14T00:00:00Z"} en base64: le falta el id.
		{"JSON sin id", "eyJ0IjoiMjAyNi0xMS0xNFQwMDowMDowMFoifQ"},
		{"vacío", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeCursor(tt.input); !errors.Is(err, errInvalidCursor) {
				t.Errorf("error = %v, se esperaba errInvalidCursor", err)
			}
		})
	}
}