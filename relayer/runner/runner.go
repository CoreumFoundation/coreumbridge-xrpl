//nolint:tagliatelle // yaml naming
package runner

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"gopkg.in/yaml.v3"

	toolshttp "github.com/CoreumFoundation/coreum-tools/pkg/http"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

const (
	configVersion  = "v1"
	configFileName = "relayer.yaml"
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
	BridgeAccount string            `yaml:"bridge_account"`
	RPC           XRPLRPCConfig     `yaml:"rpc"`
	Scanner       XRPLScannerConfig `yaml:"scanner"`
}

// Config is runner config.
type Config struct {
	Version       string `yaml:"version"`
	LoggingConfig `yaml:"logging"`
	HTTPClient    HTTPClientConfig `yaml:"http_client"`
	XRPL          XRPLConfig       `yaml:"xrpl"`
}

// DefaultConfig returns default runner config.
func DefaultConfig() Config {
	defaultXRPLRPCConfig := xrpl.DefaultRPCClientConfig("")
	defaultXRPLAccountScannerCfg := xrpl.DefaultAccountScannerConfig(rippledata.Account{})
	return Config{
		Version:       configVersion,
		LoggingConfig: LoggingConfig(logger.DefaultZapLoggerConfig()),
		HTTPClient:    HTTPClientConfig(toolshttp.DefaultClientConfig()),
		XRPL: XRPLConfig{
			// empty be default
			BridgeAccount: "",
			RPC: XRPLRPCConfig{
				// empty be default
				URL:       "",
				PageLimit: defaultXRPLRPCConfig.PageLimit,
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
	}
}

// Runner is relayer runner which aggregates all relayer components.
type Runner struct {
	Log                 *logger.ZapLogger
	RetryableHTTPClient *toolshttp.RetryableClient
	XRPLRPCClient       *xrpl.RPCClient
	XRPLAccountScanner  *xrpl.AccountScanner
}

// NewRunner return new runner from the config.
func NewRunner(cfg Config) (*Runner, error) {
	zapLogger, err := logger.NewZapLogger(logger.ZapLoggerConfig(cfg.LoggingConfig))
	if err != nil {
		return nil, err
	}
	retryableHTTPClient := toolshttp.NewRetryableClient(toolshttp.RetryableClientConfig(cfg.HTTPClient))

	// XRPL
	xrplRPCClientCfg := xrpl.RPCClientConfig(cfg.XRPL.RPC)
	xrplRPCClient := xrpl.NewRPCClient(xrplRPCClientCfg, zapLogger, retryableHTTPClient)
	xrplAccount, err := rippledata.NewAccountFromAddress(cfg.XRPL.BridgeAccount)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get xrpl account from string, string:%s", cfg.XRPL.BridgeAccount)
	}
	xrplScanner := xrpl.NewAccountScanner(xrpl.AccountScannerConfig{
		Account:           *xrplAccount,
		RecentScanEnabled: cfg.XRPL.Scanner.RecentScanEnabled,
		RecentScanWindow:  cfg.XRPL.Scanner.RecentScanWindow,
		RepeatRecentScan:  cfg.XRPL.Scanner.RepeatRecentScan,
		FullScanEnabled:   cfg.XRPL.Scanner.FullScanEnabled,
		RepeatFullScan:    cfg.XRPL.Scanner.RepeatFullScan,
		RetryDelay:        cfg.XRPL.Scanner.RetryDelay,
	}, zapLogger, xrplRPCClient)

	return &Runner{
		Log:                 zapLogger,
		RetryableHTTPClient: &retryableHTTPClient,
		XRPLRPCClient:       xrplRPCClient,
		XRPLAccountScanner:  xrplScanner,
	}, nil
}

// InitConfig creates config yaml file.
func InitConfig(homePath string, cfg Config) error {
	path := buildFilePath(homePath)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.Errorf("failed to initi config, file already exists, path:%s", path)
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
	return filepath.Join(homePath, configFileName)
}
