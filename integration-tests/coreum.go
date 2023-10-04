package integrationtests

import (
	"context"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"

	"github.com/CoreumFoundation/coreum/v3/app"
	"github.com/CoreumFoundation/coreum/v3/pkg/client"
	"github.com/CoreumFoundation/coreum/v3/pkg/config"
	"github.com/CoreumFoundation/coreum/v3/pkg/config/constant"
	"github.com/CoreumFoundation/coreum/v3/testutil/integration"
	feemodeltypes "github.com/CoreumFoundation/coreum/v3/x/feemodel/types"
)

// CoreumChainConfig represents coreum chain config.
type CoreumChainConfig struct {
	GRPCAddress     string
	FundingMnemonic string
}

// CoreumChain is configured coreum chain.
type CoreumChain struct {
	cfg CoreumChainConfig
	integration.CoreumChain
}

// NewCoreumChain returns new instance of the coreum chain.
func NewCoreumChain(cfg CoreumChainConfig) (CoreumChain, error) {
	queryCtx, queryCtxCancel := context.WithTimeout(context.Background(), getTestContextConfig().TimeoutConfig.RequestTimeout)
	defer queryCtxCancel()

	coreumGRPCClient := integration.DialGRPCClient(cfg.GRPCAddress)
	coreumSettings := integration.QueryChainSettings(queryCtx, coreumGRPCClient)

	coreumClientCtx := client.NewContext(getTestContextConfig(), app.ModuleBasics).
		WithGRPCClient(coreumGRPCClient)

	coreumFeemodelParamsRes, err := feemodeltypes.NewQueryClient(coreumClientCtx).Params(queryCtx, &feemodeltypes.QueryParamsRequest{})
	if err != nil {
		return CoreumChain{}, errors.WithStack(err)
	}
	coreumSettings.GasPrice = coreumFeemodelParamsRes.Params.Model.InitialGasPrice
	coreumSettings.CoinType = constant.CoinType

	config.SetSDKConfig(coreumSettings.AddressPrefix, constant.CoinType)

	return CoreumChain{
		cfg: coreumCfg,
		CoreumChain: integration.NewCoreumChain(integration.NewChain(
			coreumGRPCClient,
			nil,
			coreumSettings,
			cfg.FundingMnemonic),
			[]string{},
		),
	}, nil
}

// Config returns the chain config.
func (c CoreumChain) Config() CoreumChainConfig {
	return c.cfg
}

// GetAccountWithKeyName generates new coreum account saves it to the keyring and returns the account address and its key name.
func (c CoreumChain) GetAccountWithKeyName() (sdk.AccAddress, string) {
	address := c.GenAccount()
	keyRecord, err := c.ClientContext.Keyring().KeyByAddress(address)
	if err != nil {
		panic(errors.Wrapf(err, "faild to get key by address from the keyring"))
	}

	return address, keyRecord.Name
}

func getTestContextConfig() client.ContextConfig {
	cfg := client.DefaultContextConfig()
	cfg.TimeoutConfig.TxStatusPollInterval = 100 * time.Millisecond

	return cfg
}
