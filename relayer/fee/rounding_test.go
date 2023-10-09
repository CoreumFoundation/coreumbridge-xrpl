//go:build integrationtests
// +build integrationtests

package fee_test

import (
	"math/big"
	"testing"

	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"
)

const (
	// MinMinSendingDecimals - min decimals we allow to use for the truncation and rounding.
	MinMinSendingDecimals = -15
	// MaxMinSendingDecimals - max decimals we allow to use for the truncation and rounding.
	MaxMinSendingDecimals = 16
	// TransferRateDenominator - the rate denominator XRPL uses for the transfer.
	// e.g. transferRate of 1005000000 is equivalent to a transfer fee of 0.5%.
	TransferRateDenominator = int64(1000000000)
	// TransferRateDenominatorOnePercent for one percent fee.
	TransferRateDenominatorOnePercent = int64(1010000000)
)

func TestReceivedXRPLToCoreumAmount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		sendingValue        *rippledata.Value
		minSendingPrecision int
		bridgingFee         *big.Int
		tokenDecimals       int64
		wantReceivedValue   *big.Int
	}{
		{
			name:                "positive_decimals",
			sendingValue:        convertStringToRippleValue(t, "1111.001111"),
			minSendingPrecision: 3,
			tokenDecimals:       5,
			wantReceivedValue:   big.NewInt(111100100),
		},
		{
			name:                "positive_max_decimals",
			sendingValue:        convertStringToRippleValue(t, "1e-17"),
			minSendingPrecision: MaxMinSendingDecimals,
			tokenDecimals:       5,
			wantReceivedValue:   big.NewInt(0),
		},
		{
			name:                "positive_decimals_to_zero",
			sendingValue:        convertStringToRippleValue(t, "0.001111"),
			minSendingPrecision: 2,
			tokenDecimals:       5,
			wantReceivedValue:   big.NewInt(0),
		},
		{
			name:                "positive_decimals_with_zero_denominator",
			sendingValue:        convertStringToRippleValue(t, "1.0"),
			minSendingPrecision: 2,
			tokenDecimals:       5,
			wantReceivedValue:   big.NewInt(100000),
		},
		{
			name:                "positive_decimals_with_bridging_fee",
			sendingValue:        convertStringToRippleValue(t, "1111.001111"),
			minSendingPrecision: 3,
			bridgingFee:         big.NewInt(1000),
			tokenDecimals:       5,
			wantReceivedValue:   big.NewInt(111099100),
		},
		{
			name:                "zero_decimals",
			sendingValue:        convertStringToRippleValue(t, "1111.001111"),
			minSendingPrecision: 0,
			tokenDecimals:       5,
			wantReceivedValue:   big.NewInt(111100000),
		},
		{
			name:                "zero_decimals_to_zero",
			sendingValue:        convertStringToRippleValue(t, "0.001111"),
			minSendingPrecision: 0,
			tokenDecimals:       5,
			wantReceivedValue:   big.NewInt(0),
		},
		{
			name:                "zero_decimals_with_bridging_fee",
			sendingValue:        convertStringToRippleValue(t, "1111.001111"),
			minSendingPrecision: 0,
			bridgingFee:         big.NewInt(1000),
			tokenDecimals:       5,
			wantReceivedValue:   big.NewInt(111099000),
		},
		{
			name:                "negative_decimals",
			sendingValue:        convertStringToRippleValue(t, "1111.001111"),
			minSendingPrecision: -2,
			tokenDecimals:       5,
			wantReceivedValue:   big.NewInt(110000000),
		},
		{
			name:                "negative_decimals_to_zero",
			sendingValue:        convertStringToRippleValue(t, "1111.001111"),
			minSendingPrecision: -5,
			tokenDecimals:       5,
			wantReceivedValue:   big.NewInt(0),
		},
		{
			name:                "negative_decimals_with_bridging_fee",
			sendingValue:        convertStringToRippleValue(t, "1111.001111"),
			minSendingPrecision: -2,
			bridgingFee:         big.NewInt(1000),
			tokenDecimals:       5,
			wantReceivedValue:   big.NewInt(109999000),
		},
		{
			name:                "negative_min_decimals",
			sendingValue:        convertStringToRippleValue(t, "1111111121321111.0"),
			minSendingPrecision: MinMinSendingDecimals,
			tokenDecimals:       5,
			wantReceivedValue:   convertStringToBigInt(t, "100000000000000000000"),
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bridgingFee := tt.bridgingFee
			if bridgingFee == nil {
				bridgingFee = big.NewInt(0)
			}

			receivedValue := computeReceivedTransferAmountFromXRPLToCoreum(
				tt.sendingValue,
				tt.minSendingPrecision,
				bridgingFee, tt.tokenDecimals)

			require.Equal(t, tt.wantReceivedValue.String(), receivedValue.String())
		})
	}
}

func TestReceivedCoreumToXRPLAmount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                                      string
		sendingValue                              *big.Int
		minSendingPrecision                       int
		bridgingFee                               *big.Int
		transferRate                              int64
		tokenDecimals                             int64
		wantReceivedValue                         *rippledata.Value
		wantAllocatedOnTheAccountToPayTransferFee *rippledata.Value
	}{
		{
			name:                "positive_decimals",
			sendingValue:        big.NewInt(1111001111),
			minSendingPrecision: 3,
			tokenDecimals:       5,
			wantReceivedValue:   convertStringToRippleValue(t, "11110.011"),
		},
		{
			name:                "positive_decimals_to_zero",
			sendingValue:        big.NewInt(111),
			minSendingPrecision: 1,
			tokenDecimals:       5,
			wantReceivedValue:   convertStringToRippleValue(t, "0"),
		},
		{
			name:                "positive_decimals_with_bridging_fee",
			sendingValue:        big.NewInt(1111001111),
			minSendingPrecision: 3,
			tokenDecimals:       5,
			bridgingFee:         big.NewInt(100),
			wantReceivedValue:   convertStringToRippleValue(t, "11110.01"),
		},
		{
			name:                "positive_decimals_with_bridging_fee_and_transfer_fee",
			sendingValue:        big.NewInt(1111001111),
			minSendingPrecision: 3,
			tokenDecimals:       5,
			bridgingFee:         big.NewInt(100),
			transferRate:        TransferRateDenominatorOnePercent,
			wantReceivedValue:   convertStringToRippleValue(t, "10998.909"),
		},
		{
			name:                "negative_decimals_with_bridging_fee_and_transfer_fee",
			sendingValue:        big.NewInt(1111001111),
			minSendingPrecision: -2,
			tokenDecimals:       5,
			bridgingFee:         big.NewInt(100),
			transferRate:        TransferRateDenominatorOnePercent,
			wantReceivedValue:   convertStringToRippleValue(t, "10900"),
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bridgingFee := big.NewInt(0)
			if tt.bridgingFee != nil {
				bridgingFee = tt.bridgingFee
			}

			transferRate := TransferRateDenominator
			if tt.transferRate != 0 {
				transferRate = tt.transferRate
			}

			receivedValue, allocatedOnTheAccountToPayTransferFee := computeReceivedTransferAmountsWithFeesFromCoreumToXRPL(
				t,
				tt.sendingValue,
				tt.minSendingPrecision,
				transferRate,
				bridgingFee,
				tt.tokenDecimals,
			)

			require.Equal(t, tt.wantReceivedValue.String(), receivedValue.String())
			if tt.wantAllocatedOnTheAccountToPayTransferFee != nil {
				require.Equal(t, tt.wantAllocatedOnTheAccountToPayTransferFee.String(), allocatedOnTheAccountToPayTransferFee.String())
			}
		})
	}
}

func computeReceivedTransferAmountFromXRPLToCoreum(
	value *rippledata.Value,
	minSendingPrecision int,
	bridgingFee *big.Int,
	tokenDecimals int64,
) *big.Int {
	truncatedValue := truncateRatByMinSendingPrecision(value.Rat(), minSendingPrecision)
	// use token decimals to convert to int
	tenPowerDec := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(tokenDecimals), nil)
	return big.NewInt(0).Sub(
		big.NewInt(0).Quo(big.NewInt(0).Mul(truncatedValue.Num(), tenPowerDec), truncatedValue.Denom()),
		bridgingFee,
	)
}

func computeReceivedTransferAmountsWithFeesFromCoreumToXRPL(
	t *testing.T,
	value *big.Int,
	minSendingPrecision int,
	transferRate int64,
	bridgingFee *big.Int,
	tokenDecimals int64,
) (*rippledata.Value, *rippledata.Value) {
	tenPowerDec := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(tokenDecimals), nil)
	valueRat := big.NewRat(0, 1).SetFrac(value, tenPowerDec)
	transferFeeRate := big.NewRat(0, 1).SetFrac(big.NewInt(transferRate), big.NewInt(TransferRateDenominator))
	// value - ((value * transfer fee rate) - val)
	allocatedOnTheAccountToPayTransferFeeRat := big.NewRat(0, 1).Sub(big.NewRat(0, 1).Mul(valueRat, transferFeeRate), valueRat)
	ratValueWithoutTransferFee := big.NewRat(0, 1).Sub(valueRat, allocatedOnTheAccountToPayTransferFeeRat)

	bridgingFeeRat := big.NewRat(0, 1).SetFrac(bridgingFee, tenPowerDec)
	amountWithoutAllFees := big.NewRat(0, 1).Sub(ratValueWithoutTransferFee, bridgingFeeRat)
	// we truncate here as a last calculation since we should be sure that the received amount is correctly rounded
	receivedAmountRat := truncateRatByMinSendingPrecision(amountWithoutAllFees, minSendingPrecision)

	var prec int
	if minSendingPrecision > 0 {
		prec = minSendingPrecision
	} else {
		prec = 0
	}
	receivedAmount, err := rippledata.NewValue((&big.Float{}).SetRat(receivedAmountRat).Text('f', prec), false)
	require.NoError(t, err)
	allocatedOnTheAccountToPayTransferFee, err := rippledata.NewValue((&big.Float{}).SetRat(allocatedOnTheAccountToPayTransferFeeRat).Text('f', prec), false)
	require.NoError(t, err)

	return receivedAmount, allocatedOnTheAccountToPayTransferFee
}

func truncateRatByMinSendingPrecision(ratValue *big.Rat, minSendingPrecision int) *big.Rat {
	nominator := ratValue.Num()
	denominator := ratValue.Denom()

	finalRat := big.NewRat(0, 1)
	switch {
	case minSendingPrecision > 0:
		tenPowerDec := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(int64(minSendingPrecision)), nil)
		// (nominator / (denominator / 10^minSendingPrecision) * (denominator / 10^minSendingPrecision) with denominator equal original
		subDenominator := big.NewInt(0).Quo(denominator, tenPowerDec)
		if subDenominator.Cmp(big.NewInt(0)) == 1 {
			updatedNominator := big.NewInt(0).Mul(
				big.NewInt(0).Quo(nominator, subDenominator),
				subDenominator)
			finalRat.SetFrac(updatedNominator, denominator)
		} else {
			finalRat.SetFrac(nominator, denominator)
		}
	case minSendingPrecision == 0:
		// nominator > denominator
		if nominator.Cmp(denominator) == 1 {
			updatedNominator := big.NewInt(0).Quo(nominator, denominator)
			finalRat.SetFrac(updatedNominator, big.NewInt(1))
		}
		// else zero
	case minSendingPrecision < 0:
		// nominator > denominator
		if nominator.Cmp(denominator) == 1 {
			tenPowerDec := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(int64(-minSendingPrecision)), nil)
			// (nominator / denominator / 10^(-1*minSendingPrecision) * 10^(-1*minSendingPrecision) with denominator equal 1
			updatedNominator := big.NewInt(0).Mul(
				big.NewInt(0).Quo(
					big.NewInt(0).Quo(nominator, denominator), tenPowerDec),
				tenPowerDec)
			finalRat.SetFrac(updatedNominator, big.NewInt(1))
		}
		// else zero
	}

	return finalRat
}

func convertStringToRippleValue(t *testing.T, stringValue string) *rippledata.Value {
	value, err := rippledata.NewValue(stringValue, false)
	require.NoError(t, err)
	return value
}

func convertStringToBigInt(t *testing.T, stringValue string) *big.Int {
	v, ok := big.NewInt(0).SetString(stringValue, 0)
	require.True(t, ok)
	return v
}
