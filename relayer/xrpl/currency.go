package xrpl

import (
	"encoding/hex"
	"strings"

	rippledata "github.com/rubblelabs/ripple/data"
)

// ConvertCurrencyToString decodes XRPL currency to string which matches the contract expectation.
func ConvertCurrencyToString(currency rippledata.Currency) string {
	currencyString := currency.String()
	if len(currencyString) == 3 {
		return currencyString
	}
	hexString := hex.EncodeToString([]byte(currencyString))
	// append tailing zeros to match the contract expectation
	hexString += strings.Repeat("0", 40-len(hexString))
	return strings.ToUpper(hexString)
}
