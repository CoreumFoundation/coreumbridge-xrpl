package runner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
)

func TestInitAndReadConfig(t *testing.T) {
	t.Parallel()

	zapLogger, err := logger.NewZapLogger(logger.ZapLoggerConfig{
		Level:  "error",
		Format: logger.YamlConsoleLoggerFormat,
	})
	require.NoError(t, err)
	ctx := context.Background()

	defaultCfg := runner.DefaultConfig()
	yamlStringConfig, err := yaml.Marshal(defaultCfg)
	require.NoError(t, err)
	require.Equal(t, getDefaultConfigString(), string(yamlStringConfig))

	tests := []struct {
		name                  string
		beforeWriteModifyFunc func(config runner.Config) runner.Config
		expectedConfigFunc    func(config runner.Config) runner.Config
	}{
		{
			name:                  "default config",
			beforeWriteModifyFunc: func(config runner.Config) runner.Config { return config },
			expectedConfigFunc:    func(config runner.Config) runner.Config { return config },
		},
		{
			name: "zero retry_delay", // version 1.1.0 or earlier.
			beforeWriteModifyFunc: func(config runner.Config) runner.Config {
				config.Processes.RetryDelay = 0
				return config
			},
			expectedConfigFunc: func(config runner.Config) runner.Config { return config },
		},
		{
			name: "custom retry_delay",
			beforeWriteModifyFunc: func(config runner.Config) runner.Config {
				config.Processes.RetryDelay *= 2
				return config
			},
			expectedConfigFunc: func(config runner.Config) runner.Config {
				config.Processes.RetryDelay *= 2
				return config
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			// create temp dir to store the config
			tempDir := tt.TempDir()
			// try to read none-existing config
			_, err = runner.ReadConfig(ctx, zapLogger, tempDir)
			require.Error(tt, err)

			// init the config first time
			modifiedCfg := tc.beforeWriteModifyFunc(defaultCfg)
			require.NoError(tt, runner.InitConfig(tempDir, modifiedCfg))

			// try to init the config second time
			require.Error(tt, runner.InitConfig(tempDir, modifiedCfg))

			// read config
			readConfig, err := runner.ReadConfig(ctx, zapLogger, tempDir)
			require.NoError(tt, err)
			require.Error(tt, runner.InitConfig(tempDir, defaultCfg))

			require.Equal(tt, tc.expectedConfigFunc(defaultCfg), readConfig)
		})
	}
}

// the func returns the default config snapshot as string.
func getDefaultConfigString() string {
	return `version: v1
logging:
    level: info
    format: console
xrpl:
    multi_signer_key_name: xrpl-relayer
    http_client:
        request_timeout: 5s
        do_timeout: 30s
        retry_delay: 300ms
    rpc:
        url: ""
        page_limit: 100
    scanner:
        recent_scan_enabled: true
        recent_scan_window: 10000
        repeat_recent_scan: true
        full_scan_enabled: true
        repeat_full_scan: true
        retry_delay: 10s
coreum:
    relayer_key_name: coreum-relayer
    grpc:
        url: ""
    network:
        chain_id: coreum-mainnet-1
    contract:
        contract_address: ""
        gas_adjustment: 1.4
        gas_price_adjustment: 1.2
        page_limit: 50
        out_of_gas_retry_delay: 500ms
        out_of_gas_retry_attempts: 5
        request_timeout: 10s
        tx_timeout: 1m0s
        tx_status_poll_interval: 500ms
processes:
    coreum_to_xrpl:
        repeat_delay: 10s
    retry_delay: 10s
metrics:
    enabled: false
    server:
        listen_address: localhost:9090
    periodic_collector:
        repeat_delay: 1m0s
`
}
