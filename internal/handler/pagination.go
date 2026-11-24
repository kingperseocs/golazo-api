package handler

import (
	"errors"
	"net/http"
	"strconv"
)

var errInvalidLimit = errors.New("limit debe ser un númerp entero mayor que 0")

// parseLimt lee ?limit= de la URL. Sin limit usa defaultPageSize, y nunca devuelve más de maxPageSize
func parseLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultPageSize, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, errInvalidLimit
	}
	return min(n, maxPageSize), nil
}
