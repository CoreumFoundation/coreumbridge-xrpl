//go:build integrationtests
// +build integrationtests

package fee_test

import (
	"encoding/hex"
	"math/big"
	"math/rand"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/processes"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"
)

const (
	// MinSendingPrecision - min decimals we allow to use for the truncation and rounding.
	MinSendingPrecision = -15
	// MaxSendingPrecision - max decimals we allow to use for the truncation and rounding.
	MaxSendingPrecision = 15
	// TransferRateDenominator - the rate denominator XRPL uses for the transfer.
	// e.g. transferRate of 1005000000 is equivalent to a transfer fee of 0.5%.
	TransferRateDenominator = int64(1000000000)
	// TransferRateDenominatorOnePercent for one percent fee.
	TransferRateDenominatorOnePercent = int64(1010000000)
)

func TestReceivedXRPLToCoreumAmount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		sendingAmount      *rippledata.Value
		sendingPrecision   int
		bridgingFee        *big.Int
		tokenDecimals      int64
		wantReceivedAmount *big.Int
	}{
		{
			name:               "positive_precision",
			sendingAmount:      convertStringToRippleValue(t, "1111.001111"),
			sendingPrecision:   3,
			tokenDecimals:      5,
			wantReceivedAmount: big.NewInt(111100100),
		},
		{
			name:               "positive_max_precision",
			sendingAmount:      convertStringToRippleValue(t, "1e-17"),
			sendingPrecision:   MaxSendingPrecision,
			tokenDecimals:      6,
			wantReceivedAmount: big.NewInt(0),
		},
		{
			name:               "positive_precision_to_zero",
			sendingAmount:      convertStringToRippleValue(t, "0.001111"),
			sendingPrecision:   2,
			tokenDecimals:      7,
			wantReceivedAmount: big.NewInt(0),
		},
		{
			name:               "positive_precision_with_zero_denominator",
			sendingAmount:      convertStringToRippleValue(t, "1.0"),
			sendingPrecision:   2,
			tokenDecimals:      4,
			wantReceivedAmount: big.NewInt(10000),
		},
		{
			name:               "positive_precision_with_bridging_fee",
			sendingAmount:      convertStringToRippleValue(t, "1111.001111"),
			sendingPrecision:   3,
			bridgingFee:        big.NewInt(1000),
			tokenDecimals:      5,
			wantReceivedAmount: big.NewInt(111099100),
		},
		{
			name:               "zero_precision",
			sendingAmount:      convertStringToRippleValue(t, "1111.001111"),
			sendingPrecision:   0,
			tokenDecimals:      6,
			wantReceivedAmount: big.NewInt(1111000000),
		},
		{
			name:               "zero_precision_to_zero",
			sendingAmount:      convertStringToRippleValue(t, "0.001111"),
			sendingPrecision:   0,
			tokenDecimals:      5,
			wantReceivedAmount: big.NewInt(0),
		},
		{
			name:               "zero_precision_with_bridging_fee",
			sendingAmount:      convertStringToRippleValue(t, "1111.001111"),
			sendingPrecision:   0,
			bridgingFee:        big.NewInt(1000),
			tokenDecimals:      5,
			wantReceivedAmount: big.NewInt(111099000),
		},
		{
			name:               "negative_precision",
			sendingAmount:      convertStringToRippleValue(t, "1111.001111"),
			sendingPrecision:   -2,
			tokenDecimals:      5,
			wantReceivedAmount: big.NewInt(110000000),
		},
		{
			name:               "negative_precision_to_zero",
			sendingAmount:      convertStringToRippleValue(t, "1111.001111"),
			sendingPrecision:   -5,
			tokenDecimals:      5,
			wantReceivedAmount: big.NewInt(0),
		},
		{
			name:               "negative_precision_with_bridging_fee",
			sendingAmount:      convertStringToRippleValue(t, "1111.001111"),
			sendingPrecision:   -2,
			bridgingFee:        big.NewInt(1000),
			tokenDecimals:      5,
			wantReceivedAmount: big.NewInt(109999000),
		},
		{
			name:               "negative_min_precision",
			sendingAmount:      convertStringToRippleValue(t, "1111111121321111.0"),
			sendingPrecision:   MinSendingPrecision,
			tokenDecimals:      5,
			wantReceivedAmount: convertStringToBigInt(t, "100000000000000000000"),
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
				tt.sendingAmount,
				tt.sendingPrecision,
				bridgingFee, tt.tokenDecimals)

			require.Equal(t, tt.wantReceivedAmount.String(), receivedValue.String())
		})
	}
}

func TestReceivedCoreumToXRPLAmount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                                      string
		sendingValue                              *big.Int
		sendingPrecision                          int
		bridgingFee                               *big.Int
		transferRate                              int64
		tokenDecimals                             int64
		wantReceivedValue                         *rippledata.Value
		wantAllocatedOnTheAccountToPayTransferFee *rippledata.Value
	}{
		{
			name:              "positive_precision",
			sendingValue:      big.NewInt(1111001111),
			sendingPrecision:  3,
			tokenDecimals:     5,
			wantReceivedValue: convertStringToRippleValue(t, "11110.011"),
		},
		{
			name:              "positive_precision_to_zero",
			sendingValue:      big.NewInt(111),
			sendingPrecision:  1,
			tokenDecimals:     5,
			wantReceivedValue: convertStringToRippleValue(t, "0"),
		},
		{
			name:              "positive_precision_with_bridging_fee",
			sendingValue:      big.NewInt(1111001111),
			sendingPrecision:  3,
			tokenDecimals:     5,
			bridgingFee:       big.NewInt(100),
			wantReceivedValue: convertStringToRippleValue(t, "11110.01"),
		},
		{
			name:              "positive_precision_with_bridging_fee_and_transfer_fee",
			sendingValue:      big.NewInt(1111001111),
			sendingPrecision:  3,
			tokenDecimals:     5,
			bridgingFee:       big.NewInt(100),
			transferRate:      TransferRateDenominatorOnePercent,
			wantReceivedValue: convertStringToRippleValue(t, "10998.909"),
		},
		{
			name:              "negative_precision_with_bridging_fee_and_transfer_fee",
			sendingValue:      big.NewInt(1111001111),
			sendingPrecision:  -2,
			tokenDecimals:     5,
			bridgingFee:       big.NewInt(100),
			transferRate:      TransferRateDenominatorOnePercent,
			wantReceivedValue: convertStringToRippleValue(t, "10900"),
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
				tt.sendingPrecision,
				transferRate,
				bridgingFee,
				tt.tokenDecimals,
			)

			require.Equal(t, tt.wantReceivedValue.String(), receivedValue.String())
			if tt.wantAllocatedOnTheAccountToPayTransferFee != nil {
				require.Equal(
					t,
					tt.wantAllocatedOnTheAccountToPayTransferFee.String(),
					allocatedOnTheAccountToPayTransferFee.String(),
				)
			}
		})
	}
}

func FuzzAmountConversionCoreumToXRPLAndBack(f *testing.F) {
	f.Fuzz(func(t *testing.T, amount uint64, randomizerSeed int64) {
		initial := sdkmath.NewIntFromUint64(amount)

		// init rand with seed
		rnd := rand.New(rand.NewSource(randomizerSeed))

		// randomize issuer
		issuerBytes := make([]byte, 20)
		rnd.Read(issuerBytes)
		issuer := rippledata.Account(issuerBytes).String()

		// randomize currency
		currencyByte, err := generateHex(rnd, 40)
		require.NoError(t, err)

		// convert to and back from xrpl
		rippleAmount, err := processes.ConvertXRPLOriginatedTokenCoreumAmountToXRPLAmount(
			initial,
			issuer,
			string(currencyByte),
		)
		require.NoError(t, err)
		coreumAmount, err := processes.ConvertXRPLAmountToCoreumAmount(rippleAmount)
		require.NoError(t, err)

		require.EqualValues(t, initial.String(), coreumAmount.String())
	})
}

func generateHex(rnd *rand.Rand, size int) (string, error) {
	bytes := make([]byte, size/2)
	if _, err := rnd.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func computeReceivedTransferAmountFromXRPLToCoreum(
	value *rippledata.Value,
	sendingPrecision int,
	bridgingFee *big.Int,
	tokenDecimals int64,
) *big.Int {
	truncatedValue := truncateRatBySendingPrecision(value.Rat(), sendingPrecision)
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
	sendingPrecision int,
	transferRate int64,
	bridgingFee *big.Int,
	tokenDecimals int64,
) (*rippledata.Value, *rippledata.Value) {
	tenPowerDec := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(tokenDecimals), nil)
	valueRat := big.NewRat(0, 1).SetFrac(value, tenPowerDec)
	transferFeeRate := big.NewRat(0, 1).SetFrac(big.NewInt(transferRate), big.NewInt(TransferRateDenominator))
	// value - ((value * transfer fee rate) - val)
	allocatedOnTheAccountToPayTransferFeeRat := big.
		NewRat(0, 1).
		Sub(big.NewRat(0, 1).Mul(valueRat, transferFeeRate), valueRat)
	ratValueWithoutTransferFee := big.NewRat(0, 1).Sub(valueRat, allocatedOnTheAccountToPayTransferFeeRat)

	bridgingFeeRat := big.NewRat(0, 1).SetFrac(bridgingFee, tenPowerDec)
	amountWithoutAllFees := big.NewRat(0, 1).Sub(ratValueWithoutTransferFee, bridgingFeeRat)
	// we truncate here as a last calculation since we should be sure that the received amount is correctly rounded
	receivedAmountRat := truncateRatBySendingPrecision(amountWithoutAllFees, sendingPrecision)

	var prec int
	if sendingPrecision > 0 {
		prec = sendingPrecision
	} else {
		prec = 0
	}
	receivedAmount, err := rippledata.NewValue((&big.Float{}).SetRat(receivedAmountRat).Text('f', prec), false)
	require.NoError(t, err)
	allocatedOnTheAccountToPayTransferFee, err := rippledata.NewValue(
		(&big.Float{}).SetRat(allocatedOnTheAccountToPayTransferFeeRat).Text('f', prec),
		false,
	)
	require.NoError(t, err)

	return receivedAmount, allocatedOnTheAccountToPayTransferFee
}

func truncateRatBySendingPrecision(ratValue *big.Rat, sendingPrecision int) *big.Rat {
	nominator := ratValue.Num()
	denominator := ratValue.Denom()

	finalRat := big.NewRat(0, 1)
	switch {
	case sendingPrecision > 0:
		tenPowerDec := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(int64(sendingPrecision)), nil)
		// (nominator / (denominator / 1e(sendingPrecision)) * (denominator / 1e(sendingPrecision))
		// with denominator equal original
		subDenominator := big.NewInt(0).Quo(denominator, tenPowerDec)
		if subDenominator.Cmp(big.NewInt(0)) == 1 {
			updatedNominator := big.NewInt(0).Mul(
				big.NewInt(0).Quo(nominator, subDenominator),
				subDenominator)
			finalRat.SetFrac(updatedNominator, denominator)
		} else {
			finalRat.SetFrac(nominator, denominator)
		}
	case sendingPrecision == 0:
		// nominator > denominator
		if nominator.Cmp(denominator) == 1 {
			updatedNominator := big.NewInt(0).Quo(nominator, denominator)
			finalRat.SetFrac(updatedNominator, big.NewInt(1))
		}
		// else zero
	case sendingPrecision < 0:
		// nominator > denominator
		if nominator.Cmp(denominator) == 1 {
			tenPowerDec := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(int64(-sendingPrecision)), nil)
			// (nominator / denominator / 1e(-1*sendingPrecision) * 1e(-1*sendingPrecision) with denominator equal 1
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
