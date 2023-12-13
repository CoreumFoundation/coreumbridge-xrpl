package client_test

import (
	"path"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
)

func TestInitAndReadBootstrappingConfig(t *testing.T) {
	t.Parallel()

	defaultCfg := client.DefaultBootstrappingConfig()
	yamlStringConfig, err := yaml.Marshal(defaultCfg)
	require.NoError(t, err)
	require.Equal(t, getDefaultConfigString(), string(yamlStringConfig))
	filePath := path.Join(t.TempDir(), "bootstrapping.yaml")
	require.NoError(t, client.InitBootstrappingConfig(filePath))
	readConfig, err := client.ReadBootstrappingConfig(filePath)
	require.NoError(t, err)

	require.Equal(t, defaultCfg, readConfig)
}

// the func returns the default config snapshot.
func getDefaultConfigString() string {
	return `owner: ""
admin: ""
relayers:
    - coreum_address: ""
      xrpl_address: ""
      xrpl_pub_key: ""
evidence_threshold: 0
used_ticket_sequence_threshold: 150
trust_set_limit_amount: "100000000000000000000000000000000000"
contract_bytecode_path: ""
`
}
