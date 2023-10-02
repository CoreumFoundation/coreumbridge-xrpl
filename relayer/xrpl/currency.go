package xrpl

import (
	"encoding/hex"

	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
)

// StringToHexXRPLCurrency decodes string currency to the Currency type.
func StringToHexXRPLCurrency(currencyString string) (rippledata.Currency, error) {
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
