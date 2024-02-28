package main

import (
	"context"
	"os"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/CoreumFoundation/coreum-tools/pkg/run"
	coreumapp "github.com/CoreumFoundation/coreum/v4/app"
	"github.com/CoreumFoundation/coreum/v4/pkg/config"
	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/cmd/cli"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
)

func main() {
	run.Tool("coreumbridge-xrpl-relayer", func(ctx context.Context) error {
		rootCmd, err := RootCmd(ctx)
		if err != nil {
			return err
		}
		if err := rootCmd.Execute(); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}

		return nil
	})
}

// RootCmd returns the root cmd.
//
//nolint:contextcheck // the context is passed in the command
func RootCmd(ctx context.Context) (*cobra.Command, error) {
	encodingConfig := config.NewEncodingConfig(coreumapp.ModuleBasics)
	clientCtx := client.Context{}.
		WithCodec(encodingConfig.Codec).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithLegacyAmino(encodingConfig.Amino).
		WithInput(os.Stdin)
	ctx = context.WithValue(ctx, client.ClientContextKey, &clientCtx)
	cmd := &cobra.Command{
		Short: "Coreumbridge XRPL relayer.",
	}
	cmd.SetContext(ctx)

	cmd.AddCommand(cli.InitCmd())
	cmd.AddCommand(cli.StartCmd(processorProvider))
	cmd.AddCommand(cli.RelayerKeysCmd())
	cmd.AddCommand(cli.BootstrapBridgeCmd(bridgeClientProvider))
	cmd.AddCommand(cli.VersionCmd())

	coreumCmd, err := cli.CoreumCmd(bridgeClientProvider)
	if err != nil {
		return nil, err
	}
	cmd.AddCommand(coreumCmd)

	xrplCmd, err := cli.XRPLCmd(bridgeClientProvider)
	if err != nil {
		return nil, err
	}
	cmd.AddCommand(xrplCmd)

	return cmd, nil
}

func bridgeClientProvider(components runner.Components) (cli.BridgeClient, error) {
	return bridgeclient.NewBridgeClient(
		components.Log,
		components.CoreumClientCtx,
		components.CoreumContractClient,
		components.XRPLRPCClient,
		components.XRPLKeyringTxSigner,
	), nil
}

func processorProvider(cmd *cobra.Command) (cli.Runner, error) {
	rnr, err := cli.NewRunnerFromHome(cmd)
	if err != nil {
		return nil, err
	}

	return rnr, nil
}
