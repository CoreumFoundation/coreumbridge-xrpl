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
	keyringCmd, err := cli.KeyringCmd()
	if err != nil {
		return nil, err
	}
	cmd.AddCommand(keyringCmd)
	cmd.AddCommand(cli.RelayerKeyInfoCmd())
	cmd.AddCommand(cli.BootstrapBridgeCmd(bridgeClientProvider))
	cmd.AddCommand(cli.ContractConfigCmd(bridgeClientProvider))
	cmd.AddCommand(cli.RecoverTicketsCmd(bridgeClientProvider))
	cmd.AddCommand(cli.RegisterCoreumTokenCmd(bridgeClientProvider))
	cmd.AddCommand(cli.UpdateCoreumTokenCmd(bridgeClientProvider))
	cmd.AddCommand(cli.RegisterXRPLTokenCmd(bridgeClientProvider))
	cmd.AddCommand(cli.UpdateXRPLTokenCmd(bridgeClientProvider))
	cmd.AddCommand(cli.RegisteredTokensCmd(bridgeClientProvider))
	cmd.AddCommand(cli.SendFromCoreumToXRPLCmd(bridgeClientProvider))
	cmd.AddCommand(cli.SendFromXRPLToCoreumCmd(bridgeClientProvider))
	cmd.AddCommand(cli.CoreumBalancesCmd(bridgeClientProvider))
	cmd.AddCommand(cli.XRPLBalancesCmd(bridgeClientProvider))
	cmd.AddCommand(cli.SetXRPLTrustSetCmd(bridgeClientProvider))
	cmd.AddCommand(cli.VersionCmd())
	cmd.AddCommand(cli.GetPendingRefundsCmd(bridgeClientProvider))
	cmd.AddCommand(cli.ClaimRefundCmd(bridgeClientProvider))

	return cmd, nil
}

func bridgeClientProvider(cmd *cobra.Command) (cli.BridgeClient, error) {
	rnr, err := cli.GetRunnerFromHome(cmd)
	if err != nil {
		return nil, err
	}

	log, err := cli.GetCLILogger()
	if err != nil {
		return nil, err
	}
	// for the bridge client we use the CLI logger
	return bridgeclient.NewBridgeClient(
		log,
		rnr.ClientCtx,
		rnr.CoreumContractClient,
		rnr.XRPLRPCClient,
		rnr.XRPLKeyringTxSigner,
	), nil
}

func processorProvider(cmd *cobra.Command) (cli.Processor, error) {
	rnr, err := cli.GetRunnerFromHome(cmd)
	if err != nil {
		return nil, err
	}

	return rnr, nil
}
