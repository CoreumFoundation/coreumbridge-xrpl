package runner_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
)

func TestInitAndReadConfig(t *testing.T) {
	t.Parallel()

	defaultCfg := runner.DefaultConfig()
	yamlStringConfig, err := yaml.Marshal(defaultCfg)
	require.NoError(t, err)
	require.Equal(t, getDefaultConfigString(), string(yamlStringConfig))
	// create temp dir to store the config
	tempDir := t.TempDir()
	//  try to read none-existing config
	_, err = runner.ReadConfig(tempDir)
	require.Error(t, err)

	// init the config first time
	require.NoError(t, runner.InitConfig(tempDir, defaultCfg))

	// try to init the config second time
	require.Error(t, runner.InitConfig(tempDir, defaultCfg))

	// read config
	readConfig, err := runner.ReadConfig(tempDir)
	require.NoError(t, err)
	require.Error(t, runner.InitConfig(tempDir, defaultCfg))

	require.Equal(t, defaultCfg, readConfig)
}

// the func returns the default config snapshot.
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
    xrpl_tx_submitter:
        repeat_delay: 10s
metrics:
    server:
        enable: false
        listen_address: localhost:9090
`
}
