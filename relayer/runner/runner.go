//nolint:tagliatelle // yaml naming
package runner

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"

	toolshttp "github.com/CoreumFoundation/coreum-tools/pkg/http"
	coreumapp "github.com/CoreumFoundation/coreum/v3/app"
	coreumchainclient "github.com/CoreumFoundation/coreum/v3/pkg/client"
	coreumchainconfig "github.com/CoreumFoundation/coreum/v3/pkg/config"
	coreumchainconstant "github.com/CoreumFoundation/coreum/v3/pkg/config/constant"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/processes"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

const (
	configVersion = "v1"
	// ConfigFileName is file name used for the relayer config.
	ConfigFileName = "relayer.yaml"

	defaultCoreumChainID = coreumchainconstant.ChainIDMain
)

// ******************** Config ********************

// LoggingConfig is logging config.
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// HTTPClientConfig is http client config.
type HTTPClientConfig struct {
	RequestTimeout time.Duration `yaml:"request_timeout"`
	DoTimeout      time.Duration `yaml:"do_timeout"`
	RetryDelay     time.Duration `yaml:"retry_delay"`
}

// XRPLRPCConfig is XRPL RPC config.
type XRPLRPCConfig struct {
	URL       string `yaml:"url"`
	PageLimit uint32 `yaml:"page_limit"`
}

// XRPLScannerConfig is XRPL scanner config.
type XRPLScannerConfig struct {
	RecentScanEnabled bool  `yaml:"recent_scan_enabled"`
	RecentScanWindow  int64 `yaml:"recent_scan_window"`
	RepeatRecentScan  bool  `yaml:"repeat_recent_scan"`

	FullScanEnabled bool `yaml:"full_scan_enabled"`
	RepeatFullScan  bool `yaml:"repeat_full_scan"`

	RetryDelay time.Duration `yaml:"retry_delay"`
}

// XRPLConfig is XRPL config.
type XRPLConfig struct {
	MultiSignerKeyName string            `yaml:"multi_signer_key_name"`
	HTTPClient         HTTPClientConfig  `yaml:"http_client"`
	RPC                XRPLRPCConfig     `yaml:"rpc"`
	Scanner            XRPLScannerConfig `yaml:"scanner"`
}

// CoreumGRPCConfig is coreum GRPC config.
type CoreumGRPCConfig struct {
	URL string `yaml:"url"`
}

// CoreumNetworkConfig is coreum network config.
type CoreumNetworkConfig struct {
	ChainID string `yaml:"chain_id"`
}

// CoreumContractConfig is coreum contract config.
type CoreumContractConfig struct {
	ContractAddress    string  `yaml:"contract_address"`
	GasAdjustment      float64 `yaml:"gas_adjustment"`
	GasPriceAdjustment float64 `yaml:"gas_price_adjustment"`
	PageLimit          uint32  `yaml:"page_limit"`
	// client context config
	RequestTimeout       time.Duration `yaml:"request_timeout"`
	TxTimeout            time.Duration `yaml:"tx_timeout"`
	TxStatusPollInterval time.Duration `yaml:"tx_status_poll_interval"`
}

// CoreumConfig is coreum config.
type CoreumConfig struct {
	RelayerKeyName string               `yaml:"relayer_key_name"`
	GRPC           CoreumGRPCConfig     `yaml:"grpc"`
	Network        CoreumNetworkConfig  `yaml:"network"`
	Contract       CoreumContractConfig `yaml:"contract"`
}

// XRPLTxSubmitterConfig is XRPLTxSubmitter config.
type XRPLTxSubmitterConfig struct {
	RepeatDelay time.Duration `yaml:"repeat_delay"`
}

// ProcessesConfig  is processes config.
type ProcessesConfig struct {
	XRPLTxSubmitter XRPLTxSubmitterConfig `yaml:"xrpl_tx_submitter"`
}

// Config is runner config.
type Config struct {
	Version       string          `yaml:"version"`
	LoggingConfig LoggingConfig   `yaml:"logging"`
	XRPL          XRPLConfig      `yaml:"xrpl"`
	Coreum        CoreumConfig    `yaml:"coreum"`
	Processes     ProcessesConfig `yaml:"processes"`
}

// DefaultConfig returns default runner config.
func DefaultConfig() Config {
	defaultXRPLRPCfg := xrpl.DefaultRPCClientConfig("")
	defaultXRPLAccountScannerCfg := xrpl.DefaultAccountScannerConfig(rippledata.Account{})

	defaultCoreumContactConfig := coreum.DefaultContractClientConfig(sdk.AccAddress(nil))
	defaultClientCtxDefaultCfg := coreumchainclient.DefaultContextConfig()

	defaultXRPLTxSubmitterConfig := processes.DefaultXRPLTxSubmitterConfig(rippledata.Account{}, sdk.AccAddress(nil))
	return Config{
		Version:       configVersion,
		LoggingConfig: LoggingConfig(logger.DefaultZapLoggerConfig()),
		XRPL: XRPLConfig{
			// empty be default
			MultiSignerKeyName: "",
			HTTPClient:         HTTPClientConfig(toolshttp.DefaultClientConfig()),
			RPC: XRPLRPCConfig{
				// empty be default
				URL:       "",
				PageLimit: defaultXRPLRPCfg.PageLimit,
			},
			Scanner: XRPLScannerConfig{
				RecentScanEnabled: defaultXRPLAccountScannerCfg.RecentScanEnabled,
				RecentScanWindow:  defaultXRPLAccountScannerCfg.RecentScanWindow,
				RepeatRecentScan:  defaultXRPLAccountScannerCfg.RepeatRecentScan,
				FullScanEnabled:   defaultXRPLAccountScannerCfg.FullScanEnabled,
				RepeatFullScan:    defaultXRPLAccountScannerCfg.RepeatFullScan,
				RetryDelay:        defaultXRPLAccountScannerCfg.RetryDelay,
			},
		},

		Coreum: CoreumConfig{
			// empty be default
			RelayerKeyName: "",
			GRPC: CoreumGRPCConfig{
				// empty be default
				URL: "",
			},
			Network: CoreumNetworkConfig{
				ChainID: string(defaultCoreumChainID),
			},
			Contract: CoreumContractConfig{
				// empty be default
				ContractAddress:    "",
				GasAdjustment:      defaultCoreumContactConfig.GasPriceAdjustment.MustFloat64(),
				GasPriceAdjustment: defaultCoreumContactConfig.GasAdjustment,
				PageLimit:          defaultCoreumContactConfig.PageLimit,

				RequestTimeout:       defaultClientCtxDefaultCfg.TimeoutConfig.RequestTimeout,
				TxTimeout:            defaultClientCtxDefaultCfg.TimeoutConfig.TxTimeout,
				TxStatusPollInterval: defaultClientCtxDefaultCfg.TimeoutConfig.TxStatusPollInterval,
			},
		},

		Processes: ProcessesConfig{
			XRPLTxSubmitter: XRPLTxSubmitterConfig{
				RepeatDelay: defaultXRPLTxSubmitterConfig.RepeatDelay,
			},
		},
	}
}

// ******************** Runner ********************

// Processes struct which aggregate all supported processes.
type Processes struct {
	XRPLTxObserver  processes.ProcessWithOptions
	XRPLTxSubmitter processes.ProcessWithOptions
}

// Runner is relayer runner which aggregates all relayer components.
type Runner struct {
	Log                      *logger.ZapLogger
	RetryableHTTPClient      *toolshttp.RetryableClient
	XRPLRPCClient            *xrpl.RPCClient
	XRPLAccountScanner       *xrpl.AccountScanner
	CoreumContractClient     *coreum.ContractClient
	CoreumChainNetworkConfig coreumchainconfig.NetworkConfig

	Processes Processes
	Processor *processes.Processor
}

// NewRunner return new runner from the config.
//
//nolint:funlen // the func contains sequential object initialisation
func NewRunner(ctx context.Context, cfg Config, kr keyring.Keyring) (*Runner, error) {
	zapLogger, err := logger.NewZapLogger(logger.ZapLoggerConfig(cfg.LoggingConfig))
	if err != nil {
		return nil, err
	}
	retryableXRPLRPCHTTPClient := toolshttp.NewRetryableClient(toolshttp.RetryableClientConfig(cfg.XRPL.HTTPClient))

	var contractAddress sdk.AccAddress
	if cfg.Coreum.Contract.ContractAddress != "" {
		contractAddress, err = sdk.AccAddressFromBech32(cfg.Coreum.Contract.ContractAddress)
		if err != nil {
			return nil, errors.Wrapf(
				err,
				"failed to decode contract address to sdk.AccAddress, address:%s",
				cfg.Coreum.Contract.ContractAddress,
			)
		}
	}
	contractClientCfg := coreum.ContractClientConfig{
		ContractAddress:    contractAddress,
		GasAdjustment:      cfg.Coreum.Contract.GasAdjustment,
		GasPriceAdjustment: sdk.MustNewDecFromStr(fmt.Sprintf("%f", cfg.Coreum.Contract.GasPriceAdjustment)),
		PageLimit:          cfg.Coreum.Contract.PageLimit,
	}

	coreumClientContextCfg := coreumchainclient.DefaultContextConfig()
	coreumClientContextCfg.TimeoutConfig.RequestTimeout = cfg.Coreum.Contract.RequestTimeout
	coreumClientContextCfg.TimeoutConfig.TxTimeout = cfg.Coreum.Contract.TxTimeout
	coreumClientContextCfg.TimeoutConfig.TxStatusPollInterval = cfg.Coreum.Contract.TxStatusPollInterval

	clientContext := coreumchainclient.NewContext(coreumClientContextCfg, coreumapp.ModuleBasics)
	if kr != nil {
		clientContext = clientContext.WithKeyring(kr)
	}
	if cfg.Coreum.GRPC.URL != "" {
		grpcClient, err := getGRPCClientConn(cfg.Coreum.GRPC.URL)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create coreum GRPC client, URL:%s", cfg.Coreum.GRPC.URL)
		}
		clientContext = clientContext.WithGRPCClient(grpcClient)
	}

	var coreumChainNetworkConfig coreumchainconfig.NetworkConfig
	if cfg.Coreum.Network.ChainID != "" {
		coreumChainNetworkConfig, err = coreumchainconfig.NetworkConfigByChainID(
			coreumchainconstant.ChainID(cfg.Coreum.Network.ChainID),
		)
		if err != nil {
			return nil, errors.Wrapf(
				err,
				"failed to set get correum network config for the chainID, chainID:%s",
				cfg.Coreum.Network.ChainID,
			)
		}
		clientContext = clientContext.WithChainID(cfg.Coreum.Network.ChainID)
	}

	var relayerAddress sdk.AccAddress
	if kr != nil && cfg.Coreum.RelayerKeyName != "" {
		keyRecord, err := kr.Key(cfg.Coreum.RelayerKeyName)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get relayer key from the keyring, key name:%s", cfg.Coreum.RelayerKeyName)
		}
		relayerAddress, err = keyRecord.GetAddress()
		if err != nil {
			return nil, errors.Wrapf(
				err,
				"failed to get address from keyring key recodr, key name:%s",
				cfg.Coreum.RelayerKeyName,
			)
		}
	}
	contractClient := coreum.NewContractClient(contractClientCfg, zapLogger, clientContext)

	contractConfig, err := contractClient.GetContractConfig(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get contract config for the runner intialization")
	}

	xrplRPCClientCfg := xrpl.RPCClientConfig(cfg.XRPL.RPC)
	xrplRPCClient := xrpl.NewRPCClient(xrplRPCClientCfg, zapLogger, retryableXRPLRPCHTTPClient)
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
	}, zapLogger, xrplRPCClient)

	var xrplKeyringTxSigner *xrpl.KeyringTxSigner
	if kr != nil {
		xrplKeyringTxSigner = xrpl.NewKeyringTxSigner(kr)
	}

	processor := processes.NewProcessor(zapLogger)
	runnerProcesses := Processes{
		XRPLTxObserver: processes.ProcessWithOptions{
			Process: processes.NewXRPLTxObserver(
				processes.XRPLTxObserverConfig{
					BridgeXRPLAddress:    *bridgeXRPLAddress,
					RelayerCoreumAddress: relayerAddress,
				},
				zapLogger,
				xrplScanner,
				contractClient,
			),
			Name:                 "xrpl_tx_observer",
			IsRestartableOnError: true,
		},
		XRPLTxSubmitter: processes.ProcessWithOptions{
			Process: processes.NewXRPLTxSubmitter(
				processes.XRPLTxSubmitterConfig{
					BridgeXRPLAddress:    *bridgeXRPLAddress,
					RelayerCoreumAddress: relayerAddress,
					XRPLTxSignerKeyName:  cfg.XRPL.MultiSignerKeyName,
					RepeatRecentScan:     true,
					RepeatDelay:          cfg.Processes.XRPLTxSubmitter.RepeatDelay,
				},
				zapLogger,
				contractClient,
				xrplRPCClient,
				xrplKeyringTxSigner,
			),
			Name:                 "xrpl_tx_submitter",
			IsRestartableOnError: true,
		},
	}

	return &Runner{
		Log:                      zapLogger,
		RetryableHTTPClient:      &retryableXRPLRPCHTTPClient,
		XRPLRPCClient:            xrplRPCClient,
		XRPLAccountScanner:       xrplScanner,
		CoreumContractClient:     contractClient,
		CoreumChainNetworkConfig: coreumChainNetworkConfig,

		Processes: runnerProcesses,
		Processor: processor,
	}, nil
}

// SetCoreumSDKConfig cosmos sdk config for the set coreum network.
func (r *Runner) SetCoreumSDKConfig() {
	r.CoreumChainNetworkConfig.SetSDKConfig()
}

// InitConfig creates config yaml file.
func InitConfig(homePath string, cfg Config) error {
	path := buildFilePath(homePath)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.Errorf("failed to initi config, file already exists, path:%s", path)
	}

	err := os.MkdirAll(homePath, 0o700)
	if err != nil {
		return errors.Errorf("failed to create dirs by path:%s", path)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.Wrapf(err, "failed to create config file, path:%s", path)
	}
	defer file.Close()
	yamlStringConfig, err := yaml.Marshal(cfg)
	if err != nil {
		return errors.Wrap(err, "failed convert default config to yaml")
	}
	if _, err := file.Write(yamlStringConfig); err != nil {
		return errors.Wrapf(err, "failed to write yaml config file, path:%s", path)
	}

	return nil
}

// ReadConfig reads config yaml file.
func ReadConfig(homePath string) (Config, error) {
	path := buildFilePath(homePath)
	file, err := os.OpenFile(path, os.O_RDONLY, 0o600)
	defer file.Close() //nolint:staticcheck //we accept the error ignoring
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, errors.Errorf("config file does not exist, path:%s", path)
	}
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return Config{}, errors.Wrapf(err, "failed to read bytes from file does not exist, path:%s", path)
	}

	var config Config
	if err := yaml.Unmarshal(fileBytes, &config); err != nil {
		return Config{}, errors.Wrapf(err, "failed to unmarshal file to yaml, path:%s", path)
	}

	return config, nil
}

func buildFilePath(homePath string) string {
	return filepath.Join(homePath, ConfigFileName)
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
