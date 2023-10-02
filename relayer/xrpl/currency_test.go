package xrpl_test

import (
	"testing"

	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestStringToHexCurrency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		currency string
		want     rippledata.Currency
		wantErr  bool
	}{
		{
			name:     "positive_long_currency",
			currency: "ABCDEFGHIJ1234567890",
		},
		{
			name:     "medium_currency",
			currency: "ABCDE",
		},
		{
			name:     "short_currency",
			currency: "ABC",
		},
		{
			name:     "negative_too_long_currency",
			currency: "ABCDEFGHIJ1234567890X",
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := xrpl.StringToHexXRPLCurrency(tt.currency)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.Equal(t, tt.currency, got.String())
		})
	}
}
