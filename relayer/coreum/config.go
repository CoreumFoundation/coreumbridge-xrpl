package coreum

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/CoreumFoundation/coreum/v4/pkg/config/constant"
)

// SetSDKConfig sets global Cosmos SDK config.
func SetSDKConfig(addressPrefix string) {
	config := sdk.GetConfig()

	// Set address & public key prefixes
	config.SetBech32PrefixForAccount(addressPrefix, addressPrefix+"pub")
	config.SetBech32PrefixForValidator(addressPrefix+"valoper", addressPrefix+"valoperpub")
	config.SetBech32PrefixForConsensusNode(addressPrefix+"valcons",
		addressPrefix+"valconspub")

	// Set BIP44 coin type corresponding to CORE
	config.SetCoinType(constant.CoinType)
}
