package runner

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"runtime/debug"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/CosmWasm/wasmd/x/wasm"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	toolshttp "github.com/CoreumFoundation/coreum-tools/pkg/http"
	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	coreumchainclient "github.com/CoreumFoundation/coreum/v5/pkg/client"
	coreumchainconfig "github.com/CoreumFoundation/coreum/v5/pkg/config"
	coreumchainconstant "github.com/CoreumFoundation/coreum/v5/pkg/config/constant"
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
		return nil, errors.Wrapf(err, "failed to get contract config for the runner initialization")
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
		components.MetricsRegistry,
	)

	xrplToCoreumProcess, err := processes.NewXRPLToCoreumProcess(
		processes.XRPLToCoreumProcessConfig{
			BridgeXRPLAddress:    *bridgeXRPLAddress,
			RelayerCoreumAddress: coreumRelayerAddress,
		},
		components.Log,
		xrplScanner,
		components.CoreumContractClient,
		components.MetricsRegistry,
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
		components.MetricsRegistry,
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
		"XRPL-to-Coreum": taskWithRestartOnError(
			r.xrplToCoreumProcess.Start,
			r.log,
			r.cfg.Processes.ExitOnError,
			r.cfg.Processes.RetryDelay,
		),
		"Coreum-to-XRPL": taskWithRestartOnError(
			r.coreumToXRPLProcess.Start,
			r.log,
			r.cfg.Processes.ExitOnError,
			r.cfg.Processes.RetryDelay,
		),
	}
	if r.cfg.Metrics.Enabled {
		runnerProcesses["metrics-server"] = r.metricsServer.Start
		runnerProcesses["metrics-periodic-collector"] = r.components.MetricsPeriodicCollector.Start
	}
	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for name, start := range runnerProcesses {
			spawn(name, parallel.Continue, func(ctx context.Context) error {
				ctx = tracing.WithTracingProcess(ctx, name)
				return start(ctx)
			})
		}
		return nil
	})
}

func taskWithRestartOnError(
	task parallel.Task,
	log logger.Logger,
	exitOnError bool,
	retryDelay time.Duration,
) parallel.Task {
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
			log.Error(ctx, "Received unexpected error from the process", zap.Error(err))
			if exitOnError {
				log.Warn(ctx, "The process is not auto-restartable on error")
				return err
			}

			if retryDelay > 0 {
				log.Info(ctx,
					"Process is paused and will be restarted later",
					zap.Duration("retryDelay", retryDelay))
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(retryDelay):
				}
			}
			log.Info(ctx, "Restarting process after the error")
		}
	}
}

// Components groups components required by runner.
type Components struct {
	Log                      logger.Logger
	MetricsRegistry          *metrics.Registry
	MetricsPeriodicCollector *metrics.PeriodicCollector
	RunnerConfig             Config
	XRPLSDKClietCtx          client.Context
	XRPLRPCClient            *xrpl.RPCClient
	XRPLKeyringTxSigner      *xrpl.KeyringTxSigner
	CoreumSDKClientCtx       client.Context
	CoreumClientCtx          coreumchainclient.Context
	CoreumContractClient     *coreum.ContractClient
}

// NewComponents creates components required by runner and other CLI commands.
func NewComponents(
	cfg Config, xrplSDKClientCtx, coreumSDKClientCtx client.Context, log logger.Logger,
) (Components, error) {
	metricsRegistry := metrics.NewRegistry()
	log, err := logger.WithMetrics(log, metricsRegistry)
	if err != nil {
		return Components{}, err
	}

	retryableXRPLRPCHTTPClient := toolshttp.NewRetryableClient(toolshttp.RetryableClientConfig(cfg.XRPL.HTTPClient))

	xrplRPCClientCfg := xrpl.RPCClientConfig(cfg.XRPL.RPC)
	xrplRPCClient := xrpl.NewRPCClient(xrplRPCClientCfg, log, retryableXRPLRPCHTTPClient, metricsRegistry)

	coreumClientContextCfg := coreumchainclient.DefaultContextConfig()
	coreumClientContextCfg.TimeoutConfig.RequestTimeout = cfg.Coreum.Contract.RequestTimeout
	coreumClientContextCfg.TimeoutConfig.TxTimeout = cfg.Coreum.Contract.TxTimeout
	coreumClientContextCfg.TimeoutConfig.TxStatusPollInterval = cfg.Coreum.Contract.TxStatusPollInterval

	coreumClientCtx := coreumchainclient.NewContext(
		coreumClientContextCfg, auth.AppModuleBasic{}, wasm.AppModuleBasic{},
	).
		WithKeyring(coreumSDKClientCtx.Keyring).
		WithGenerateOnly(coreumSDKClientCtx.GenerateOnly).
		WithFromAddress(coreumSDKClientCtx.FromAddress).
		WithUnsignedSimulation(true)

	if cfg.Coreum.Network.ChainID != "" {
		coreumChainNetworkConfig, err := coreumchainconfig.NetworkConfigByChainID(
			coreumchainconstant.ChainID(cfg.Coreum.Network.ChainID),
		)
		if err != nil {
			return Components{}, errors.Wrapf(
				err, "failed to set get correum network config for the chainID, chainID:%s",
				cfg.Coreum.Network.ChainID,
			)
		}
		coreumClientCtx = coreumClientCtx.WithChainID(cfg.Coreum.Network.ChainID)
		coreum.SetSDKConfig(coreumChainNetworkConfig.Provider.GetAddressPrefix())
	}

	var contractAddress sdk.AccAddress
	if cfg.Coreum.Contract.ContractAddress != "" {
		var err error
		contractAddress, err = sdk.AccAddressFromBech32(cfg.Coreum.Contract.ContractAddress)
		if err != nil {
			return Components{}, errors.Wrapf(
				err, "failed to decode contract address to sdk.AccAddress, address:%s",
				cfg.Coreum.Contract.ContractAddress,
			)
		}
	}
	contractClientCfg := coreum.DefaultContractClientConfig(contractAddress)
	contractClientCfg.GasAdjustment = cfg.Coreum.Contract.GasAdjustment
	contractClientCfg.GasPriceAdjustment = sdkmath.LegacyMustNewDecFromStr(
		fmt.Sprintf("%f", cfg.Coreum.Contract.GasPriceAdjustment),
	)
	contractClientCfg.PageLimit = cfg.Coreum.Contract.PageLimit
	contractClientCfg.OutOfGasRetryDelay = cfg.Coreum.Contract.OutOfGasRetryDelay
	contractClientCfg.OutOfGasRetryAttempts = cfg.Coreum.Contract.OutOfGasRetryAttempts

	if cfg.Coreum.GRPC.URL != "" {
		grpcClient, err := getGRPCClientConn(cfg.Coreum.GRPC.URL)
		if err != nil {
			return Components{}, errors.Wrapf(err, "failed to create coreum GRPC client, URL:%s", cfg.Coreum.GRPC.URL)
		}
		coreumClientCtx = coreumClientCtx.WithGRPCClient(grpcClient)
	}

	contractClient := coreum.NewContractClient(contractClientCfg, log, coreumClientCtx)

	metricsPeriodicCollectorCfg := metrics.DefaultPeriodicCollectorConfig()
	metricsPeriodicCollectorCfg.RepeatDelay = cfg.Metrics.PeriodicCollector.RepeatDelay
	metricsPeriodicCollector := metrics.NewPeriodicCollector(
		metricsPeriodicCollectorCfg, log, metricsRegistry, xrplRPCClient, contractClient, coreumClientCtx,
	)

	var xrplKeyringTxSigner *xrpl.KeyringTxSigner
	if xrplSDKClientCtx.Keyring != nil {
		xrplKeyringTxSigner = xrpl.NewKeyringTxSigner(xrplSDKClientCtx.Keyring)
	}

	return Components{
		Log:                      log,
		RunnerConfig:             cfg,
		MetricsRegistry:          metricsRegistry,
		MetricsPeriodicCollector: metricsPeriodicCollector,
		XRPLSDKClietCtx:          xrplSDKClientCtx,
		XRPLRPCClient:            xrplRPCClient,
		XRPLKeyringTxSigner:      xrplKeyringTxSigner,
		CoreumSDKClientCtx:       coreumSDKClientCtx,
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

	encodingConfig := coreumchainconfig.NewEncodingConfig(auth.AppModuleBasic{}, wasm.AppModuleBasic{})
	pc, ok := encodingConfig.Codec.(codec.GRPCCodecProvider)
	if !ok {
		return nil, errors.New("failed to cast codec to codec.GRPCCodecProvider)")
	}

	host := parsedURL.Host

	// https - tls grpc
	if parsedURL.Scheme == "https" {
		grpcClient, err := grpc.NewClient(
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
	grpcClient, err := grpc.NewClient(
		host,
		grpc.WithDefaultCallOptions(grpc.ForceCodec(pc.GRPCCodec())),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, errors.Wrap(err, "failed to dial grpc")
	}

	return grpcClient, nil
}
