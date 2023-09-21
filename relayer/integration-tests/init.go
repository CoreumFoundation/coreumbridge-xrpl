//go:build integrationtests
// +build integrationtests

package integrationtests

import (
	"context"
	"flag"
	"testing"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/CoreumFoundation/coreum-tools/pkg/http"
	"github.com/CoreumFoundation/coreum/v3/app"
	"github.com/CoreumFoundation/coreum/v3/pkg/client"
	"github.com/CoreumFoundation/coreum/v3/pkg/config"
	"github.com/CoreumFoundation/coreum/v3/pkg/config/constant"
	coreumtestutilintegration "github.com/CoreumFoundation/coreum/v3/testutil/integration"
	feemodeltypes "github.com/CoreumFoundation/coreum/v3/x/feemodel/types"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client/xrpl"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
)

var (
	ctx    context.Context
	chains Chains
)

// flag variables.
var (
	coreumGRPCAddress     string
	coreumFundingMnemonic string
	xrplRPCAddress        string
	xrplFundingSeed       string
)

// Chains struct holds chains required for the testing.
type Chains struct {
	Coreum coreumtestutilintegration.CoreumChain
	XRPL   XRPLChain
}

func init() {
	flag.StringVar(&coreumGRPCAddress, "coreum-grpc-address", "localhost:9090", "GRPC address of cored node started by coreumtestutilintegration")
	flag.StringVar(&coreumFundingMnemonic, "coreum-funding-mnemonic", "sad hobby filter tray ordinary gap half web cat hard call mystery describe member round trend friend beyond such clap frozen segment fan mistake", "Funding coreum account mnemonic required by tests")
	flag.StringVar(&xrplRPCAddress, "xrpl-rpc-address", "http://localhost:5005", "RPC address of xrpl node")
	flag.StringVar(&xrplFundingSeed, "xrpl-funding-seed", "snoPBrXtMeMyMHUVTgbuqAfg1SUTb", "Funding XRPL account seed required by tests")

	// accept testing flags
	testing.Init()
	// parse additional flags
	flag.Parse()

	ctx = context.Background()
	queryCtx, queryCtxCancel := context.WithTimeout(ctx, getTestContextConfig().TimeoutConfig.RequestTimeout)
	defer queryCtxCancel()

	// ********** Coreum **********

	coreumGRPCClient := coreumtestutilintegration.DialGRPCClient(coreumGRPCAddress)
	coreumSettings := coreumtestutilintegration.QueryChainSettings(queryCtx, coreumGRPCClient)

	coreumClientCtx := client.NewContext(getTestContextConfig(), app.ModuleBasics).
		WithGRPCClient(coreumGRPCClient)

	coreumFeemodelParamsRes, err := feemodeltypes.NewQueryClient(coreumClientCtx).Params(queryCtx, &feemodeltypes.QueryParamsRequest{})
	if err != nil {
		panic(errors.WithStack(err))
	}
	coreumSettings.GasPrice = coreumFeemodelParamsRes.Params.Model.InitialGasPrice
	coreumSettings.CoinType = constant.CoinType

	config.SetSDKConfig(coreumSettings.AddressPrefix, constant.CoinType)

	chains.Coreum = coreumtestutilintegration.NewCoreumChain(coreumtestutilintegration.NewChain(
		coreumGRPCClient,
		nil,
		coreumSettings,
		coreumFundingMnemonic), []string{})

	// ********** XRPL **********

	zapDevLogger, err := zap.NewDevelopment()
	if err != nil {
		panic(errors.WithStack(err))
	}

	rpcClient := xrpl.NewRPCClient(
		xrpl.DefaultRPCClientConfig(xrplRPCAddress),
		logger.NewZapLogger(zapDevLogger),
		http.NewRetryableClient(http.DefaultClientConfig()),
	)

	xrplChain, err := NewXRPLChain(xrplFundingSeed, rpcClient)
	if err != nil {
		panic(errors.WithStack(err))
	}

	chains.XRPL = xrplChain
}

// NewTestingContext returns the configured coreum and xrpl chains and new context for the integration tests.
func NewTestingContext(t *testing.T) (context.Context, Chains) {
	testCtx, testCtxCancel := context.WithCancel(ctx)
	t.Cleanup(testCtxCancel)

	return testCtx, chains
}

func getTestContextConfig() client.ContextConfig {
	cfg := client.DefaultContextConfig()
	cfg.TimeoutConfig.TxStatusPollInterval = 100 * time.Millisecond

	return cfg
}
