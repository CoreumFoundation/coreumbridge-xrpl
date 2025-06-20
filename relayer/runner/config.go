//nolint:tagliatelle // yaml naming
package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"gopkg.in/yaml.v3"

	toolshttp "github.com/CoreumFoundation/coreum-tools/pkg/http"
	coreumchainclient "github.com/CoreumFoundation/coreum/v5/pkg/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/metrics"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/processes"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

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
	ContractAddress       string        `yaml:"contract_address"`
	GasAdjustment         float64       `yaml:"gas_adjustment"`
	GasPriceAdjustment    float64       `yaml:"gas_price_adjustment"`
	PageLimit             uint32        `yaml:"page_limit"`
	OutOfGasRetryDelay    time.Duration `yaml:"out_of_gas_retry_delay"`
	OutOfGasRetryAttempts uint32        `yaml:"out_of_gas_retry_attempts"`
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

// CoreumToXRPLProcessConfig is CoreumToXRPLProcess config.
type CoreumToXRPLProcessConfig struct {
	RepeatDelay time.Duration `yaml:"repeat_delay"`
}

// ProcessesConfig  is processes config.
type ProcessesConfig struct {
	CoreumToXRPLProcess CoreumToXRPLProcessConfig `yaml:"coreum_to_xrpl"`
	RetryDelay          time.Duration             `yaml:"retry_delay"`
	ExitOnError         bool                      `yaml:"-"`
}

// MetricsServerConfig is metric server config.
type MetricsServerConfig struct {
	ListenAddress string `yaml:"listen_address"`
}

// MetricsPeriodicCollectorConfig is metric periodic collector config.
type MetricsPeriodicCollectorConfig struct {
	RepeatDelay time.Duration `yaml:"repeat_delay"`
}

// MetricsConfig is metric config.
type MetricsConfig struct {
	Enabled           bool                           `yaml:"enabled"`
	Server            MetricsServerConfig            `yaml:"server"`
	PeriodicCollector MetricsPeriodicCollectorConfig `yaml:"periodic_collector"`
}

// Config is runner config.
type Config struct {
	Version       string          `yaml:"version"`
	LoggingConfig LoggingConfig   `yaml:"logging"`
	XRPL          XRPLConfig      `yaml:"xrpl"`
	Coreum        CoreumConfig    `yaml:"coreum"`
	Processes     ProcessesConfig `yaml:"processes"`
	Metrics       MetricsConfig   `yaml:"metrics"`
}

// DefaultConfig returns default runner config.
func DefaultConfig() Config {
	defaultXRPLRPCfg := xrpl.DefaultRPCClientConfig("")
	defaultXRPLAccountScannerCfg := xrpl.DefaultAccountScannerConfig(rippledata.Account{})

	defaultCoreumContactConfig := coreum.DefaultContractClientConfig(sdk.AccAddress(nil))
	defaultClientCtxDefaultCfg := coreumchainclient.DefaultContextConfig()

	defaultProcessConfig := processes.DefaultProcessConfig(
		rippledata.Account{},
		sdk.AccAddress(nil),
	)
	defaultLoggerConfig := logger.DefaultZapLoggerConfig()

	defaultMetricsServerConfig := metrics.DefaultServerConfig()
	defaultMetricsPeriodicCollectorConfig := metrics.DefaultPeriodicCollectorConfig()

	return Config{
		Version: configVersion,
		LoggingConfig: LoggingConfig{
			Level:  defaultLoggerConfig.Level,
			Format: defaultLoggerConfig.Format,
		},
		XRPL: XRPLConfig{
			// empty be default
			MultiSignerKeyName: "xrpl-relayer",
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
			RelayerKeyName: "coreum-relayer",
			GRPC: CoreumGRPCConfig{
				// empty be default
				URL: "",
			},
			Network: CoreumNetworkConfig{
				ChainID: string(DefaultCoreumChainID),
			},
			Contract: CoreumContractConfig{
				// empty be default
				ContractAddress:       "",
				GasAdjustment:         defaultCoreumContactConfig.GasAdjustment,
				GasPriceAdjustment:    defaultCoreumContactConfig.GasPriceAdjustment.MustFloat64(),
				PageLimit:             defaultCoreumContactConfig.PageLimit,
				OutOfGasRetryDelay:    defaultCoreumContactConfig.OutOfGasRetryDelay,
				OutOfGasRetryAttempts: defaultCoreumContactConfig.OutOfGasRetryAttempts,

				RequestTimeout:       defaultClientCtxDefaultCfg.TimeoutConfig.RequestTimeout,
				TxTimeout:            defaultClientCtxDefaultCfg.TimeoutConfig.TxTimeout,
				TxStatusPollInterval: defaultClientCtxDefaultCfg.TimeoutConfig.TxStatusPollInterval,
			},
		},

		Processes: ProcessesConfig{
			CoreumToXRPLProcess: CoreumToXRPLProcessConfig{
				RepeatDelay: defaultProcessConfig.CoreumToXRPL.RepeatDelay,
			},
			RetryDelay: defaultProcessConfig.RetryDelay,
		},

		Metrics: MetricsConfig{
			Enabled: false,
			Server: MetricsServerConfig{
				ListenAddress: defaultMetricsServerConfig.ListenAddress,
			},
			PeriodicCollector: MetricsPeriodicCollectorConfig{
				RepeatDelay: defaultMetricsPeriodicCollectorConfig.RepeatDelay,
			},
		},
	}
}

// InitConfig creates config yaml file.
func InitConfig(homePath string, cfg Config) error {
	path := BuildFilePath(homePath)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.Errorf("failed to init config, file already exists, path:%s", path)
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
func ReadConfig(ctx context.Context, log logger.Logger, homePath string) (Config, error) {
	config, err := readConfigFromFile(homePath)
	if err != nil {
		return Config{}, err
	}
	setConfigDefaults(ctx, log, &config)
	return config, nil
}

func setConfigDefaults(ctx context.Context, log logger.Logger, config *Config) {
	// Set default retry_delay if the value is not set because of an old config version which doesn't contain retry_delay.
	if config.Processes.RetryDelay == 0 {
		defaultRetryDelay := DefaultConfig().Processes.RetryDelay
		log.Warn(
			ctx,
			fmt.Sprintf(
				"processes.retry_delay is not set in %s, using default value: %s",
				ConfigFileName, defaultRetryDelay,
			),
		)
		config.Processes.RetryDelay = defaultRetryDelay
	}
}

func readConfigFromFile(homePath string) (Config, error) {
	path := BuildFilePath(homePath)
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

// BuildFilePath builds the file path.
func BuildFilePath(homePath string) string {
	return filepath.Join(homePath, ConfigFileName)
}
