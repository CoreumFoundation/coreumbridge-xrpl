package processes

import (
	sdkmath "cosmossdk.io/math"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
)

const (
	xrplCurrencyDecimals = 15
	xrplXRPDecimals      = 6
)

// ConvertXRPLNativeTokenAmountToCoreum converts the XRPL native token amount to coreum based on the currency type.
func ConvertXRPLNativeTokenAmountToCoreum(xrplAmount rippledata.Amount) (sdkmath.Int, error) {
	if xrplAmount.Value == nil {
		return sdkmath.ZeroInt(), nil
	}
	prec := xrplCurrencyDecimals
	// is token is XRP
	if xrplAmount.IsNative() {
		prec = xrplXRPDecimals
	}

	// by default the sdkmath.Dec uses 18 decimals as max, so if you plan to use more that logic must be changed
	floatString := xrplAmount.Value.Rat().FloatString(prec)
	rawAmount, err := sdkmath.LegacyNewDecFromStr(floatString)
	if err != nil {
		return sdkmath.Int{}, errors.Wrapf(err, "failed to convert XRPL amount to sdkmath.Dec")
	}
	// native amount is represented as int value
	if xrplAmount.IsNative() {
		return rawAmount.TruncateInt(), nil
	}
	// not native value is repressed as value multiby by -10^15
	return rawAmount.MulInt(sdkmath.NewIntWithDecimal(1, prec)).TruncateInt(), nil
}
