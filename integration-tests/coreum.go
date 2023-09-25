package integrationtests

import (
	"context"
	"time"

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

// NewCoreumChain returns new instance of the coreum chain.
func NewCoreumChain(cfg CoreumChainConfig) (integration.CoreumChain, error) {
	queryCtx, queryCtxCancel := context.WithTimeout(context.Background(), getTestContextConfig().TimeoutConfig.RequestTimeout)
	defer queryCtxCancel()

	coreumGRPCClient := integration.DialGRPCClient(cfg.GRPCAddress)
	coreumSettings := integration.QueryChainSettings(queryCtx, coreumGRPCClient)

	coreumClientCtx := client.NewContext(getTestContextConfig(), app.ModuleBasics).
		WithGRPCClient(coreumGRPCClient)

	coreumFeemodelParamsRes, err := feemodeltypes.NewQueryClient(coreumClientCtx).Params(queryCtx, &feemodeltypes.QueryParamsRequest{})
	if err != nil {
		return integration.CoreumChain{}, errors.WithStack(err)
	}
	coreumSettings.GasPrice = coreumFeemodelParamsRes.Params.Model.InitialGasPrice
	coreumSettings.CoinType = constant.CoinType

	config.SetSDKConfig(coreumSettings.AddressPrefix, constant.CoinType)

	return integration.NewCoreumChain(integration.NewChain(
		coreumGRPCClient,
		nil,
		coreumSettings,
		cfg.FundingMnemonic), []string{}), nil
}

func getTestContextConfig() client.ContextConfig {
	cfg := client.DefaultContextConfig()
	cfg.TimeoutConfig.TxStatusPollInterval = 100 * time.Millisecond

	return cfg
}
