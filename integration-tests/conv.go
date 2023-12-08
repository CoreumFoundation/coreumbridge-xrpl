package integrationtests

import (
	"math/big"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

// ConvertStringWithDecimalsToSDKInt accepts the float string and returns the value equal to `value * 1e(tokenDecimals)` truncate to int.
//
//nolint:lll // TODO(dzmitryhil) linter length limit
func ConvertStringWithDecimalsToSDKInt(t *testing.T, stringValue string, tokenDecimals int64) sdkmath.Int {
	tenPowerDec := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(tokenDecimals), nil)
	valueRat, ok := big.NewRat(0, 1).SetString(stringValue)
	require.True(t, ok)
	valueRat = big.NewRat(0, 1).Mul(valueRat, big.NewRat(0, 1).SetFrac(tenPowerDec, big.NewInt(1)))
	valueBigInt := big.NewInt(0).Quo(valueRat.Num(), valueRat.Denom())

	return sdkmath.NewIntFromBigInt(valueBigInt)
}
