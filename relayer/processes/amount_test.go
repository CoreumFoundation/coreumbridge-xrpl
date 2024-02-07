package processes_test

import (
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/processes"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestConvertXRPLAmountToCoreumAmount(t *testing.T) {
	t.Parallel()

	var (
		fooIssuer   = xrpl.GenPrivKeyTxSigner().Account().String()
		fooCurrency = "FOO"
	)

	tests := []struct {
		name       string
		xrplAmount rippledata.Amount
		want       sdkmath.Int
		wantErr    error
	}{
		{
			name:       "one_XRPL_XRP_to_coreum_XRP",
			xrplAmount: amountStringToXRPLAmount(t, "1.0XRP"),
			want:       sdkmath.NewIntFromUint64(1_000_000),
		},
		{
			name:       "one_with_some_decimals_XRPL_XRP_to_coreum_XRP",
			xrplAmount: amountStringToXRPLAmount(t, "1.0001XRP"),
			want:       sdkmath.NewIntFromUint64(1000100),
		},
		{
			name:       "min_decimals_XRPL_XRP_to_coreum_XRP",
			xrplAmount: amountStringToXRPLAmount(t, "999.000001XRP"),
			want:       sdkmath.NewIntFromUint64(999000001),
		},
		{
			name:       "lower_than_min_decimals_XRPL_XRP_to_coreum_XRP",
			xrplAmount: amountStringToXRPLAmount(t, "0.0000001XRP"),
			want:       sdkmath.NewIntFromUint64(0),
		},
		{
			name:       "high_value_XRPL_XRP_to_coreum_XRP",
			xrplAmount: amountStringToXRPLAmount(t, "1000000000000.000001XRP"),
			want:       sdkmath.NewIntFromUint64(1000000000000000001),
		},
		{
			name:       "one_XRPL_FOO_to_coreum_FOO",
			xrplAmount: amountStringToXRPLAmount(t, fmt.Sprintf("1.0/%s/%s", fooCurrency, fooIssuer)),
			want:       stringToSDKInt(t, "1000000000000000"),
		},
		{
			name:       "one_with_some_decimals_XRPL_FOO_to_coreum_FOO",
			xrplAmount: amountStringToXRPLAmount(t, fmt.Sprintf("1.0000000001/%s/%s", fooCurrency, fooIssuer)),
			want:       sdkmath.NewIntFromUint64(1000000000100000),
		},
		{
			name:       "min_decimals_XRPL_FOO_to_coreum_FOO",
			xrplAmount: amountStringToXRPLAmount(t, fmt.Sprintf("0.000000000000001/%s/%s", fooCurrency, fooIssuer)),
			want:       sdkmath.NewIntFromUint64(1),
		},
		{
			name:       "high_value_XRPL_FOO_to_coreum_FOO",
			xrplAmount: amountStringToXRPLAmount(t, fmt.Sprintf("34e22/%s/%s", fooCurrency, fooIssuer)),
			want:       stringToSDKInt(t, "340000000000000000000000000000000000000"),
		},
		{
			name:       "invalid_foo_amount_contract_out_of_bound",
			xrplAmount: amountStringToXRPLAmount(t, fmt.Sprintf("34e23/%s/%s", fooCurrency, fooIssuer)),
			wantErr:    processes.ErrContractUint128OutOfBounds,
		},
		{
			name:       "invalid_foo_amount_sdkmath_out_of_bound",
			xrplAmount: amountStringToXRPLAmount(t, fmt.Sprintf("1e80/%s/%s", fooCurrency, fooIssuer)),
			wantErr:    processes.ErrSDKMathIntOutOfBounds,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := processes.ConvertXRPLAmountToCoreumAmount(tt.xrplAmount)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.want.String(), got.String())
		})
	}
}

func TestConvertCoreumAmountToXRPLAmount(t *testing.T) {
	t.Parallel()

	var (
		fooIssuer   = xrpl.GenPrivKeyTxSigner().Account().String()
		fooCurrency = "FOO"
	)

	tests := []struct {
		name         string
		coreumAmount sdkmath.Int
		issuer       string
		currency     string
		want         rippledata.Amount
		wantErr      bool
	}{
		{
			name:         "one_coreum_XRP_to_XRPL_XRP",
			coreumAmount: sdkmath.NewIntFromUint64(1_000_000),
			issuer:       xrpl.XRPTokenIssuer.String(),
			currency:     xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
			want:         amountStringToXRPLAmount(t, "1.0XRP"),
		},
		{
			name:         "one_with_some_decimals_coreum_XRP_to_XRPL_XRP",
			coreumAmount: sdkmath.NewIntFromUint64(1000101),
			issuer:       xrpl.XRPTokenIssuer.String(),
			currency:     xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
			want:         amountStringToXRPLAmount(t, "1.000101XRP"),
		},
		{
			name:         "min_decimals_coreum_XRP_to_XRPL_XRP",
			coreumAmount: sdkmath.NewIntFromUint64(999000001),
			issuer:       xrpl.XRPTokenIssuer.String(),
			currency:     xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
			want:         amountStringToXRPLAmount(t, "999.000001XRP"),
		},
		{
			name:         "high_value_coreum_XRP_to_XRPL_XRP",
			coreumAmount: sdkmath.NewIntFromUint64(1000000000000001),
			issuer:       xrpl.XRPTokenIssuer.String(),
			currency:     xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
			want:         amountStringToXRPLAmount(t, "1000000000.000001XRP"),
		},
		{
			name:         "one_coreum_FOO_to_XRPL_FOO",
			coreumAmount: sdkmath.NewIntFromUint64(1000000000000000),
			issuer:       fooIssuer,
			currency:     fooCurrency,
			want:         amountStringToXRPLAmount(t, fmt.Sprintf("1.0/%s/%s", fooCurrency, fooIssuer)),
		},
		{
			name:         "one_with_some_decimals_FOO_to_XRPL_FOO",
			coreumAmount: sdkmath.NewIntFromUint64(1000000000100000),
			issuer:       fooIssuer,
			currency:     fooCurrency,
			want:         amountStringToXRPLAmount(t, fmt.Sprintf("1.0000000001/%s/%s", fooCurrency, fooIssuer)),
		},
		{
			name:         "min_decimals_FOO_to_XRPL_FOO",
			coreumAmount: sdkmath.NewIntFromUint64(1),
			issuer:       fooIssuer,
			currency:     fooCurrency,
			want:         amountStringToXRPLAmount(t, fmt.Sprintf("0.000000000000001/%s/%s", fooCurrency, fooIssuer)),
		},
		{
			name:         "high_value_FOO_to_XRPL_FOO",
			coreumAmount: stringToSDKInt(t, "100000000000000000000000000000000000"),
			issuer:       fooIssuer,
			currency:     fooCurrency,
			want:         amountStringToXRPLAmount(t, fmt.Sprintf("1e20/%s/%s", fooCurrency, fooIssuer)),
		},
		{
			name:         "max_high_value_with_some_decimals_FOO_to_XRPL_FOO",
			coreumAmount: stringToSDKInt(t, "1000000000000001"),
			issuer:       fooIssuer,
			currency:     fooCurrency,
			want:         amountStringToXRPLAmount(t, fmt.Sprintf("1.000000000000001/%s/%s", fooCurrency, fooIssuer)),
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := processes.ConvertCoreumAmountToXRPLAmount(tt.coreumAmount, tt.issuer, tt.currency)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.want.String(), got.String())
		})
	}
}

func amountStringToXRPLAmount(t *testing.T, amountString string) rippledata.Amount {
	t.Helper()

	amount, err := rippledata.NewAmount(amountString)
	require.NoError(t, err)

	return *amount
}

func stringToSDKInt(t *testing.T, stringValue string) sdkmath.Int {
	intValue, ok := sdkmath.NewIntFromString(stringValue)
	require.True(t, ok)
	return intValue
}
