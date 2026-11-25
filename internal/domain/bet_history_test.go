package domain

import (
	"errors"
	"testing"
)

func TestParseBetListFilter(t *testing.T) {
	tests := []struct {
		raw     string
		want    BetListFilter
		wantErr error
	}{
		{"", BetsAll, nil},
		{"pending", BetsPending, nil},
		{"settled", BetsSettled, nil},
		{"won", "", ErrInvalidBetFilter},
		{"PENDING", "", ErrInvalidBetFilter},
	}
	for _, tt := range tests {
		got, err := ParseBetListFilter(tt.raw)
		if !errors.Is(err, tt.wantErr) || got != tt.want {
			t.Errorf("ParseBetListFilter(%q) = %q, %v; se esperaba %q, %v", tt.raw, got, err, tt.want, tt.wantErr)
		}
	}
}
