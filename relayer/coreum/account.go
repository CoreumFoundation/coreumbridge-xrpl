package coreum

import (
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GenAccount generates random coreum account.
func GenAccount() sdk.AccAddress {
	return sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
}
