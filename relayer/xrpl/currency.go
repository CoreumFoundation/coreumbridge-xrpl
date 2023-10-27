package xrpl

import (
	"encoding/hex"

	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
)

// ConvertStringToHexXRPLCurrency decodes string currency to the Currency type.
// Deprecated: FunctionName is deprecated and will be removed soon, once we migrate to HEX string, and the rippledata.NewCurrency() will be used.
func ConvertStringToHexXRPLCurrency(currencyString string) (rippledata.Currency, error) {
	encodedCurrency := hex.EncodeToString([]byte(currencyString))
	if len(encodedCurrency) > 40 {
		return rippledata.Currency{}, errors.Errorf("failed to convert currency string to Currency, invalid decoded hex length")
	}

	decodedCurrency, err := hex.DecodeString(encodedCurrency)
	if err != nil {
		return rippledata.Currency{}, errors.Wrapf(err, "faild to create Currency type from hex string")
	}

	var currency rippledata.Currency
	copy(currency[:], decodedCurrency)

	return currency, nil
}

// ConvertCurrencyToString decodes XRPL currency to string which matches the contract expectation.
func ConvertCurrencyToString(currency rippledata.Currency) string {
	currencyString := currency.String()
	if len(currencyString) == 3 {
		return currencyString
	}

	return hex.EncodeToString([]byte(currencyString))
}
