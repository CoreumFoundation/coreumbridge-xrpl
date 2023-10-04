package testutils

import (
	"crypto/rand"

	rippledata "github.com/rubblelabs/ripple/data"
)

// GenXRPLAccount generates random XRPL account.
func GenXRPLAccount() rippledata.Account {
	var acc rippledata.Account
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	copy(acc[:], buf)
	return acc
}
