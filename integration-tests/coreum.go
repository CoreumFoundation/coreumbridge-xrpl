package integrationtests

import (
	"context"
	"time"

	"github.com/pkg/errors"

	"github.com/CoreumFoundation/coreum/v4/app"
	"github.com/CoreumFoundation/coreum/v4/pkg/client"
	"github.com/CoreumFoundation/coreum/v4/pkg/config/constant"
	"github.com/CoreumFoundation/coreum/v4/testutil/integration"
	feemodeltypes "github.com/CoreumFoundation/coreum/v4/x/feemodel/types"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
)

// CoreumChainConfig represents coreum chain config.
type CoreumChainConfig struct {
	GRPCAddress          string
	FundingMnemonic      string
	ContractPath         string
	PreviousContractPath string
}

// CoreumChain is configured coreum chain.
type CoreumChain struct {
	cfg CoreumChainConfig
	integration.CoreumChain
}

// NewCoreumChain returns new instance of the coreum chain.
func NewCoreumChain(cfg CoreumChainConfig) (CoreumChain, error) {
	queryCtx, queryCtxCancel := context.WithTimeout(
		context.Background(),
		getTestContextConfig().TimeoutConfig.RequestTimeout,
	)
	defer queryCtxCancel()

	coreumGRPCClient, err := integration.DialGRPCClient(cfg.GRPCAddress)
	if err != nil {
		return CoreumChain{}, errors.WithStack(err)
	}
	coreumSettings := integration.QueryChainSettings(queryCtx, coreumGRPCClient)

	coreumClientCtx := client.NewContext(getTestContextConfig(), app.ModuleBasics).
		WithGRPCClient(coreumGRPCClient)

	coreumFeemodelParamsRes, err := feemodeltypes.
		NewQueryClient(coreumClientCtx).
		Params(queryCtx, &feemodeltypes.QueryParamsRequest{})
	if err != nil {
		return CoreumChain{}, errors.WithStack(err)
	}
	coreumSettings.GasPrice = coreumFeemodelParamsRes.Params.Model.InitialGasPrice
	coreumSettings.CoinType = constant.CoinType

	coreum.SetSDKConfig(coreumSettings.AddressPrefix)

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

func getTestContextConfig() client.ContextConfig {
	cfg := client.DefaultContextConfig()
	cfg.TimeoutConfig.TxStatusPollInterval = 100 * time.Millisecond

	return cfg
}
