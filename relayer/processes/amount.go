package processes

import (
	"fmt"
	"math/big"

	sdkmath "cosmossdk.io/math"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

const (
	// XRPLAmountPrec is precision we use to covert float to float string for the amount representation.
	// That value is value which corelates with the min/max sending precision.
	XRPLAmountPrec = 16
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
		// format with exponent
		amountString := big.NewFloat(0).SetInt(coreumAmount.BigInt()).Text('g', XRPLAmountPrec)
		// we don't use the decimals for the XRP values since the `NewValue` function will do it automatically
		xrplValue, err := rippledata.NewValue(amountString, true)
		if err != nil {
			return rippledata.Amount{}, errors.Wrapf(
				err,
				"failed to convert amount string to ripple.Value, amount stirng: %s",
				amountString,
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
	tenPowerDec := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	floatAmount := big.NewFloat(0).SetRat(big.NewRat(0, 1).SetFrac(coreumAmount.BigInt(), tenPowerDec))
	// format with exponent
	amountString := fmt.Sprintf(
		"%s/%s/%s",
		floatAmount.Text('g', XRPLAmountPrec),
		currencyString,
		issuerString,
	)
	xrplValue, err := rippledata.NewValue(amountString, false)
	if err != nil {
		return rippledata.Amount{}, errors.Wrapf(
			err,
			"failed to convert amount string to ripple.Value, amount stirng: %s",
			amountString,
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
