package coreum

import (
	"math/big"

	sdkmath "cosmossdk.io/math"
)

// KeyringSuffix is used as suffix for coreum keyring.
const KeyringSuffix = "coreum"

// MaxContractAmount is max coins amount you can use for the wasm coin type.
// The value is ((2^128)-1) = 340282366920938463463374607431768211455.
var MaxContractAmount = sdkmath.NewIntFromBigInt(big.NewInt(0).Exp(big.NewInt(2), big.NewInt(128), nil)).SubRaw(1)
