package processes

import (
	"math/big"

	sdkmath "cosmossdk.io/math"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

// ConvertXRPLAmountToCoreumAmount converts the XRPL native token amount from XRPL to coreum amount
// based on the currency type.
func ConvertXRPLAmountToCoreumAmount(xrplAmount rippledata.Amount) (sdkmath.Int, error) {
	if xrplAmount.Value == nil {
		return sdkmath.ZeroInt(), nil
	}
	xrplRatAmount := xrplAmount.Value.Rat()
	// native amount is represented as int value
	if xrplAmount.IsNative() {
		return sdkmath.NewIntFromBigInt(xrplRatAmount.Num()), nil
	}
	return convertXRPLAmountToCoreumAmountWithDecimals(xrplAmount, xrpl.XRPLIssuedTokenDecimals)
}

// ConvertXRPLOriginatedTokenCoreumAmountToXRPLAmount converts the XRPL originated token amount from
// coreum to XRPL amount based on the currency type.
func ConvertXRPLOriginatedTokenCoreumAmountToXRPLAmount(
	coreumAmount sdkmath.Int,
	issuerString,
	currencyString string,
) (rippledata.Amount, error) {
	if isXRPToken(issuerString, currencyString) {
		if !coreumAmount.IsInt64() {
			return rippledata.Amount{}, errors.Errorf(
				"failed to convert coreum XRP amount to int64, out of bound, value:%s", coreumAmount.String(),
			)
		}
		xrplValue, err := rippledata.NewNativeValue(coreumAmount.Int64())
		if err != nil {
			return rippledata.Amount{}, errors.Wrapf(
				err,
				"failed to convert int64 to ripple.Value, value: %d",
				coreumAmount.Int64(),
			)
		}

		return rippledata.Amount{
			Value: xrplValue,
		}, nil
	}

	return convertCoreumAmountToXRPLAmountWithDecimals(
		coreumAmount,
		xrpl.XRPLIssuedTokenDecimals,
		issuerString, currencyString,
	)
}

func convertXRPLAmountToCoreumAmountWithDecimals(xrplAmount rippledata.Amount, decimals uint32) (sdkmath.Int, error) {
	xrplRatAmount := xrplAmount.Value.Rat()
	// not XRP value is repressed as value multiplied by 1e15
	tenPowerDec := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	binIntAmount := big.NewInt(0).Quo(big.NewInt(0).Mul(tenPowerDec, xrplRatAmount.Num()), xrplRatAmount.Denom())
	if binIntAmount.BitLen() > sdkmath.MaxBitLen {
		return sdkmath.Int{}, errors.New("failed to convert big.Int to sdkmath.Int, out of bound")
	}

	return sdkmath.NewIntFromBigInt(binIntAmount), nil
}

func convertCoreumAmountToXRPLAmountWithDecimals(
	coreumAmount sdkmath.Int,
	decimals uint32,
	issuerString, currencyString string,
) (rippledata.Amount, error) {
	coreumAmountString := coreumAmount.String()
	offset := int64(0)
	for i := len(coreumAmountString) - 1; i >= 0; i-- {
		if string(coreumAmountString[i]) != "0" {
			break
		}
		offset++
	}
	intValue := coreumAmount.Quo(sdkmath.NewIntWithDecimal(1, int(offset)))
	if !intValue.IsInt64() {
		return rippledata.Amount{}, errors.Errorf(
			"failed to convert coreum XRPL currency amount to int64, out of bound, value:%s", intValue.String(),
		)
	}

	if len(coreumAmountString)-int(offset) > 16 {
		return rippledata.Amount{}, errors.Errorf(
			"maximum significant digits should not exceed 16, input number: %s", coreumAmountString,
		)
	}
	// include decimals to offset
	offset -= int64(decimals)
	xrplValue, err := rippledata.NewNonNativeValue(intValue.Int64(), offset)
	if err != nil {
		return rippledata.Amount{}, errors.Wrapf(
			err,
			"failed to convert int64 to ripple.Value, value: %d",
			intValue.Int64(),
		)
	}

	currency, err := rippledata.NewCurrency(currencyString)
	if err != nil {
		return rippledata.Amount{}, errors.Wrapf(
			err,
			"failed to convert currency to ripple.Currency, currency: %s",
			currencyString,
		)
	}
	issuer, err := rippledata.NewAccountFromAddress(issuerString)
	if err != nil {
		return rippledata.Amount{}, errors.Wrapf(
			err,
			"failed to convert issuer to ripple.Account, issuer: %s",
			issuerString,
		)
	}

	return rippledata.Amount{
		Value:    xrplValue,
		Currency: currency,
		Issuer:   *issuer,
	}, nil
}

func isXRPToken(issuer, currency string) bool {
	return issuer == xrpl.XRPTokenIssuer.String() && currency == xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency)
}
