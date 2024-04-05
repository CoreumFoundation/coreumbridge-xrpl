//go:build integrationtests
// +build integrationtests

package integrationtests

import (
	"context"
	"flag"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
)

var chains Chains

// flag variables.
var (
	coreumCfg             CoreumChainConfig
	xrplCfg               XRPLChainConfig
	contractAddressString string
)

// Chains struct holds chains required for the testing.
type Chains struct {
	Coreum CoreumChain
	XRPL   XRPLChain
	Log    logger.Logger
}

//nolint:lll // breaking down cli flags will make it less readable.
func init() {
	flag.StringVar(&coreumCfg.GRPCAddress, "coreum-grpc-address", "localhost:9090", "GRPC address of cored node started by coreum")
	flag.StringVar(&coreumCfg.FundingMnemonic, "coreum-funding-mnemonic", "sad hobby filter tray ordinary gap half web cat hard call mystery describe member round trend friend beyond such clap frozen segment fan mistake", "Funding coreum account mnemonic required by tests")
	flag.StringVar(&coreumCfg.ContractPath, "coreum-contract-path", "../../contract/artifacts/coreumbridge_xrpl.wasm", "Path to smart contract bytecode")
	flag.StringVar(&coreumCfg.PreviousContractPath, "coreum-previous-contract-path", "../../bin/coreumbridge-xrpl-v1.1.0.wasm", "Path to previous smart contract bytecode")
	flag.StringVar(&xrplCfg.RPCAddress, "xrpl-rpc-address", "http://localhost:5005", "RPC address of xrpl node")
	flag.StringVar(&xrplCfg.FundingSeed, "xrpl-funding-seed", "snoPBrXtMeMyMHUVTgbuqAfg1SUTb", "Funding XRPL account seed required by tests")
	// this is the default address used in znet
	flag.StringVar(&contractAddressString, "contract-address", "devcore14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sd4f0ak", "Smart contract address of the bridge")

	// accept testing flags
	testing.Init()
	// parse additional flags
	flag.Parse()

	logCfg := logger.DefaultZapLoggerConfig()
	// set correct skip caller since we don't use the err counter wrapper here
	logCfg.CallerSkip = 1
	log, err := logger.NewZapLogger(logCfg)
	if err != nil {
		panic(errors.WithStack(err))
	}
	chains.Log = log

	coreumChain, err := NewCoreumChain(coreumCfg)
	if err != nil {
		panic(errors.Wrapf(err, "failed to init coreum chain"))
	}
	chains.Coreum = coreumChain

	xrplChain, err := NewXRPLChain(xrplCfg, chains.Log)
	if err != nil {
		panic(errors.Wrapf(err, "failed to init coreum chain"))
	}
	chains.XRPL = xrplChain
}

// NewTestingContext returns the configured coreum and xrpl chains and new context for the integration tests.
func NewTestingContext(t *testing.T) (context.Context, Chains) {
	testCtx, testCtxCancel := context.WithTimeout(context.Background(), 30*time.Minute)
	t.Cleanup(func() {
		require.NoError(t, testCtx.Err())
		testCtxCancel()
	})

	return testCtx, chains
}

// GetContractAddress returns the contract address for the bridge.
func GetContractAddress(t *testing.T) sdk.AccAddress {
	address, err := sdk.AccAddressFromBech32(contractAddressString)
	require.NoError(t, err)
	return address
}
