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
	"github.com/CoreumFoundation/coreum/v4/pkg/config/constant"
	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/cmd/cli"
	overridecryptokeyring "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/cmd/cli/cosmos/override/crypto/keyring"
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

	keyringCoreumCmd, err := cli.KeyringCmd(coreum.KeyringSuffix, constant.CoinType,
		overridecryptokeyring.CoreumAddressFormatter)
	if err != nil {
		return nil, err
	}
	cmd.AddCommand(keyringCoreumCmd)

	keyringXRPLCmd, err := cli.KeyringCmd(xrpl.KeyringSuffix, xrpl.XRPLCoinType,
		overridecryptokeyring.XRPLAddressFormatter)
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
	cmd.AddCommand(cli.PendingRefundsCmd(bridgeClientProvider))
	cmd.AddCommand(cli.ClaimRefundCmd(bridgeClientProvider))
	cmd.AddCommand(cli.ClaimRelayerFeesCmd(bridgeClientProvider))
	cmd.AddCommand(cli.GetRelayerFeesCmd(bridgeClientProvider))
	cmd.AddCommand(cli.HaltBridgeCmd(bridgeClientProvider))
	cmd.AddCommand(cli.ResumeBridgeCmd(bridgeClientProvider))
	cmd.AddCommand(cli.GetProhibitedXRPLRecipientsCmd(bridgeClientProvider))
	cmd.AddCommand(cli.UpdateProhibitedXRPLRecipientsCmd(bridgeClientProvider))
	cmd.AddCommand(cli.CancelPendingOperationCmd(bridgeClientProvider))
	cmd.AddCommand(cli.PendingOperationsCmd(bridgeClientProvider))

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
