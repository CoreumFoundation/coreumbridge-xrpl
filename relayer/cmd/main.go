package main

import (
	"context"
	"os"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/CoreumFoundation/coreum-tools/pkg/run"
	coreumapp "github.com/CoreumFoundation/coreum/v3/app"
	"github.com/CoreumFoundation/coreum/v3/pkg/config"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/cmd/cli"
)

func main() {
	run.Tool("CoreumbridgeXRPLRelayer", func(ctx context.Context) error {
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
	cmd.AddCommand(cli.StartCmd())
	keyringCmd, err := cli.KeyringCmd()
	if err != nil {
		return nil, err
	}
	cmd.AddCommand(keyringCmd)
	cmd.AddCommand(cli.XRPLKeyInfoCmd())
	cmd.AddCommand(cli.BootstrapBridge())

	return cmd, nil
}
