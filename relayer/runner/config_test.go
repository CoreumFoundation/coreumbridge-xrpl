package runner_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
)

func TestInitAndReadConfig(t *testing.T) {
	t.Parallel()
	defaultCfg := runner.DefaultConfig()

	t.Run("latest", func(tt *testing.T) {
		yamlStringConfig, err := yaml.Marshal(defaultCfg)
		require.NoError(tt, err)
		require.Equal(tt, getDefaultConfigString(), string(yamlStringConfig))
		// create temp dir to store the config
		tempDir := tt.TempDir()
		//  try to read none-existing config
		_, err = runner.ReadConfig(tempDir)
		require.Error(tt, err)

		// init the config first time
		require.NoError(tt, runner.InitConfig(tempDir, defaultCfg))

		// try to init the config second time
		require.Error(tt, runner.InitConfig(tempDir, defaultCfg))

		// read config
		readConfig, err := runner.ReadConfig(tempDir)
		require.NoError(tt, err)
		require.Error(tt, runner.InitConfig(tempDir, defaultCfg))

		require.Equal(tt, defaultCfg, readConfig)
	})

	t.Run("v1.1.0", func(tt *testing.T) {
		// create temp dir to store the config
		tempDir := tt.TempDir()
		//  try to read none-existing config
		_, err := runner.ReadConfig(tempDir)
		require.Error(tt, err)

		// store v1.1.0 config to temp dir
		configPath := runner.BuildFilePath(tempDir)
		file, err := os.OpenFile(configPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		require.NoError(tt, err)
		defer file.Close()
		_, err = file.Write([]byte(getV110DefaultConfigString()))
		require.NoError(tt, err)

		// read config
		readConfig, err := runner.ReadConfig(tempDir)
		require.NoError(tt, err)

		// Retry delay is set even though it is absent in v1.1.0 config.
		require.NotZero(tt, readConfig.Processes.RetryDelay)
		require.Equal(tt, defaultCfg, readConfig)
	})
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

// the func returns the default config snapshot as string for v1.1.0 relayer version.
func getV110DefaultConfigString() string {
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
metrics:
    enabled: false
    server:
        listen_address: localhost:9090
    periodic_collector:
        repeat_delay: 1m0s
`
}
