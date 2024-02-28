package cli

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/CoreumFoundation/coreum/v4/pkg/config/constant"
	overridecryptokeyring "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/cmd/cli/cosmos/override/crypto/keyring"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

// XRPLCmd returns aggregated XRPL commands.
func XRPLCmd(bcp BridgeClientProvider) (*cobra.Command, error) {
	xrplCmd := &cobra.Command{
		Use:   "xrpl",
		Short: "XRPL CLI.",
	}

	xrplTxCmd := &cobra.Command{
		Use:   TxCLIUse,
		Short: "XRPL transactions.",
	}
	xrplTxCmd.AddCommand(SendFromXRPLToCoreumCmd(bcp))
	xrplTxCmd.AddCommand(SetXRPLTrustSetCmd(bcp))

	AddKeyringFlags(xrplTxCmd)
	AddKeyNameFlag(xrplTxCmd)
	AddHomeFlag(xrplTxCmd)

	xrplQueryCmd := &cobra.Command{
		Use:   QueryCLIUse,
		Short: "XRPL queries.",
	}
	xrplQueryCmd.AddCommand(XRPLBalancesCmd(bcp))
	AddHomeFlag(xrplQueryCmd)

	keyringXRPLCmd, err := KeyringCmd(XRPLKeyringSuffix, constant.CoinType,
		overridecryptokeyring.XRPLAddressFormatter)
	if err != nil {
		return nil, err
	}

	xrplCmd.AddCommand(xrplTxCmd)
	xrplCmd.AddCommand(xrplQueryCmd)
	xrplCmd.AddCommand(keyringXRPLCmd)

	return xrplCmd, nil
}

// ********** TX **********

// SendFromXRPLToCoreumCmd sends tokens from the XRPL to Coreum.
func SendFromXRPLToCoreumCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "send-from-xrpl-to-coreum [amount] [issuer] [currency] [recipient]",
		Short: "Send tokens from the XRPL to Coreum.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Send tokens from the XRPL to Coreum.
Example:
$ send-from-xrpl-to-coreum 1000000 %s %s %s --%s sender
`,
				xrpl.XRPTokenIssuer.String(),
				xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
				constant.AddressSampleTest,
				FlagKeyName,
			),
		),
		Args: cobra.ExactArgs(4),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				issuer, err := rippledata.NewAccountFromAddress(args[1])
				if err != nil {
					return errors.Wrapf(err, "failed to convert issuer string to rippledata.Account: %s", args[2])
				}

				currency, err := rippledata.NewCurrency(args[2])
				if err != nil {
					return errors.Wrapf(err, "failed to convert currency string to rippledata.Currency: %s", args[1])
				}

				isNative := false
				if xrpl.ConvertCurrencyToString(currency) == xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency) &&
					issuer.String() == xrpl.XRPTokenIssuer.String() {
					isNative = true
				}

				value, err := rippledata.NewValue(args[0], isNative)
				if err != nil {
					return errors.Wrapf(err, "failed to amount to rippledata.Value: %s", args[0])
				}

				recipient, err := sdk.AccAddressFromBech32(args[3])
				if err != nil {
					return errors.Wrapf(err, "failed to convert recipient string to sdk.AccAddress: %s", args[3])
				}

				keyName, err := cmd.Flags().GetString(FlagKeyName)
				if err != nil {
					return errors.Wrapf(err, "failed to get flag %s", FlagKeyName)
				}

				return bridgeClient.SendFromXRPLToCoreum(
					ctx,
					keyName,
					rippledata.Amount{
						Value:    value,
						Currency: currency,
						Issuer:   *issuer,
					},
					recipient,
				)
			}),
	}
}

// SetXRPLTrustSetCmd sends the XRPL TrustSet transaction.
func SetXRPLTrustSetCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "set-trust-set [amount] [issuer] [currency]",
		Short: "Sent XRPL trust set transaction to allow the sender receive the token.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Send tokens from the XRPL to Coreum.
Example:
$ set-trust-set 1e80 %s %s --%s sender
`, xrpl.XRPTokenIssuer.String(), xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency), FlagKeyName),
		),
		Args: cobra.ExactArgs(3),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				issuer, err := rippledata.NewAccountFromAddress(args[1])
				if err != nil {
					return errors.Wrapf(err, "failed to convert issuer string to rippledata.Account: %s", args[2])
				}

				currency, err := rippledata.NewCurrency(args[2])
				if err != nil {
					return errors.Wrapf(err, "failed to convert currency string to rippledata.Currency: %s", args[1])
				}

				isNative := false
				if xrpl.ConvertCurrencyToString(currency) == xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency) &&
					issuer.String() == xrpl.XRPTokenIssuer.String() {
					isNative = true
				}

				value, err := rippledata.NewValue(args[0], isNative)
				if err != nil {
					return errors.Wrapf(err, "failed to amount to rippledata.Value: %s", args[0])
				}

				keyName, err := cmd.Flags().GetString(FlagKeyName)
				if err != nil {
					return errors.Wrapf(err, "failed to get flag %s", FlagKeyName)
				}

				return bridgeClient.SetXRPLTrustSet(
					ctx,
					keyName,
					rippledata.Amount{
						Value:    value,
						Currency: currency,
						Issuer:   *issuer,
					},
				)
			}),
	}
}

// ********** Query **********

// XRPLBalancesCmd prints XRPL balances.
func XRPLBalancesCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "balances [address]",
		Short: "Print XRPL balances of the provided address.",
		Args:  cobra.ExactArgs(1),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				acc, err := rippledata.NewAccountFromAddress(args[0])
				if err != nil {
					return errors.Wrapf(err, "failed to convert address to rippledata.Address, address:%s", args[0])
				}
				balances, err := bridgeClient.GetXRPLBalances(ctx, *acc)
				if err != nil {
					return err
				}

				balancesFormatted := lo.Map(balances, func(amount rippledata.Amount, index int) string {
					return fmt.Sprintf(
						"%s/%s %s",
						amount.Issuer.String(),
						xrpl.ConvertCurrencyToString(amount.Currency),
						amount.Value.String(),
					)
				})

				components.Log.Info(ctx, "Got balances: [issuer/currency amount]", zap.Any("balances", balancesFormatted))
				return nil
			}),
	}
}
