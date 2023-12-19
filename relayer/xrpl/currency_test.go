package xrpl_test

import (
	"encoding/hex"
	"strings"
	"testing"

	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestConvertCurrencyToString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		currency rippledata.Currency
		want     string
	}{
		{
			name:     "short_currency_upper",
			currency: mustCurrency(t, "ABC"),
			want:     "ABC",
		},
		{
			name:     "short_currency_lower",
			currency: mustCurrency(t, "abc"),
			want:     "abc",
		},
		{
			name:     "hex_full_length_currency",
			currency: mustCurrency(t, hex.EncodeToString([]byte(strings.Repeat("Z", 20)))),
			want:     "5A5A5A5A5A5A5A5A5A5A5A5A5A5A5A5A5A5A5A5A",
		},
		{
			name:     "tailing_zero_currency",
			currency: mustCurrency(t, "636f7265756d3939663062653133663900000000"),
			want:     "636F7265756D3939663062653133663900000000",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := xrpl.ConvertCurrencyToString(tt.currency)
			require.Equal(t, tt.want, got)
			// check that is convertable back
			currency, err := rippledata.NewCurrency(got)
			require.NoError(t, err)
			require.Equal(t, tt.currency.String(), currency.String())
		})
	}
}

func mustCurrency(t *testing.T, currencyString string) rippledata.Currency {
	currency, err := rippledata.NewCurrency(currencyString)
	require.NoError(t, err)
	return currency
}
