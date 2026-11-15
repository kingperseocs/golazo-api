package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/kingperseocs/golazo-api/internal/domain"
)

// errInvalidCursor se devuelve cuando el cursor que envía el cliente no es válido
var errInvalidCursor = errors.New("cursor inválido")

// cursorPayload es lo que va DENTRO del cursor. El cliente nunca lo ve así
type cursorPayload struct {
	CreatedAt time.Time `json:"t"`
	ID        string    `json:"id"`
}


// encodeCursor convierte la posición en un ctexto opaco para el cliente
func encodeCursor(c domain.TxCursor) (string, error) {
	raw, err := json.Marshal(cursorPayload{CreatedAt: c.CreatedAt, ID: c.ID})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// decodeCursor hace lo contrario del texto opaco a la posicion
func decodeCursor(s string) (domain.TxCursor, error) {
	raw , err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return domain.TxCursor{}, errInvalidCursor
	}

	var p cursorPayload

	if err := json.Unmarshal(raw, &p); err != nil {
		return domain.TxCursor{}, errInvalidCursor
	}

	if p.ID == "" || p.CreatedAt.IsZero() {
		return domain.TxCursor{}, errInvalidCursor
	}

	return domain.TxCursor{CreatedAt: p.CreatedAt, ID: p.ID}, nil
}