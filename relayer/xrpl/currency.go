package xrpl

import (
	"encoding/hex"

	rippledata "github.com/rubblelabs/ripple/data"
)

// ConvertCurrencyToString decodes XRPL currency to string which matches the contract expectation.
func ConvertCurrencyToString(currency rippledata.Currency) string {
	currencyString := currency.String()
	if len(currencyString) == 3 {
		return currencyString
	}

	return hex.EncodeToString([]byte(currencyString))
}
