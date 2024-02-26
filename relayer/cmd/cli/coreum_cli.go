package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	coreumchainclient "github.com/CoreumFoundation/coreum/v4/pkg/client"
	"github.com/CoreumFoundation/coreum/v4/pkg/config/constant"
	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	overridecryptokeyring "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/cmd/cli/cosmos/override/crypto/keyring"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

// CoreumCmd returns aggregated Coreum commands.
func CoreumCmd(bcp BridgeClientProvider) (*cobra.Command, error) {
	coreumCmd := &cobra.Command{
		Use:   "coreum",
		Short: "Coreum CLI.",
	}

	coreumTxCmd := &cobra.Command{
		Use:   TxCLIUse,
		Short: "Coreum transactions.",
	}
	coreumTxCmd.AddCommand(RecoverTicketsCmd(bcp))
	coreumTxCmd.AddCommand(RegisterCoreumTokenCmd(bcp))
	coreumTxCmd.AddCommand(UpdateCoreumTokenCmd(bcp))
	coreumTxCmd.AddCommand(RegisterXRPLTokenCmd(bcp))
	coreumTxCmd.AddCommand(RecoverXRPLTokenRegistrationCmd(bcp))
	coreumTxCmd.AddCommand(UpdateXRPLTokenCmd(bcp))
	coreumTxCmd.AddCommand(RotateKeysCmd(bcp))
	coreumTxCmd.AddCommand(UpdateXRPLBaseFeeCmd(bcp))
	coreumTxCmd.AddCommand(SendFromCoreumToXRPLCmd(bcp))
	coreumTxCmd.AddCommand(ClaimRefundCmd(bcp))
	coreumTxCmd.AddCommand(ClaimRelayerFeesCmd(bcp))
	coreumTxCmd.AddCommand(HaltBridgeCmd(bcp))
	coreumTxCmd.AddCommand(ResumeBridgeCmd(bcp))
	coreumTxCmd.AddCommand(CancelPendingOperationCmd(bcp))
	coreumTxCmd.AddCommand(UpdateProhibitedXRPLRecipientsCmd(bcp))

	AddKeyringFlags(coreumTxCmd)
	AddKeyNameFlag(coreumTxCmd)
	AddHomeFlag(coreumTxCmd)
	addGenerateOnlyFlag(coreumTxCmd)

	coreumQueryCmd := &cobra.Command{
		Use:   QueryCLIUse,
		Short: "Coreum queries.",
	}
	coreumQueryCmd.AddCommand(ContractConfigCmd(bcp))
	coreumQueryCmd.AddCommand(RegisteredTokensCmd(bcp))
	coreumQueryCmd.AddCommand(CoreumBalancesCmd(bcp))
	coreumQueryCmd.AddCommand(PendingRefundsCmd(bcp))
	coreumQueryCmd.AddCommand(RelayerFeesCmd(bcp))
	coreumQueryCmd.AddCommand(PendingOperationsCmd(bcp))
	coreumQueryCmd.AddCommand(ProhibitedXRPLRecipientsCmd(bcp))
	AddHomeFlag(coreumQueryCmd)

	keyringCoreumCmd, err := KeyringCmd(CoreumKeyringSuffix, constant.CoinType,
		overridecryptokeyring.CoreumAddressFormatter)
	if err != nil {
		return nil, err
	}

	coreumCmd.AddCommand(coreumTxCmd)
	coreumCmd.AddCommand(coreumQueryCmd)
	coreumCmd.AddCommand(keyringCoreumCmd)

	return coreumCmd, nil
}

// ********** TX **********

// RecoverTicketsCmd recovers 250 tickets in the bridge contract.
func RecoverTicketsCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recover-tickets",
		Short: "Recover tickets in the bridge contract.",
		Long: strings.TrimSpace(fmt.Sprintf(
			`Recover tickets in the bridge contract.
Example:
$ recover-tickets --%s 250 --%s owner
`, FlagTicketsToAllocate, FlagKeyName)),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				ticketsToAllocated, err := getFlagUint32IfPresent(cmd, FlagTicketsToAllocate)
				if err != nil {
					return errors.Wrapf(err, "failed to get %s", FlagTicketsToAllocate)
				}

				sender, err := readAddressFromKeyNameFlag(cmd, components.CoreumClientCtx)
				if err != nil {
					return err
				}

				return bridgeClient.RecoverTickets(ctx, sender, ticketsToAllocated)
			}),
	}
	cmd.PersistentFlags().Uint32(
		FlagTicketsToAllocate, 0, "tickets to allocate (if not provided the contract uses used tickets count)",
	)

	return cmd
}

// ClaimRelayerFeesCmd claims relayer fees.
func ClaimRelayerFeesCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claim-relayer-fees",
		Short: "Claim pending relayer fees,  either all or specific amount.",
		Long: strings.TrimSpace(fmt.Sprintf(
			`Claims relayer fees.
Example:
$ claim-relayer-fees --key-name address --%s %s
`, FlagAmount, sampleAmount,
		)),
		Args: cobra.NoArgs,
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				address, err := readAddressFromKeyNameFlag(cmd, components.CoreumClientCtx)
				if err != nil {
					return err
				}

				amountStr, err := cmd.Flags().GetString(FlagAmount)
				if err != nil {
					return err
				}

				if amountStr != "" {
					amount, err := sdk.ParseCoinsNormalized(amountStr)
					if err != nil {
						return err
					}
					return bridgeClient.ClaimRelayerFees(ctx, address, amount)
				}

				feesCollected, err := bridgeClient.GetFeesCollected(ctx, address)
				if err != nil {
					return err
				}

				return bridgeClient.ClaimRelayerFees(ctx, address, feesCollected)
			}),
	}
	cmd.PersistentFlags().String(FlagAmount, "", "specific amount to be collected")

	return cmd
}

// HaltBridgeCmd halts the bridge and stops its operation.
func HaltBridgeCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "halt-bridge",
		Short: "Halt the bridge and stops its operation.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Halt the bridge and stops its operation.
Example:
$ halt-bridge --%s owner
`, FlagKeyName)),
		Args: cobra.NoArgs,
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				sender, err := readAddressFromKeyNameFlag(cmd, components.CoreumClientCtx)
				if err != nil {
					return err
				}

				return bridgeClient.HaltBridge(
					ctx,
					sender,
				)
			}),
	}
}

// ResumeBridgeCmd resumes the bridge and restarts its operation.
func ResumeBridgeCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "resume-bridge",
		Short: "Resume the bridge.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Resume the bridge.
Example:
$ resume-bridge --%s owner
`, FlagKeyName)),
		Args: cobra.NoArgs,
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				sender, err := readAddressFromKeyNameFlag(cmd, components.CoreumClientCtx)
				if err != nil {
					return err
				}
				return bridgeClient.ResumeBridge(
					ctx,
					sender,
				)
			}),
	}
}

// CancelPendingOperationCmd cancels pending operation.
func CancelPendingOperationCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel-pending-operation [operation-id]",
		Short: "Cancel pending operation.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Cancel pending operation.
Example:
$ cancel-pending-operation 123 --%s owner
`, FlagKeyName)),
		Args: cobra.ExactArgs(1),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				sender, err := readAddressFromKeyNameFlag(cmd, components.CoreumClientCtx)
				if err != nil {
					return err
				}

				operationID, err := strconv.ParseUint(args[0], 10, 32)
				if err != nil {
					return errors.Wrapf(err, "invalid operation ID: %s", args[0])
				}

				return bridgeClient.CancelPendingOperation(
					ctx,
					sender,
					uint32(operationID),
				)
			}),
	}
}

// ClaimRefundCmd claims pending refund.
func ClaimRefundCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claim-refund",
		Short: "Claim pending refund, either all pending refunds or with a refund id.",
		Long: strings.TrimSpace(fmt.Sprintf(
			`Claim pending refunds.
Example:
$ claim-refund --%s claimer --%s 1705664693-2
`, FlagKeyName, FlagRefundID,
		)),
		Args: cobra.NoArgs,
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				address, err := readAddressFromKeyNameFlag(cmd, components.CoreumClientCtx)
				if err != nil {
					return err
				}

				refundID, err := cmd.Flags().GetString(FlagRefundID)
				if err != nil {
					return err
				}

				if refundID != "" {
					return bridgeClient.ClaimRefund(ctx, address, refundID)
				}

				refunds, err := bridgeClient.GetPendingRefunds(ctx, address)
				if err != nil {
					return err
				}

				for _, refund := range refunds {
					err := bridgeClient.ClaimRefund(ctx, address, refund.ID)
					if err != nil {
						return err
					}
				}
				return nil
			}),
	}

	cmd.PersistentFlags().String(FlagRefundID, "", "pending refund id")

	return cmd
}

// RegisterCoreumTokenCmd registers the Coreum originated token in the bridge contract.
func RegisterCoreumTokenCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "register-coreum-token [denom] [decimals] [sendingPrecision] [maxHoldingAmount] [bridgingFee]",
		Short: "Register Coreum token in the bridge contract.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Register Coreum token in the bridge contract.
Example:
$ register-coreum-token ucore 6 2 500000000000000 4000 --%s owner
`, FlagKeyName)),
		Args: cobra.ExactArgs(5),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				sender, err := readAddressFromKeyNameFlag(cmd, components.CoreumClientCtx)
				if err != nil {
					return err
				}

				denom := args[0]
				decimals, err := strconv.ParseUint(args[1], 10, 64)
				if err != nil {
					return errors.Wrapf(err, "invalid decimals: %s", args[1])
				}

				sendingPrecision, err := strconv.ParseInt(args[2], 10, 64)
				if err != nil {
					return errors.Wrapf(err, "invalid sendingPrecision: %s", args[2])
				}

				maxHoldingAmount, ok := sdkmath.NewIntFromString(args[3])
				if !ok {
					return errors.Wrapf(err, "invalid maxHoldingAmount: %s", args[3])
				}

				bridgingFee, ok := sdkmath.NewIntFromString(args[4])
				if !ok {
					return errors.Wrapf(err, "invalid bridgingFee: %s", args[4])
				}

				_, err = bridgeClient.RegisterCoreumToken(
					ctx,
					sender,
					denom,
					uint32(decimals),
					int32(sendingPrecision),
					maxHoldingAmount,
					bridgingFee,
				)
				return err
			}),
	}
}

// UpdateCoreumTokenCmd updates the Coreum originated token in the bridge contract.
func UpdateCoreumTokenCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-coreum-token [denom]",
		Short: "Update Coreum token in the bridge contract.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Update Coreum token in the bridge contract.
Example:
$ update-coreum-token ucore --%s enabled --%s 2 --%s 10000000 --%s 4000 --%s owner
`, FlagTokenState, FlagSendingPrecision, FlagMaxHoldingAmount, FlagBridgingFee, FlagKeyName)),
		Args: cobra.ExactArgs(1),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				sender, err := readAddressFromKeyNameFlag(cmd, components.CoreumClientCtx)
				if err != nil {
					return err
				}
				denom := args[0]

				state, sendingPrecision, maxHoldingAmount, bridgingFee, err := readUpdateTokenFlags(cmd)
				if err != nil {
					return err
				}

				tokenState, err := convertStateStringTokenState(state)
				if err != nil {
					return err
				}

				return bridgeClient.UpdateCoreumToken(
					ctx,
					sender,
					denom,
					tokenState,
					sendingPrecision,
					maxHoldingAmount,
					bridgingFee,
				)
			}),
	}

	addUpdateTokenFlags(cmd)

	return cmd
}

// RegisterXRPLTokenCmd registers the XRPL originated token in the bridge contract.
func RegisterXRPLTokenCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "register-xrpl-token [issuer] [currency] [sendingPrecision] [maxHoldingAmount] [bridgeFee]",
		Short: "Register XRPL token in the bridge contract.",
		//nolint:lll // example
		Long: strings.TrimSpace(
			fmt.Sprintf(`Register XRPL token in the bridge contract.
Example:
$ register-xrpl-token rcoreNywaoz2ZCQ8Lg2EbSLnGuRBmun6D 434F524500000000000000000000000000000000 2 500000000000000 4000 --%s owner
`, FlagKeyName)),
		Args: cobra.ExactArgs(5),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				sender, err := readAddressFromKeyNameFlag(cmd, components.CoreumClientCtx)
				if err != nil {
					return err
				}

				issuer, err := rippledata.NewAccountFromAddress(args[0])
				if err != nil {
					return errors.Wrapf(err, "failed to convert issuer string to rippledata.Account: %s", args[0])
				}

				currency, err := rippledata.NewCurrency(args[1])
				if err != nil {
					return errors.Wrapf(err, "failed to convert currency string to rippledata.Currency: %s", args[1])
				}

				sendingPrecision, err := strconv.ParseInt(args[2], 10, 64)
				if err != nil {
					return errors.Wrapf(err, "invalid sendingPrecision: %s", args[2])
				}

				maxHoldingAmount, ok := sdkmath.NewIntFromString(args[3])
				if !ok {
					return errors.Wrapf(err, "invalid maxHoldingAmount: %s", args[3])
				}

				bridgingFee, ok := sdkmath.NewIntFromString(args[4])
				if !ok {
					return errors.Wrapf(err, "invalid bridgeFee: %s", args[4])
				}

				_, err = bridgeClient.RegisterXRPLToken(
					ctx,
					sender,
					*issuer,
					currency,
					int32(sendingPrecision),
					maxHoldingAmount,
					bridgingFee,
				)
				return err
			}),
	}
}

// RecoverXRPLTokenRegistrationCmd recovers xrpl token registration.
func RecoverXRPLTokenRegistrationCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "recover-xrpl-token-registration [issuer] [currency]",
		Short: "Recover XRPL token registration.",
		Long: strings.TrimSpace(fmt.Sprintf(
			`Recover XRPL token registration.
Example:
$ recover-xrpl-token-registration [issuer] [currency] --%s owner
`, FlagKeyName)),
		Args: cobra.ExactArgs(2),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				sender, err := readAddressFromKeyNameFlag(cmd, components.CoreumClientCtx)
				if err != nil {
					return err
				}

				issuer, err := rippledata.NewAccountFromAddress(args[0])
				if err != nil {
					return errors.Wrapf(err, "failed to convert issuer string to rippledata.Account: %s", args[0])
				}

				currency, err := rippledata.NewCurrency(args[1])
				if err != nil {
					return errors.Wrapf(err, "failed to convert currency string to rippledata.Currency: %s", args[1])
				}

				return bridgeClient.RecoverXRPLTokenRegistration(ctx, sender, issuer.String(), currency.String())
			}),
	}
}

// UpdateXRPLTokenCmd updates the XRPL originated token in the bridge contract.
func UpdateXRPLTokenCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-xrpl-token [issuer] [currency]",
		Short: "Update XRPL token in the bridge contract.",
		//nolint:lll // long example
		Long: strings.TrimSpace(
			fmt.Sprintf(`Update XRPL token in the bridge contract.
Example:
$ update-xrpl-token rcoreNywaoz2ZCQ8Lg2EbSLnGuRBmun6D 434F524500000000000000000000000000000000 --%s enabled --%s 2 --%s 10000000 --%s 4000 --%s owner
`, FlagTokenState, FlagSendingPrecision, FlagMaxHoldingAmount, FlagBridgingFee, FlagKeyName)),
		Args: cobra.ExactArgs(2),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				sender, err := readAddressFromKeyNameFlag(cmd, components.CoreumClientCtx)
				if err != nil {
					return err
				}
				issuer := args[0]
				currency := args[1]

				state, sendingPrecision, maxHoldingAmount, bridgingFee, err := readUpdateTokenFlags(cmd)
				if err != nil {
					return err
				}

				tokenState, err := convertStateStringTokenState(state)
				if err != nil {
					return err
				}

				return bridgeClient.UpdateXRPLToken(
					ctx,
					sender,
					issuer, currency,
					tokenState,
					sendingPrecision,
					maxHoldingAmount,
					bridgingFee,
				)
			}),
	}

	addUpdateTokenFlags(cmd)

	return cmd
}

// RotateKeysCmd starts the keys rotation.
func RotateKeysCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rotate-keys [config-path]",
		Args:  cobra.ExactArgs(1),
		Short: "Start the keys rotation of the bridge.",
		Long: strings.TrimSpace(fmt.Sprintf(
			`Start the keys rotation of the bridge.
Example:
$ rotate-keys new-keys.yaml --%s owner
`, FlagKeyName)),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				keyName, err := cmd.Flags().GetString(FlagKeyName)
				if err != nil {
					return errors.Wrapf(err, "failed to get %s", FlagKeyName)
				}

				filePath := args[0]
				initOnly, err := cmd.Flags().GetBool(FlagInitOnly)
				if err != nil {
					return errors.Wrapf(err, "failed to get %s", FlagInitOnly)
				}
				if initOnly {
					components.Log.Info(ctx, "Initializing default keys rotation config", zap.String("path", filePath))
					return bridgeclient.InitKeysRotationConfig(filePath)
				}

				record, err := components.CoreumClientCtx.Keyring().Key(keyName)
				if err != nil {
					return errors.Wrapf(err, "failed to get key by name:%s", keyName)
				}
				addr, err := record.GetAddress()
				if err != nil {
					return errors.Wrapf(err, "failed to address for key name:%s", keyName)
				}

				cfg, err := bridgeclient.ReadKeysRotationConfig(filePath)
				if err != nil {
					return err
				}

				components.Log.Info(ctx, "Start keys rotation", zap.Any("config", cfg))
				components.Log.Info(ctx, "Press any key to continue.")

				input := bufio.NewScanner(os.Stdin)
				input.Scan()

				return bridgeClient.RotateKeys(ctx, addr, cfg)
			}),
	}

	cmd.PersistentFlags().Bool(FlagInitOnly, false, "Init default config")

	return cmd
}

// UpdateXRPLBaseFeeCmd updates the XRPL base fee in the bridge contract.
func UpdateXRPLBaseFeeCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "update-xrpl-base-fee [fee]",
		Short: "Update XRPL base fee in the bridge contract.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Update XRPL base fee in the bridge contract.
Example:
$ update-xrpl-base-fee 20 --%s owner
`, FlagKeyName)),
		Args: cobra.ExactArgs(1),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				sender, err := readAddressFromKeyNameFlag(cmd, components.CoreumClientCtx)
				if err != nil {
					return err
				}

				xrplBaseFee, err := strconv.ParseUint(args[0], 10, 64)
				if err != nil {
					return errors.Wrapf(err, "invalid XRPL base fee: %s", args[0])
				}

				return bridgeClient.UpdateXRPLBaseFee(
					ctx,
					sender,
					uint32(xrplBaseFee),
				)
			}),
	}
}

// SendFromCoreumToXRPLCmd sends tokens from the Coreum to XRPL.
func SendFromCoreumToXRPLCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send-from-coreum-to-xrpl [amount] [recipient]",
		Short: "Send tokens from the Coreum to XRPL.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Send tokens from the Coreum to XRPL.
Example:
$ send-from-coreum-to-xrpl 1000000ucore rrrrrrrrrrrrrrrrrrrrrhoLvTp --%s sender --%s 100000
`, FlagKeyName, FlagDeliverAmount)),
		Args: cobra.ExactArgs(2),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				deliverAmount, err := getFlagSDKIntIfPresent(cmd, FlagDeliverAmount)
				if err != nil {
					return err
				}

				sender, err := readAddressFromKeyNameFlag(cmd, components.CoreumClientCtx)
				if err != nil {
					return err
				}

				amount, err := sdk.ParseCoinNormalized(args[0])
				if err != nil {
					return err
				}
				recipient, err := rippledata.NewAccountFromAddress(args[1])
				if err != nil {
					return errors.Wrapf(err, "failed to convert recipient string to rippledata.Account: %s", args[1])
				}

				return bridgeClient.SendFromCoreumToXRPL(ctx, sender, *recipient, amount, deliverAmount)
			}),
	}

	cmd.PersistentFlags().String(FlagDeliverAmount, "", "Deliver amount")

	return cmd
}

// UpdateProhibitedXRPLRecipientsCmd updates/replace the list of the prohibited XRPL recipients.
func UpdateProhibitedXRPLRecipientsCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-prohibited-xrpl-recipients",
		Short: "Update prohibited XRPL recipients.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Update prohibited XRPL recipients.
Example (expects multiple %s):
$ update-prohibited-xrpl-recipients --%s %s --%s %s --%s owner
`,
				FlagProhibitedXRPLRecipient,
				FlagProhibitedXRPLRecipient, xrpl.XRPTokenIssuer.String(),
				FlagProhibitedXRPLRecipient, xrpl.XRPTokenIssuer.String(),
				FlagKeyName),
		),
		Args: cobra.NoArgs,
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()
				owner, err := readAddressFromKeyNameFlag(cmd, components.CoreumClientCtx)
				if err != nil {
					return err
				}

				prohibitedXRPLRecipients, err := cmd.Flags().GetStringArray(FlagProhibitedXRPLRecipient)
				if err != nil {
					return err
				}

				return bridgeClient.UpdateProhibitedXRPLRecipients(
					ctx,
					owner,
					prohibitedXRPLRecipients,
				)
			}),
	}

	cmd.PersistentFlags().StringArray(FlagProhibitedXRPLRecipient, []string{}, "Prohibited XRPL recipients")

	return cmd
}

// ********** QUERY **********

// ContractConfigCmd prints contracts config.
func ContractConfigCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "contract-config",
		Short: "Print contract config.",
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				cfg, err := bridgeClient.GetContractConfig(ctx)
				if err != nil {
					return err
				}

				components.Log.Info(ctx, "Got contract config", zap.Any("config", cfg))

				return nil
			}),
	}
}

// RegisteredTokensCmd prints all registered tokens.
func RegisteredTokensCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "registered-tokens",
		Short: "Print all registered tokens.",
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				coreumTokens, xrplTokens, err := bridgeClient.GetAllTokens(ctx)
				if err != nil {
					return err
				}

				components.Log.Info(ctx, "Coreum tokens", zap.Int("total", len(coreumTokens)))

				for _, token := range coreumTokens {
					components.Log.Info(ctx, token.Denom, zap.Any("token", token))
				}

				components.Log.Info(ctx, "XRPL tokens", zap.Int("total", len(xrplTokens)))

				for _, token := range xrplTokens {
					components.Log.Info(ctx, fmt.Sprintf("%s/%s", token.Currency, token.Issuer), zap.Any("token", token))
				}

				return nil
			}),
	}
}

// CoreumBalancesCmd prints coreum balances.
func CoreumBalancesCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "balances [address]",
		Short: "Print Coreum balances of the provided address.",
		Args:  cobra.ExactArgs(1),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				address, err := sdk.AccAddressFromBech32(args[0])
				if err != nil {
					return errors.Wrapf(err, "failed to convert address string to sdk.AccAddress: %s", args[0])
				}

				coins, err := bridgeClient.GetCoreumBalances(ctx, address)
				if err != nil {
					return err
				}

				components.Log.Info(ctx, "Got balances", zap.Any("balances", coins))

				return nil
			}),
	}
}

// PendingRefundsCmd gets the pending refunds of and address.
func PendingRefundsCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "pending-refunds [address]",
		Short: "Print pending refunds of an address",
		Long: strings.TrimSpace(fmt.Sprintf(
			`Print pending refunds.
Example:
$ pending-refunds %s 
`, constant.AddressSampleTest,
		)),
		Args: cobra.ExactArgs(1),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				address, err := sdk.AccAddressFromBech32(args[0])
				if err != nil {
					return err
				}

				refunds, err := bridgeClient.GetPendingRefunds(ctx, address)
				if err != nil {
					return err
				}

				components.Log.Info(ctx, "pending refunds", zap.Any("refunds", refunds))
				return nil
			}),
	}
}

// RelayerFeesCmd gets the fees of a relayer.
func RelayerFeesCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "relayer-fees [address]",
		Short: "Print the relayer fees",
		Long: strings.TrimSpace(fmt.Sprintf(
			`Print pending refunds.
Example:
$ relayer-fees %s 
`, constant.AddressSampleTest,
		)),
		Args: cobra.ExactArgs(1),
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				address, err := sdk.AccAddressFromBech32(args[0])
				if err != nil {
					return err
				}

				relayerFees, err := bridgeClient.GetFeesCollected(ctx, address)
				if err != nil {
					return err
				}

				components.Log.Info(ctx, "relayer fees", zap.String("fees", relayerFees.String()))
				return nil
			}),
	}
}

// PendingOperationsCmd prints pending operations.
func PendingOperationsCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "pending-operations",
		Short: "Print pending operations.",
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				pendingOperations, err := bridgeClient.GetPendingOperations(ctx)
				if err != nil {
					return err
				}

				log, err := GetCLILogger()
				if err != nil {
					return err
				}
				log.Info(ctx, "Got pending operations", zap.Any("pendingOperations", pendingOperations))

				return nil
			}),
	}
}

// ProhibitedXRPLRecipientsCmd gets the prohibited xrpl recipients from the contract.
func ProhibitedXRPLRecipientsCmd(bcp BridgeClientProvider) *cobra.Command {
	return &cobra.Command{
		Use:   "prohibited-xrpl-recipients",
		Short: "Print prohibited xrpl recipients.",
		Long: `Print prohibited xrpl recipients.
Example:
$ prohibited-xrpl-recipients %s 
`,
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				prohibitedXRPLRecipients, err := bridgeClient.GetProhibitedXRPLRecipients(ctx)
				if err != nil {
					return err
				}

				components.Log.Info(
					ctx,
					"Got prohibited XRPL recipients",
					zap.Any("prohibitedXRPLRecipients", prohibitedXRPLRecipients),
				)
				return nil
			}),
	}
}

func readAddressFromKeyNameFlag(cmd *cobra.Command, clientCtx coreumchainclient.Context) (sdk.AccAddress, error) {
	keyName, err := cmd.Flags().GetString(FlagKeyName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get flag %s", FlagKeyName)
	}
	keyRecord, err := clientCtx.Keyring().Key(keyName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get key by name:%s", keyName)
	}
	addr, err := keyRecord.GetAddress()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to address for key name:%s", keyName)
	}

	return addr, nil
}

func addUpdateTokenFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String(
		FlagTokenState,
		"",
		fmt.Sprintf("Token state (%s/%s)", coreum.TokenStateEnabled, coreum.TokenStateDisabled),
	)
	cmd.PersistentFlags().Int32(
		FlagSendingPrecision,
		0, "Token sending precision")
	cmd.PersistentFlags().String(
		FlagMaxHoldingAmount,
		"", "Token max holding amount")
	cmd.PersistentFlags().String(
		FlagBridgingFee,
		"", "Token bridging fee")
}

func readUpdateTokenFlags(cmd *cobra.Command) (*string, *int32, *sdkmath.Int, *sdkmath.Int, error) {
	var (
		state *string
		err   error
	)
	if state, err = getFlagStringIfPresent(cmd, FlagTokenState); err != nil {
		return nil, nil, nil, nil, err
	}
	var sendingPrecision *int32
	if sendingPrecision, err = getFlagInt32IfPresent(cmd, FlagSendingPrecision); err != nil {
		return nil, nil, nil, nil, err
	}

	maxHoldingAmount, err := getFlagSDKIntIfPresent(cmd, FlagMaxHoldingAmount)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	bridgingFee, err := getFlagSDKIntIfPresent(cmd, FlagBridgingFee)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	return state, sendingPrecision, maxHoldingAmount, bridgingFee, nil
}

func convertStateStringTokenState(state *string) (*coreum.TokenState, error) {
	if state == nil {
		return nil, nil //nolint:nilnil // nil is expected value
	}
	tokenState := coreum.TokenState(*state)
	switch tokenState {
	case coreum.TokenStateEnabled, coreum.TokenStateDisabled:
		return lo.ToPtr(tokenState), nil
	default:
		return nil, errors.Errorf("invalid token state: %s", *state)
	}
}

func addGenerateOnlyFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().Bool(flags.FlagGenerateOnly, false, "generate unsigned transaction")
}
