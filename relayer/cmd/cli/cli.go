package cli

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/keys"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/CoreumFoundation/coreum/v3/pkg/config"
	"github.com/CoreumFoundation/coreum/v3/pkg/config/constant"
	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

const (
	// DefaultHomeDir is default home for the relayer.
	DefaultHomeDir = ".coreumbridge-xrpl-relayer"
	// FlagHome is home flag.
	FlagHome = "home"
	// FlagKeyName is key name flag.
	FlagKeyName = "key-name"
	// FlagCoreumChainID is chain-id flag.
	FlagCoreumChainID = "coreum-chain-id"
	// FlagCoreumGRPCURL is Coreum GRPC URL flag.
	FlagCoreumGRPCURL = "coreum-grpc-url"
	// FlagXRPLRPCURL is XRPL RPC URL flag.
	FlagXRPLRPCURL = "xrpl-rpc-url"
	// FlagInitOnly is init only flag.
	FlagInitOnly = "init-only"
	// FlagRelayersCount is relayers count flag.
	FlagRelayersCount = "relayers-count"
)

// InitCmd returns the init cmd.
func InitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initializes the relayer home with the default config.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			home, err := getRelayerHome(cmd)
			if err != nil {
				return err
			}
			log, err := getConsoleLogger()
			if err != nil {
				return err
			}
			log.Info(ctx, "Generating settings", zap.String("home", home))

			chainID, err := cmd.Flags().GetString(FlagCoreumChainID)
			if err != nil {
				return errors.Wrapf(err, "failed to read %s", FlagCoreumChainID)
			}
			coreumGRPCURL, err := cmd.Flags().GetString(FlagCoreumGRPCURL)
			if err != nil {
				return errors.Wrapf(err, "failed to read %s", FlagCoreumGRPCURL)
			}

			xrplRPCURL, err := cmd.Flags().GetString(FlagXRPLRPCURL)
			if err != nil {
				return errors.Wrapf(err, "failed to read %s", FlagXRPLRPCURL)
			}

			cfg := runner.DefaultConfig()
			cfg.Coreum.Network.ChainID = chainID
			cfg.Coreum.GRPC.URL = coreumGRPCURL

			cfg.XRPL.RPC.URL = xrplRPCURL

			if err = runner.InitConfig(home, cfg); err != nil {
				return err
			}
			log.Info(ctx, "Settings are generated successfully")
			return nil
		},
	}

	addCoreumChainIDFlag(cmd)
	cmd.PersistentFlags().String(FlagXRPLRPCURL, "", "XRPL RPC address")
	cmd.PersistentFlags().String(FlagCoreumGRPCURL, "", "Coreum GRPC address.")

	addHomeFlag(cmd)

	return cmd
}

// StartCmd returns the start cmd.
func StartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start relayer.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// scan helps to wait for any input infinitely and just then call the relayer. That handles
			// the relayer restart in the container. Because after the restart the container is detached, relayer
			// requests the keyring password and fail inanimately.
			log, err := getConsoleLogger()
			if err != nil {
				return err
			}
			log.Info(ctx, "Press any key to start the relayer.")
			input := bufio.NewScanner(os.Stdin)
			input.Scan()

			rnr, err := getRunnerFromHome(cmd)
			if err != nil {
				return err
			}
			return rnr.Processor.StartProcesses(ctx, rnr.Processes.XRPLTxSubmitter, rnr.Processes.XRPLTxObserver)
		},
	}
	addHomeFlag(cmd)
	addKeyringFlags(cmd)

	return cmd
}

// KeyringCmd returns cosmos keyring cmd inti with the correct keys home.
func KeyringCmd() (*cobra.Command, error) {
	// we set it for the keyring manually since it doesn't use the runner which does it for other CLI commands
	cmd := keys.Commands(DefaultHomeDir)
	for _, childCmd := range cmd.Commands() {
		childCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
			return setCoreumConfigFromHomeFlag(cmd)
		}
	}

	return cmd, nil
}

// XRPLKeyInfoCmd prints the XRPL key info.
func XRPLKeyInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "relayer-keys-info",
		Short: "Prints the coreum and XRPL relayer key info.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := setCoreumConfigFromHomeFlag(cmd); err != nil {
				return err
			}

			ctx := cmd.Context()
			log, err := getConsoleLogger()
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}

			// XRPL
			cfg, err := getRelayerHomeRunnerConfig(cmd)
			if err != nil {
				return err
			}

			kr := clientCtx.Keyring
			xrplKeyringTxSigner := xrpl.NewKeyringTxSigner(kr)

			xrplAddress, err := xrplKeyringTxSigner.Account(cfg.XRPL.MultiSignerKeyName)
			if err != nil {
				return err
			}

			xrplPubKey, err := xrplKeyringTxSigner.PubKey(cfg.XRPL.MultiSignerKeyName)
			if err != nil {
				return err
			}

			// Coreum
			coreumKeyRecord, err := kr.Key(cfg.Coreum.RelayerKeyName)
			if err != nil {
				return errors.Wrapf(err, "failed to get coreum key, keyName:%s", cfg.Coreum.RelayerKeyName)
			}
			coreumAddress, err := coreumKeyRecord.GetAddress()
			if err != nil {
				return errors.Wrapf(err, "failed to get coreum address from key, keyName:%s", cfg.Coreum.RelayerKeyName)
			}

			log.Info(
				ctx,
				"Keys info",
				zap.String("coreumAddress", coreumAddress.String()),
				zap.String("xrplAddress", xrplAddress.String()),
				zap.String("xrplPubKey", xrplPubKey.String()),
			)

			return nil
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// BootstrapBridgeCmd safely creates XRPL bridge account with all required settings and deploys the bridge contract.
func BootstrapBridgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bootstrap-bridge [config-path]",
		Args:  cobra.ExactArgs(1),
		Short: "Sets up the XRPL bridge account with all required settings and deploys the bridge contract.",
		Long: strings.TrimSpace(
			`Sets up the XRPL bridge account with all required settings and deploys the bridge contract.
Example:
$ bootstrap-bridge bootstraping.yaml--key-name bridge-account
`,
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}

			log, err := getConsoleLogger()
			if err != nil {
				return err
			}

			keyName, err := cmd.Flags().GetString(FlagKeyName)
			if err != nil {
				return errors.Wrapf(err, "failed to get %s", FlagKeyName)
			}

			kr := clientCtx.Keyring
			xrplKeyringTxSigner := xrpl.NewKeyringTxSigner(kr)
			xrplBridgeAddress, err := xrplKeyringTxSigner.Account(keyName)
			if err != nil {
				return err
			}
			log.Info(ctx, "XRPL bridge address", zap.Any("address", xrplBridgeAddress.String()))

			filePath := args[0]
			initOnly, err := cmd.Flags().GetBool(FlagInitOnly)
			if err != nil {
				return errors.Wrapf(err, "failed to get %s", FlagInitOnly)
			}
			if initOnly {
				log.Info(ctx, "Initializing default bootstrapping config", zap.Any("path", filePath))
				if err := bridgeclient.InitBootstrappingConfig(filePath); err != nil {
					return err
				}
				relayersCount, err := cmd.Flags().GetInt(FlagRelayersCount)
				if err != nil {
					return errors.Wrapf(err, "failed to get %s", FlagRelayersCount)
				}
				if relayersCount > 0 {
					minXrplBridgeBalance := bridgeclient.ComputeXRPLBrideAccountBalance(relayersCount)
					log.Info(ctx, "Computed minimum XRPL bridge balance", zap.Float64("balance", minXrplBridgeBalance))
				}

				return nil
			}

			rnr, err := getRunnerFromHome(cmd)
			if err != nil {
				return err
			}
			record, err := clientCtx.Keyring.Key(keyName)
			if err != nil {
				return errors.Wrapf(err, "failed to get key by name:%s", keyName)
			}
			addr, err := record.GetAddress()
			if err != nil {
				return errors.Wrapf(err, "failed to address for key name:%s", keyName)
			}
			cfg, err := bridgeclient.ReadBootstrappingConfig(filePath)
			if err != nil {
				return err
			}
			log.Info(ctx, "Bootstrapping XRPL bridge", zap.Any("config", cfg))
			log.Info(ctx, "Press any key to continue.")
			input := bufio.NewScanner(os.Stdin)
			input.Scan()

			_, err = rnr.BridgeClient.Bootstrap(ctx, addr, keyName, cfg)
			return err
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	cmd.PersistentFlags().Bool(FlagInitOnly, false, "Init default config")
	cmd.PersistentFlags().Int(FlagRelayersCount, 0, "Relayers count")

	return cmd
}

// ContractConfigCmd prints contracts config .
func ContractConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contract-config",
		Short: "Prints contract config.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			rnr, err := getRunnerFromHome(cmd)
			if err != nil {
				return err
			}
			cfg, err := rnr.BridgeClient.GetContractConfig(ctx)
			if err != nil {
				return err
			}
			rnr.Log.Info(ctx, "Got contract config", zap.Any("config", cfg))
			return nil
		},
	}
	addHomeFlag(cmd)

	return cmd
}

// RecoverTicketsCmd recovers 250 tickets in the bridge contract.
func RecoverTicketsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recovery-tickets",
		Short: "Recovers 250 tickets in the bridge contract.",
		Long: strings.TrimSpace(
			`Recovers 250 tickets in the bridge contract.
Example:
$ recovery-tickets --key-name owner
`,
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}
			owner, err := readAddressFromKeyNameFlag(cmd, clientCtx)
			if err != nil {
				return err
			}
			rnr, err := getRunnerFromHome(cmd)
			if err != nil {
				return err
			}
			return rnr.BridgeClient.RecoverMaxTickets(ctx, owner, xrpl.MaxTicketsToAllocate)
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// RegisterCoreumTokenCmd registers the Coreum originated token in the bridge contract.
func RegisterCoreumTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-coreum-token [denom] [decimals] [sendingPrecision] [maxHoldingAmount]",
		Short: "Registers Coreum token in the bridge contract.",
		Long: strings.TrimSpace(
			`Registers Coreum token in the bridge contract.
Example:
$ register-coreum-token ucore 6 2 500000000000000 --key-name owner
`,
		),
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}
			owner, err := readAddressFromKeyNameFlag(cmd, clientCtx)
			if err != nil {
				return err
			}
			rnr, err := getRunnerFromHome(cmd)
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

			_, err = rnr.BridgeClient.RegisterCoreumToken(
				ctx,
				owner,
				denom,
				uint32(decimals),
				int32(sendingPrecision),
				maxHoldingAmount,
			)
			return err
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// RegisterXRPLTokenCmd registers the XRPL originated token in the bridge contract.
func RegisterXRPLTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-xrpl-token [issuer] [currency] [sendingPrecision] [maxHoldingAmount]",
		Short: "Registers XRPL token in the bridge contract.",
		//nolint:lll // example
		Long: strings.TrimSpace(
			`Registers XRPL token in the bridge contract.
Example:
$ register-xrpl-token rcoreNywaoz2ZCQ8Lg2EbSLnGuRBmun6D 434F524500000000000000000000000000000000 2 500000000000000 --key-name owner
`,
		),
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}
			owner, err := readAddressFromKeyNameFlag(cmd, clientCtx)
			if err != nil {
				return err
			}
			rnr, err := getRunnerFromHome(cmd)
			if err != nil {
				return err
			}

			issuer := args[0]
			currency := args[1]
			sendingPrecision, err := strconv.ParseInt(args[2], 10, 64)
			if err != nil {
				return errors.Wrapf(err, "invalid sendingPrecision: %s", args[2])
			}

			maxHoldingAmount, ok := sdkmath.NewIntFromString(args[3])
			if !ok {
				return errors.Wrapf(err, "invalid maxHoldingAmount: %s", args[3])
			}

			_, err = rnr.BridgeClient.RegisterXRPLToken(
				ctx,
				owner,
				issuer,
				currency,
				int32(sendingPrecision),
				maxHoldingAmount,
			)
			return err
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// RegisteredTokensCmd prints all registered tokens.
func RegisteredTokensCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registered-tokens",
		Short: "Prints all registered tokens.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			rnr, err := getRunnerFromHome(cmd)
			if err != nil {
				return err
			}
			coreumTokens, xrplTokens, err := rnr.BridgeClient.GetAllTokens(ctx)
			if err != nil {
				return err
			}
			rnr.Log.Info(ctx, "Got Coreum tokens", zap.Int("total", len(coreumTokens)))
			for _, token := range coreumTokens {
				rnr.Log.Info(ctx, token.Denom, zap.Any("token", token))
			}
			rnr.Log.Info(ctx, "Got XRPL tokens", zap.Int("total", len(coreumTokens)))
			for _, token := range xrplTokens {
				rnr.Log.Info(ctx, fmt.Sprintf("%s/%s", token.Currency, token.Issuer), zap.Any("token", token))
			}

			return nil
		},
	}
	addHomeFlag(cmd)

	return cmd
}

// SendFromCoreumToXRPLCmd sends tokens from the Coreum to XRPL.
func SendFromCoreumToXRPLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send-from-coreum-to-xrpl [amount] [recipient]",
		Short: "Sends tokens from the Coreum to XRPL.",
		Long: strings.TrimSpace(
			`Sends tokens from the Coreum to XRPL.
Example:
$ send-from-coreum-to-xrpl 1000000ucore rrrrrrrrrrrrrrrrrrrrrhoLvTp --key-name sender
`,
		),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}
			sender, err := readAddressFromKeyNameFlag(cmd, clientCtx)
			if err != nil {
				return err
			}
			rnr, err := getRunnerFromHome(cmd)
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

			return rnr.BridgeClient.SendFromCoreumToXRPL(ctx, sender, amount, *recipient)
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// SendFromXRPLToCoreumCmd sends tokens from the XRPL to Coreum.
func SendFromXRPLToCoreumCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send-from-xrpl-to-coreum [amount] [currency] [issuer] [recipient]",
		Short: "Sends tokens from the XRPL to Coreum.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Sends tokens from the XRPL to Coreum.
Example:
$ send-from-xrpl-to-coreum 1000000 %s %s %s  --key-name sender
`, xrpl.XRPTokenCurrency.String(), xrpl.XRPTokenIssuer.String(), constant.AddressSampleTest),
		),
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			rnr, err := getRunnerFromHome(cmd)
			if err != nil {
				return err
			}

			currency, err := rippledata.NewCurrency(args[1])
			if err != nil {
				return errors.Wrapf(err, "failed to convert currency string to rippledata.Currency: %s", args[1])
			}

			issuer, err := rippledata.NewAccountFromAddress(args[2])
			if err != nil {
				return errors.Wrapf(err, "failed to convert issuer string to rippledata.Account: %s", args[2])
			}
			isNative := false
			if currency.String() == xrpl.XRPTokenCurrency.String() && issuer.String() == xrpl.XRPTokenIssuer.String() {
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

			return rnr.BridgeClient.SendFromXRPLToCoreum(
				ctx,
				keyName,
				rippledata.Amount{
					Value:    value,
					Currency: currency,
					Issuer:   *issuer,
				},
				recipient,
			)
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// CoreumBalancesCmd prints coreum balances.
func CoreumBalancesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "coreum-balances [address]",
		Short: "Prints coreum balances of the provided address.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			rnr, err := getRunnerFromHome(cmd)
			if err != nil {
				return err
			}
			bankClient := banktypes.NewQueryClient(rnr.ClientCtx)
			balancesRes, err := bankClient.AllBalances(ctx, &banktypes.QueryAllBalancesRequest{
				Address: args[0],
			})
			if err != nil {
				return errors.Wrapf(err, "failed to get coreum balances, address:%s", args[0])
			}
			rnr.Log.Info(ctx, "Got balances", zap.Any("balances", balancesRes.Balances))
			return nil
		},
	}
	addHomeFlag(cmd)

	return cmd
}

// XRPLBalancesCmd prints XRPL balances.
func XRPLBalancesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "xrpl-balances [address]",
		Short: "Prints XRPL balances of the provided address.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			rnr, err := getRunnerFromHome(cmd)
			if err != nil {
				return err
			}

			acc, err := rippledata.NewAccountFromAddress(args[0])
			if err != nil {
				return errors.Wrapf(err, "failed to convert address to rippledata.Address, address:%s", args[0])
			}

			balances := make(map[string]rippledata.Amount, 0)
			accInfo, err := rnr.XRPLRPCClient.AccountInfo(ctx, *acc)
			if err != nil {
				return errors.Wrapf(err, "failed to get XRPL account info, address:%s", acc.String())
			}
			balances[fmt.Sprintf("%s/%s", xrpl.XRPTokenCurrency.String(), xrpl.XRPTokenIssuer.String())] = rippledata.Amount{
				Value: accInfo.AccountData.Balance,
			}
			// none xrp amounts
			accLines, err := rnr.XRPLRPCClient.AccountLines(ctx, *acc, "closed", nil)
			if err != nil {
				return errors.Wrapf(err, "failed to get XRPL account lines, address:%s", acc.String())
			}
			for _, line := range accLines.Lines {
				lineCopy := line
				balances[fmt.Sprintf("%s/%s", lineCopy.Currency.String(), lineCopy.Account.String())] = rippledata.Amount{
					Value:    &lineCopy.Balance.Value,
					Currency: lineCopy.Currency,
					Issuer:   lineCopy.Account,
				}
			}

			if err != nil {
				return errors.Wrapf(err, "failed to get XRPL balances")
			}
			rnr.Log.Info(ctx, "Got balances: [currency/issuer amount]", zap.Any("balances", balances))
			return nil
		},
	}
	addHomeFlag(cmd)

	return cmd
}

// SetXRPLTrustSet sends the XRPL TrustSet transaction.
func SetXRPLTrustSet() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-xrpl-trust-set [amount] [currency] [issuer]",
		Short: "Sends tokens from the XRPL to Coreum.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Sends tokens from the XRPL to Coreum.
Example:
$ set-xrpl-trust-set 1e80 %s %s --key-name sender
`, xrpl.XRPTokenCurrency.String(), xrpl.XRPTokenIssuer.String()),
		),
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			rnr, err := getRunnerFromHome(cmd)
			if err != nil {
				return err
			}

			currency, err := rippledata.NewCurrency(args[1])
			if err != nil {
				return errors.Wrapf(err, "failed to convert currency string to rippledata.Currency: %s", args[1])
			}

			issuer, err := rippledata.NewAccountFromAddress(args[2])
			if err != nil {
				return errors.Wrapf(err, "failed to convert issuer string to rippledata.Account: %s", args[2])
			}
			isNative := false
			if currency.String() == xrpl.XRPTokenCurrency.String() && issuer.String() == xrpl.XRPTokenIssuer.String() {
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

			return rnr.BridgeClient.SetXRPLTrustSet(
				ctx,
				keyName,
				rippledata.Amount{
					Value:    value,
					Currency: currency,
					Issuer:   *issuer,
				},
			)
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

func readAddressFromKeyNameFlag(cmd *cobra.Command, clientCtx client.Context) (sdk.AccAddress, error) {
	keyName, err := cmd.Flags().GetString(FlagKeyName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get flag %s", FlagKeyName)
	}
	keyRecord, err := clientCtx.Keyring.Key(keyName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get key by name:%s", keyName)
	}
	addr, err := keyRecord.GetAddress()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to address for key name:%s", keyName)
	}

	return addr, nil
}

func getRunnerFromHome(cmd *cobra.Command) (*runner.Runner, error) {
	cfg, err := getRelayerHomeRunnerConfig(cmd)
	if err != nil {
		return nil, err
	}
	clientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get client context")
	}
	rnr, err := runner.NewRunner(cmd.Context(), cfg, clientCtx.Keyring, true)
	if err != nil {
		return nil, err
	}

	return rnr, nil
}

func setCoreumConfigFromHomeFlag(cmd *cobra.Command) error {
	cfg, err := getRelayerHomeRunnerConfig(cmd)
	if err != nil {
		return err
	}
	network, err := config.NetworkConfigByChainID(constant.ChainID(cfg.Coreum.Network.ChainID))
	if err != nil {
		return err
	}
	network.SetSDKConfig()

	return nil
}

func getRelayerHomeRunnerConfig(cmd *cobra.Command) (runner.Config, error) {
	home, err := getRelayerHome(cmd)
	if err != nil {
		return runner.Config{}, err
	}
	return runner.ReadConfig(home)
}

func getRelayerHome(cmd *cobra.Command) (string, error) {
	home, err := cmd.Flags().GetString(FlagHome)
	if err != nil {
		return "", errors.WithStack(err)
	}
	if home == "" || home == DefaultHomeDir {
		home, err = getUserHomeDir(DefaultHomeDir)
		if err != nil {
			return "", err
		}
	}

	return home, nil
}

func getUserHomeDir(subPath ...string) (string, error) {
	dirname, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "failed to get user home dir")
	}
	for _, item := range subPath {
		dirname = path.Join(dirname, item)
	}

	return dirname, nil
}

func addHomeFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().String(FlagHome, DefaultHomeDir, "Relayer home directory")
}

func addCoreumChainIDFlag(cmd *cobra.Command) *string {
	return cmd.PersistentFlags().String(FlagCoreumChainID, string(runner.DefaultCoreumChainID), "Default coreum chain ID")
}

func addKeyringFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String(
		flags.FlagKeyringBackend,
		flags.DefaultKeyringBackend,
		"Select keyring's backend (os|file|kwallet|pass|test)",
	)
	cmd.PersistentFlags().String(
		flags.FlagKeyringDir,
		DefaultHomeDir, "The client Keyring directory; if omitted, the default 'home' directory will be used")
}

func addKeyNameFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().String(FlagKeyName, "", "Key name from the keyring")
}

// returns the console logger initialised with the default logger config but with set `console` format.
func getConsoleLogger() (*logger.ZapLogger, error) {
	cfg := runner.DefaultConfig().LoggingConfig
	cfg.Format = "console"
	zapLogger, err := logger.NewZapLogger(logger.ZapLoggerConfig(cfg))
	if err != nil {
		return nil, err
	}

	return zapLogger, nil
}
