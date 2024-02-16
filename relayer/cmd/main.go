package main

import (
	"context"
	"os"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/CoreumFoundation/coreum-tools/pkg/run"
	coreumapp "github.com/CoreumFoundation/coreum/v4/app"
	"github.com/CoreumFoundation/coreum/v4/pkg/config"
	"github.com/CoreumFoundation/coreum/v4/pkg/config/constant"
	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/cmd/cli"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
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

	keyringCoreumCmd, err := cli.KeyringCmd(coreum.KeyringSuffix, constant.CoinType)
	if err != nil {
		return nil, err
	}
	cmd.AddCommand(keyringCoreumCmd)

	keyringXRPLCmd, err := cli.KeyringCmd(xrpl.KeyringSuffix, xrpl.XRPLCoinType)
	if err != nil {
		return nil, err
	}
	cmd.AddCommand(keyringXRPLCmd)

	cmd.AddCommand(cli.RelayerKeyInfoCmd())
	cmd.AddCommand(cli.BootstrapBridgeCmd(bridgeClientProvider))
	cmd.AddCommand(cli.ContractConfigCmd(bridgeClientProvider))
	cmd.AddCommand(cli.RecoverTicketsCmd(bridgeClientProvider))
	cmd.AddCommand(cli.RegisterCoreumTokenCmd(bridgeClientProvider))
	cmd.AddCommand(cli.UpdateCoreumTokenCmd(bridgeClientProvider))
	cmd.AddCommand(cli.RegisterXRPLTokenCmd(bridgeClientProvider))
	cmd.AddCommand(cli.RecoverXRPLTokenRegistrationCmd(bridgeClientProvider))
	cmd.AddCommand(cli.UpdateXRPLTokenCmd(bridgeClientProvider))
	cmd.AddCommand(cli.RotateKeysCmd(bridgeClientProvider))
	cmd.AddCommand(cli.UpdateXRPLBaseFeeCmd(bridgeClientProvider))
	cmd.AddCommand(cli.RegisteredTokensCmd(bridgeClientProvider))
	cmd.AddCommand(cli.SendFromCoreumToXRPLCmd(bridgeClientProvider))
	cmd.AddCommand(cli.SendFromXRPLToCoreumCmd(bridgeClientProvider))
	cmd.AddCommand(cli.CoreumBalancesCmd(bridgeClientProvider))
	cmd.AddCommand(cli.XRPLBalancesCmd(bridgeClientProvider))
	cmd.AddCommand(cli.SetXRPLTrustSetCmd(bridgeClientProvider))
	cmd.AddCommand(cli.VersionCmd())
	cmd.AddCommand(cli.GetPendingRefundsCmd(bridgeClientProvider))
	cmd.AddCommand(cli.ClaimRefundCmd(bridgeClientProvider))
	cmd.AddCommand(cli.ClaimRelayerFeesCmd(bridgeClientProvider))
	cmd.AddCommand(cli.GetRelayerFeesCmd(bridgeClientProvider))
	cmd.AddCommand(cli.HaltBridgeCmd(bridgeClientProvider))
	cmd.AddCommand(cli.ResumeBridgeCmd(bridgeClientProvider))

	return cmd, nil
}

func isGenerateOnly(
	cmd *cobra.Command,
) bool {
	flagSet := cmd.Flags()
	if flagSet.Changed(flags.FlagGenerateOnly) {
		genOnly, _ := flagSet.GetBool(flags.FlagGenerateOnly)
		return genOnly
	}

	return false
}

func bridgeClientProvider(cmd *cobra.Command) (cli.BridgeClient, error) {
	log, err := cli.GetCLILogger()
	if err != nil {
		return nil, err
	}

	cfg, err := cli.GetHomeRunnerConfig(cmd)
	if err != nil {
		return nil, err
	}

	clientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get client context")
	}
	xrplClientCtx, err := cli.WithKeyring(clientCtx, cmd.Flags(), xrpl.KeyringSuffix)
	if err != nil {
		return nil, errors.Wrap(err, "failed to configure xrpl keyring")
	}
	coreumClientCtx, err := cli.WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
	if err != nil {
		return nil, errors.Wrap(err, "failed to configure coreum keyring")
	}

	components, err := runner.NewComponents(cfg, xrplClientCtx.Keyring, coreumClientCtx.Keyring, log, true, false)
	if err != nil {
		return nil, err
	}

	generateOnly := isGenerateOnly(cmd)
	components.CoreumContractClient.SetGenerateOnly(generateOnly)
	// for the bridge client we use the CLI logger
	return bridgeclient.NewBridgeClient(
		components.Log,
		components.CoreumClientCtx.WithGenerateOnly(generateOnly),
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
