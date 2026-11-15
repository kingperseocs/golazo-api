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

	"github.com/kingperseocs/golazo-api/internal/domain"
)

// fakeWalletService registra qué operación se llamó y devuelve el error indicado.
type fakeWalletService struct {
	called string
	err error
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