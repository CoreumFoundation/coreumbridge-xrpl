package cli_test

import (
	"context"
	"fmt"
	"path"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/cmd/cli"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
)

func TestInitCmd(t *testing.T) {
	configPath := path.Join(t.TempDir(), "config-path")
	configFilePath := path.Join(configPath, runner.ConfigFileName)
	require.NoFileExists(t, configFilePath)

	args := []string{
		fmt.Sprintf("--%s=%s", cli.FlagHome, configPath),
	}
	executeCmd(t, cli.InitCmd(), args...)
	require.FileExists(t, configFilePath)
}

func executeCmd(t *testing.T, cmd *cobra.Command, args ...string) {
	t.Helper()

	cmd.SetArgs(args)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		require.NoError(t, err)
	}

	t.Logf("Command %s is executed successfully", cmd.Name())
}
