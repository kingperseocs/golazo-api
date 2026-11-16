package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kingperseocs/golazo-api/internal/domain"
)

// fakeWalletService registra qué operación se llamó y devuelve el error indicado.
type fakeWalletService struct {
	called string
	err    error

	// Para el historial.
	items    []domain.WalletTransaction
	hasMore  bool
	gotLimit int
	gotAfter *domain.TxCursor
}

func (f *fakeWalletService) Deposit(ctx context.Context, userID, walletID string, amountCents int64, key string) (domain.WalletTransaction, bool, error) {
	f.called = "deposit"
	return domain.WalletTransaction{Type: domain.TxDeposit, AmountCents: amountCents}, true, f.err
}

func (f *fakeWalletService) Withdraw(ctx context.Context, userID, walletID string, amountCents int64, key string) (domain.WalletTransaction, bool, error) {
	f.called = "withdraw"
	return domain.WalletTransaction{Type: domain.TxWithdrawal, AmountCents: -amountCents}, true, f.err
}

func (f *fakeWalletService) MyWallet(ctx context.Context, userID string) (domain.Wallet, error) {
	return domain.Wallet{}, nil
}

func (f *fakeWalletService) MyTransactions(ctx context.Context, userID string, after *domain.TxCursor, limit int) ([]domain.WalletTransaction, bool, error) {
	f.gotLimit = limit
	f.gotAfter = after
	return f.items, f.hasMore, f.err
}

func TestWalletHandler_Movimientos(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name       string
		withdraw   bool
		err        error
		wantCalled string
		wantStatus int
		wantCode   string
	}{
		{"depósito llama a Deposit", false, nil, "deposit", http.StatusCreated, ""},
		{"retiro llama a Withdraw", true, nil, "withdraw", http.StatusCreated, ""},
		{"retiro sin saldo", true, domain.ErrInsufficientFunds, "withdraw", http.StatusUnprocessableEntity, "insufficient_funds"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeWalletService{err: tt.err}
			h := NewWalletHandler(svc, discard)

			req := httptest.NewRequest(http.MethodPost, "/wallets/w-1/x", strings.NewReader(`{"amount_cents":3000}`))
			req.Header.Set("Idempotency-Key", "k-1")
			req = req.WithContext(context.WithValue(req.Context(), userIDKey{}, "ana-id"))
			rec := httptest.NewRecorder()

			if tt.withdraw {
				h.Withdraw(rec, req)
			} else {
				h.Deposit(rec, req)
			}

			if svc.called != tt.wantCalled {
				t.Errorf("se llamó a %q, se esperaba %q", svc.called, tt.wantCalled)
			}
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, se esperaba %d", rec.Code, tt.wantStatus)
			}
			if tt.wantCode != "" {
				var body errorResponse
				if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
					t.Fatalf("leer respuesta: %v", err)
				}
				if body.Error.Code != tt.wantCode {
					t.Errorf("code = %q, se esperaba %q", body.Error.Code, tt.wantCode)
				}
			}
		})
	}
}

func TestWalletHandler_MyTransactions(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	last := domain.WalletTransaction{ID: "tx-2", CreatedAt: time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)}
	twoItems := []domain.WalletTransaction{{ID: "tx-1"}, last}
	validCursor, err := encodeCursor(domain.TxCursor{CreatedAt: last.CreatedAt, ID: last.ID})
	if err != nil {
		t.Fatalf("codificar cursor: %v", err)
	}

	tests := []struct {
		name          string
		query         string
		hasMore       bool
		wantStatus    int
		wantLimit     int
		wantAfter     bool
		wantNext      bool
		wantErrorCode string
	}{
		{"primera página por defecto", "", true, http.StatusOK, 20, false, true, ""},
		{"última página", "?limit=2", false, http.StatusOK, 2, false, false, ""},
		{"limit mayor que el máximo", "?limit=500", false, http.StatusOK, 100, false, false, ""},
		{"con cursor", "?cursor=" + validCursor, false, http.StatusOK, 20, true, false, ""},
		{"limit no numérico", "?limit=abc", false, http.StatusBadRequest, 0, false, false, "invalid_limit"},
		{"limit cero", "?limit=0", false, http.StatusBadRequest, 0, false, false, "invalid_limit"},
		{"cursor inválido", "?cursor=basura!!", false, http.StatusBadRequest, 0, false, false, "invalid_cursor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeWalletService{items: twoItems, hasMore: tt.hasMore}
			h := NewWalletHandler(svc, discard)

			req := httptest.NewRequest(http.MethodGet, "/wallets/me/transactions"+tt.query, nil)
			req = req.WithContext(context.WithValue(req.Context(), userIDKey{}, "ana-id"))
			rec := httptest.NewRecorder()

			h.MyTransactions(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, se esperaba %d", rec.Code, tt.wantStatus)
			}

			if tt.wantErrorCode != "" {
				var body errorResponse
				if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
					t.Fatalf("leer respuesta: %v", err)
				}
				if body.Error.Code != tt.wantErrorCode {
					t.Errorf("code = %q, se esperaba %q", body.Error.Code, tt.wantErrorCode)
				}
				return
			}

			if svc.gotLimit != tt.wantLimit {
				t.Errorf("limit = %d, se esperaba %d", svc.gotLimit, tt.wantLimit)
			}
			if (svc.gotAfter != nil) != tt.wantAfter {
				t.Errorf("¿llegó cursor al servicio? = %v, se esperaba %v", svc.gotAfter != nil, tt.wantAfter)
			}

			var page struct {
				Data       []map[string]any `json:"data"`
				NextCursor *string          `json:"next_cursor"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
				t.Fatalf("leer respuesta: %v", err)
			}
			if len(page.Data) != 2 {
				t.Errorf("data tiene %d elementos, se esperaban 2", len(page.Data))
			}
			if (page.NextCursor != nil) != tt.wantNext {
				t.Errorf("¿hay next_cursor? = %v, se esperaba %v", page.NextCursor != nil, tt.wantNext)
			}
			if tt.wantNext && *page.NextCursor != validCursor {
				t.Errorf("next_cursor no apunta al último movimiento de la página")
			}
		})
	}
}
