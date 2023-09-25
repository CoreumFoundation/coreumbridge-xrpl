//go:build integrationtests
// +build integrationtests

package integrationtests

import (
	"context"
	"flag"
	"testing"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	coreumintegration "github.com/CoreumFoundation/coreum/v3/testutil/integration"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
)

var chains Chains

// flag variables.
var (
	coreumCfg CoreumChainConfig
	xrplCfg   XRPLChainConfig
)

// Chains struct holds chains required for the testing.
type Chains struct {
	Coreum coreumintegration.CoreumChain
	XRPL   XRPLChain
	Log    logger.Logger
}

func init() {
	flag.StringVar(&coreumCfg.GRPCAddress, "coreum-grpc-address", "localhost:9090", "GRPC address of cored node started by coreum")
	flag.StringVar(&coreumCfg.FundingMnemonic, "coreum-funding-mnemonic", "sad hobby filter tray ordinary gap half web cat hard call mystery describe member round trend friend beyond such clap frozen segment fan mistake", "Funding coreum account mnemonic required by tests")
	flag.StringVar(&xrplCfg.RPCAddress, "xrpl-rpc-address", "http://localhost:5005", "RPC address of xrpl node")
	flag.StringVar(&xrplCfg.FundingSeed, "xrpl-funding-seed", "snoPBrXtMeMyMHUVTgbuqAfg1SUTb", "Funding XRPL account seed required by tests")

	// accept testing flags
	testing.Init()
	// parse additional flags
	flag.Parse()

	zapDevLogger, err := zap.NewDevelopment()
	if err != nil {
		panic(errors.WithStack(err))
	}
	log := logger.NewZapLogger(zapDevLogger)
	chains.Log = log

	coreumChain, err := NewCoreumChain(coreumCfg)
	if err != nil {
		panic(errors.Wrapf(err, "failed to init coreum chain"))
	}
	chains.Coreum = coreumChain

	xrplChain, err := NewXRPLChain(xrplCfg, log)
	if err != nil {
		panic(errors.Wrapf(err, "failed to init coreum chain"))
	}
	chains.XRPL = xrplChain
}

// NewTestingContext returns the configured coreum and xrpl chains and new context for the integration tests.
func NewTestingContext(t *testing.T) (context.Context, Chains) {
	testCtx, testCtxCancel := context.WithCancel(context.Background())
	t.Cleanup(testCtxCancel)

	return testCtx, chains
}
