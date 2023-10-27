package testutils

import (
	"crypto/rand"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

// GenXRPLPubKey generates random XRPL pub key.
func GenXRPLPubKey() rippledata.PublicKey {
	var pubKey rippledata.PublicKey
	copy(pubKey[:], ed25519.GenPrivKey().PubKey().Bytes())
	return pubKey
}

// GenXRPLSignature generates random XRPL signature.
func GenXRPLSignature() rippledata.VariableLength {
	var signature rippledata.VariableLength
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	copy(signature[:], ed25519.GenPrivKey().PubKey().Bytes())
	return signature
}

// GenCoreumAccount generates random coreum account.
func GenCoreumAccount() sdk.AccAddress {
	return sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
}
