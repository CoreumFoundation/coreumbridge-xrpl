package processes_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/processes"
)

func TestConvertXRPLNativeTokenAmountToCoreum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		xrplAmount rippledata.Amount
		want       sdkmath.Int
		wantErr    bool
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
			wantErr:    false,
		},
		{
			name:       "min_decimals_XRPL_XRP_to_coreum_XRP",
			xrplAmount: amountStringToXRPLAmount(t, "999.000001XRP"),
			want:       sdkmath.NewIntFromUint64(999000001),
			wantErr:    false,
		},
		{
			name:       "lower_than_min_decimals_XRPL_XRP_to_coreum_XRP",
			xrplAmount: amountStringToXRPLAmount(t, "0.0000001XRP"),
			want:       sdkmath.NewIntFromUint64(0),
			wantErr:    false,
		},
		{
			name:       "high_value_XRPL_XRP_to_coreum_XRP",
			xrplAmount: amountStringToXRPLAmount(t, "1000000000000.000001XRP"),
			want:       sdkmath.NewIntFromUint64(1000000000000000001),
		},
		{
			name:       "one_XRPL_FOO_to_coreum_FOO",
			xrplAmount: amountStringToXRPLAmount(t, "1.0/FOO/rE6BWGaND13tXp8kzBVNhgcu1remuhmXk6"),
			want:       stringToSDKInt(t, "1000000000000000"),
		},
		{
			name:       "one_with_some_decimals_XRPL_FOO_to_coreum_FOO",
			xrplAmount: amountStringToXRPLAmount(t, "1.0000000001/FOO/rE6BWGaND13tXp8kzBVNhgcu1remuhmXk6"),
			want:       sdkmath.NewIntFromUint64(1000000000100000),
			wantErr:    false,
		},
		{
			name:       "min_decimals_XRPL_FOO_to_coreum_FOO",
			xrplAmount: amountStringToXRPLAmount(t, "0.000000000000001/FOO/rE6BWGaND13tXp8kzBVNhgcu1remuhmXk6"),
			want:       sdkmath.NewIntFromUint64(1),
			wantErr:    false,
		},
		{
			name:       "high_value_XRPL_FOO_to_coreum_FOO",
			xrplAmount: amountStringToXRPLAmount(t, "1.1e10/FOO/rE6BWGaND13tXp8kzBVNhgcu1remuhmXk6"),
			want:       stringToSDKInt(t, "11000000000000000000000000"),
			wantErr:    false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := processes.ConvertXRPLNativeTokenAmountToCoreum(tt.xrplAmount)
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
