package runner

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"os"
	"runtime/debug"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	toolshttp "github.com/CoreumFoundation/coreum-tools/pkg/http"
	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	coreumapp "github.com/CoreumFoundation/coreum/v4/app"
	coreumchainclient "github.com/CoreumFoundation/coreum/v4/pkg/client"
	coreumchainconfig "github.com/CoreumFoundation/coreum/v4/pkg/config"
	coreumchainconstant "github.com/CoreumFoundation/coreum/v4/pkg/config/constant"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/metrics"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/processes"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/tracing"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

const (
	configVersion = "v1"
	// ConfigFileName is file name used for the relayer config.
	ConfigFileName = "relayer.yaml"
	// DefaultCoreumChainID is default chain id.
	DefaultCoreumChainID = coreumchainconstant.ChainIDMain
)

// Runner is relayer runner which aggregates all relayer components.
type Runner struct {
	cfg           Config
	log           logger.Logger
	components    Components
	metricsServer *metrics.Server

	xrplToCoreumProcess *processes.XRPLToCoreumProcess
	coreumToXRPLProcess *processes.CoreumToXRPLProcess
}

// NewRunner return new runner from the config.
func NewRunner(ctx context.Context, components Components, cfg Config) (*Runner, error) {
	if cfg.Coreum.Contract.ContractAddress == "" {
		return nil, errors.New("contract address is not configured")
	}

	coreumRelayerAddress, err := getAddressFromKeyring(components.CoreumClientCtx.Keyring(), cfg.Coreum.RelayerKeyName)
	if err != nil {
		return nil, err
	}

	// load the key form the XRPL KR to check that it exists, and to let the user give access to the KR
	_, err = components.XRPLKeyringTxSigner.Account(cfg.XRPL.MultiSignerKeyName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get key from the XRPL keyring, key name:%s", cfg.XRPL.MultiSignerKeyName)
	}

	contractConfig, err := components.CoreumContractClient.GetContractConfig(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get contract config for the runner intialization")
	}

	bridgeXRPLAddress, err := rippledata.NewAccountFromAddress(contractConfig.BridgeXRPLAddress)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get xrpl account from string, string:%s", contractConfig.BridgeXRPLAddress)
	}

	xrplScanner := xrpl.NewAccountScanner(xrpl.AccountScannerConfig{
		Account:           *bridgeXRPLAddress,
		RecentScanEnabled: cfg.XRPL.Scanner.RecentScanEnabled,
		RecentScanWindow:  cfg.XRPL.Scanner.RecentScanWindow,
		RepeatRecentScan:  cfg.XRPL.Scanner.RepeatRecentScan,
		FullScanEnabled:   cfg.XRPL.Scanner.FullScanEnabled,
		RepeatFullScan:    cfg.XRPL.Scanner.RepeatFullScan,
		RetryDelay:        cfg.XRPL.Scanner.RetryDelay,
	},
		components.Log,
		components.XRPLRPCClient,
		components.MetricsRegistry.XRPLAccountRecentHistoryScanLedgerIndexGauge,
		components.MetricsRegistry.XRPLAccountFullHistoryScanLedgerIndexGauge,
	)

	xrplToCoreumProcess, err := processes.NewXRPLToCoreumProcess(
		processes.XRPLToCoreumProcessConfig{
			BridgeXRPLAddress:    *bridgeXRPLAddress,
			RelayerCoreumAddress: coreumRelayerAddress,
		},
		components.Log,
		xrplScanner,
		components.CoreumContractClient,
	)
	if err != nil {
		return nil, err
	}

	coreumToXRPLProcess, err := processes.NewCoreumToXRPLProcess(
		processes.CoreumToXRPLProcessConfig{
			BridgeXRPLAddress:    *bridgeXRPLAddress,
			RelayerCoreumAddress: coreumRelayerAddress,
			XRPLTxSignerKeyName:  cfg.XRPL.MultiSignerKeyName,
			RepeatRecentScan:     true,
			RepeatDelay:          cfg.Processes.CoreumToXRPLProcess.RepeatDelay,
		},
		components.Log,
		components.CoreumContractClient,
		components.XRPLRPCClient,
		components.XRPLKeyringTxSigner,
	)
	if err != nil {
		return nil, err
	}

	metricsServerCfg := metrics.ServerConfig{
		ListenAddress: cfg.Metrics.Server.ListenAddress,
	}
	metricsServer := metrics.NewServer(metricsServerCfg, components.MetricsRegistry)

	return &Runner{
		cfg:           cfg,
		log:           components.Log,
		components:    components,
		metricsServer: metricsServer,

		xrplToCoreumProcess: xrplToCoreumProcess,
		coreumToXRPLProcess: coreumToXRPLProcess,
	}, nil
}

// Start starts runner.
func (r *Runner) Start(ctx context.Context) error {
	runnerProcesses := map[string]func(context.Context) error{
		"XRPL-to-Coreum": r.withRestartOnError(r.xrplToCoreumProcess.Start),
		"Coreum-to-XRPL": r.withRestartOnError(r.coreumToXRPLProcess.Start),
	}
	if r.cfg.Metrics.Enabled {
		runnerProcesses["metrics-server"] = r.metricsServer.Start
		runnerProcesses["metrics-periodic-collector"] = r.components.MetricsPeriodicCollector.Start
	}
	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for name, start := range runnerProcesses {
			name := name
			start := start
			spawn(name, parallel.Continue, func(ctx context.Context) error {
				ctx = tracing.WithTracingProcess(ctx, name)
				return start(ctx)
			})
		}
		return nil
	})
}

func (r *Runner) withRestartOnError(task parallel.Task) parallel.Task {
	return func(ctx context.Context) error {
		for {
			// start process and handle the panic

			err := func() (err error) {
				defer func() {
					if p := recover(); p != nil {
						err = errors.Wrap(parallel.ErrPanic{Value: p, Stack: debug.Stack()}, "handled panic")
					}
				}()
				return task(ctx)
			}()

			if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}

			// restart the process if it is restartable

			r.log.Error(ctx, "Received unexpected error from the process", zap.Error(err))
			if r.cfg.Processes.ExitOnError {
				r.log.Warn(ctx, "The process is not auto-restartable on error")
				return err
			}
			r.log.Info(ctx, "Restarting process after the error")
		}
	}
}

// Components groups components required by runner.
type Components struct {
	Log                      logger.Logger
	MetricsRegistry          *metrics.Registry
	MetricsPeriodicCollector *metrics.PeriodicCollector
	RunnerConfig             Config
	XRPLClientCtx            coreumchainclient.Context
	XRPLRPCClient            *xrpl.RPCClient
	XRPLKeyringTxSigner      *xrpl.KeyringTxSigner
	CoreumClientCtx          coreumchainclient.Context
	CoreumContractClient     *coreum.ContractClient
}

// NewComponents creates components required by runner and other CLI commands.
func NewComponents(
	cfg Config,
	xrplKeyring keyring.Keyring,
	coreumKeyring keyring.Keyring,
	log logger.Logger,
) (Components, error) {
	metricsRegistry := metrics.NewRegistry()
	log, err := logger.WithErrorCounterMetric(log, metricsRegistry.ErrorCounter)
	if err != nil {
		return Components{}, err
	}

	retryableXRPLRPCHTTPClient := toolshttp.NewRetryableClient(toolshttp.RetryableClientConfig(cfg.XRPL.HTTPClient))

	xrplRPCClientCfg := xrpl.RPCClientConfig(cfg.XRPL.RPC)
	xrplRPCClient := xrpl.NewRPCClient(xrplRPCClientCfg, log, retryableXRPLRPCHTTPClient)

	coreumClientContextCfg := coreumchainclient.DefaultContextConfig()
	coreumClientContextCfg.TimeoutConfig.RequestTimeout = cfg.Coreum.Contract.RequestTimeout
	coreumClientContextCfg.TimeoutConfig.TxTimeout = cfg.Coreum.Contract.TxTimeout
	coreumClientContextCfg.TimeoutConfig.TxStatusPollInterval = cfg.Coreum.Contract.TxStatusPollInterval

	clientCtx := coreumchainclient.NewContext(coreumClientContextCfg, coreumapp.ModuleBasics).
		WithInput(os.Stdin)
	if cfg.Coreum.Network.ChainID != "" {
		coreumChainNetworkConfig, err := coreumchainconfig.NetworkConfigByChainID(
			coreumchainconstant.ChainID(cfg.Coreum.Network.ChainID),
		)
		if err != nil {
			return Components{}, errors.Wrapf(
				err,
				"failed to set get correum network config for the chainID, chainID:%s",
				cfg.Coreum.Network.ChainID,
			)
		}
		clientCtx = clientCtx.WithChainID(cfg.Coreum.Network.ChainID)

		coreum.SetSDKConfig(coreumChainNetworkConfig.Provider.GetAddressPrefix())
	}

	var contractAddress sdk.AccAddress
	if cfg.Coreum.Contract.ContractAddress != "" {
		var err error
		contractAddress, err = sdk.AccAddressFromBech32(cfg.Coreum.Contract.ContractAddress)
		if err != nil {
			return Components{}, errors.Wrapf(
				err,
				"failed to decode contract address to sdk.AccAddress, address:%s",
				cfg.Coreum.Contract.ContractAddress,
			)
		}
	}
	contractClientCfg := coreum.ContractClientConfig{
		ContractAddress:       contractAddress,
		GasAdjustment:         cfg.Coreum.Contract.GasAdjustment,
		GasPriceAdjustment:    sdk.MustNewDecFromStr(fmt.Sprintf("%f", cfg.Coreum.Contract.GasPriceAdjustment)),
		PageLimit:             cfg.Coreum.Contract.PageLimit,
		OutOfGasRetryDelay:    cfg.Coreum.Contract.OutOfGasRetryDelay,
		OutOfGasRetryAttempts: cfg.Coreum.Contract.OutOfGasRetryAttempts,
	}

	if cfg.Coreum.GRPC.URL != "" {
		grpcClient, err := getGRPCClientConn(cfg.Coreum.GRPC.URL)
		if err != nil {
			return Components{}, errors.Wrapf(err, "failed to create coreum GRPC client, URL:%s", cfg.Coreum.GRPC.URL)
		}
		clientCtx = clientCtx.WithGRPCClient(grpcClient)
	}

	coreumClientCtx := clientCtx.WithKeyring(coreumKeyring).WithGenerateOnly(cfg.Coreum.GenerateOnly)
	contractClient := coreum.NewContractClient(contractClientCfg, log, coreumClientCtx)

	metricsPeriodicCollectorCfg := metrics.DefaultPeriodicCollectorConfig()
	metricsPeriodicCollectorCfg.RepeatDelay = cfg.Metrics.PeriodicCollector.RepeatDelay
	metricsPeriodicCollector := metrics.NewPeriodicCollector(
		metricsPeriodicCollectorCfg,
		log,
		metricsRegistry,
		xrplRPCClient,
		contractClient,
		coreumClientCtx,
	)

	var xrplKeyringTxSigner *xrpl.KeyringTxSigner
	if xrplKeyring != nil {
		xrplKeyringTxSigner = xrpl.NewKeyringTxSigner(xrplKeyring)
	}

	return Components{
		Log:                      log,
		RunnerConfig:             cfg,
		MetricsRegistry:          metricsRegistry,
		MetricsPeriodicCollector: metricsPeriodicCollector,
		XRPLClientCtx:            clientCtx.WithKeyring(xrplKeyring),
		XRPLRPCClient:            xrplRPCClient,
		XRPLKeyringTxSigner:      xrplKeyringTxSigner,
		CoreumClientCtx:          coreumClientCtx,
		CoreumContractClient:     contractClient,
	}, nil
}

func getAddressFromKeyring(kr keyring.Keyring, keyName string) (sdk.AccAddress, error) {
	keyRecord, err := kr.Key(keyName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get key from the keyring, key name:%s", keyName)
	}
	addr, err := keyRecord.GetAddress()
	if err != nil {
		return nil, errors.Wrapf(
			err,
			"failed to get address from keyring key recodr, key name:%s",
			keyName,
		)
	}
	return addr, nil
}

func getGRPCClientConn(grpcURL string) (*grpc.ClientConn, error) {
	parsedURL, err := url.Parse(grpcURL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse grpc URL")
	}

	encodingConfig := coreumchainconfig.NewEncodingConfig(coreumapp.ModuleBasics)
	pc, ok := encodingConfig.Codec.(codec.GRPCCodecProvider)
	if !ok {
		return nil, errors.New("failed to cast codec to codec.GRPCCodecProvider)")
	}

	host := parsedURL.Host

	// https - tls grpc
	if parsedURL.Scheme == "https" {
		grpcClient, err := grpc.Dial(
			host,
			grpc.WithDefaultCallOptions(grpc.ForceCodec(pc.GRPCCodec())),
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})),
		)
		if err != nil {
			return nil, errors.Wrap(err, "failed to dial grpc")
		}
		return grpcClient, nil
	}

	// handling of host:port URL without the protocol
	if host == "" {
		host = fmt.Sprintf("%s:%s", parsedURL.Scheme, parsedURL.Opaque)
	}
	// http - insecure
	grpcClient, err := grpc.Dial(
		host,
		grpc.WithDefaultCallOptions(grpc.ForceCodec(pc.GRPCCodec())),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, errors.Wrap(err, "failed to dial grpc")
	}

	return grpcClient, nil
}
