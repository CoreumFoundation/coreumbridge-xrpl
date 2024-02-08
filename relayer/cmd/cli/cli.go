package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/keys"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/zap"

	"github.com/CoreumFoundation/coreum/v4/pkg/config"
	"github.com/CoreumFoundation/coreum/v4/pkg/config/constant"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/buildinfo"
	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

//go:generate mockgen -destination=cli_mocks_test.go -package=cli_test . BridgeClient,Runner

func init() {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}

	DefaultHomeDir = filepath.Join(userHomeDir, ".coreumbridge-xrpl-relayer")
}

// DefaultHomeDir is default home for the relayer.
var DefaultHomeDir string

const (
	sampleAmount = "100ucore"
)

const (
	// FlagAmount is the amount flag.
	FlagAmount = "amount"
	// FlagHome is home flag.
	FlagHome = "home"
	// FlagKeyName is key name flag.
	FlagKeyName = "key-name"
	// FlagCoreumKeyName is coreum key name flag.
	FlagCoreumKeyName = "coreum-key-name"
	// FlagXRPLKeyName is XRPL key name flag.
	FlagXRPLKeyName = "xrpl-key-name"
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
	// FlagTokenState is token state flag.
	FlagTokenState = "token-state"
	// FlagSendingPrecision is sending precision flag.
	FlagSendingPrecision = "sending-precision"
	// FlagBridgingFee is bridging fee flag.
	FlagBridgingFee = "bridging-fee"
	// FlagRefundID is id of a pending refund.
	FlagRefundID = "refund-id"
	// FlagMaxHoldingAmount is max holding amount flag.
	FlagMaxHoldingAmount = "max-holding-amount"
	// FlagDeliverAmount is deliver amount flag.
	FlagDeliverAmount = "deliver-amount"
	// FlagTicketsToAllocate is tickets to allocate flag.
	FlagTicketsToAllocate = "tickets-to-allocate"
	// FlagMetricsEnable enables metrics server.
	FlagMetricsEnable = "metrics-enable"
	// FlagsMetricsListenAddr sets listen address for metrics server.
	FlagsMetricsListenAddr = "metrics-listen-addr"
)

// BridgeClient is bridge client used to interact with the chains and contract.
//
//nolint:interfacebloat
type BridgeClient interface {
	Bootstrap(ctx context.Context,
		senderAddress sdk.AccAddress,
		bridgeAccountKeyName string,
		cfg bridgeclient.BootstrappingConfig,
	) (sdk.AccAddress, error)
	GetContractConfig(ctx context.Context) (coreum.ContractConfig, error)
	RecoverTickets(
		ctx context.Context,
		ownerAddress sdk.AccAddress,
		ticketsToAllocate *uint32,
	) error
	RegisterCoreumToken(
		ctx context.Context,
		ownerAddress sdk.AccAddress,
		denom string,
		decimals uint32,
		sendingPrecision int32,
		maxHoldingAmount sdkmath.Int,
		bridgingFee sdkmath.Int,
	) (coreum.CoreumToken, error)
	RegisterXRPLToken(
		ctx context.Context,
		ownerAddress sdk.AccAddress,
		issuer rippledata.Account, currency rippledata.Currency,
		sendingPrecision int32,
		maxHoldingAmount sdkmath.Int,
		bridgingFee sdkmath.Int,
	) (coreum.XRPLToken, error)
	GetAllTokens(ctx context.Context) ([]coreum.CoreumToken, []coreum.XRPLToken, error)
	SendFromCoreumToXRPL(
		ctx context.Context,
		sender sdk.AccAddress,
		recipient rippledata.Account,
		amount sdk.Coin,
		deliverAmount *sdkmath.Int,
	) error
	SendFromXRPLToCoreum(
		ctx context.Context,
		senderKeyName string,
		amount rippledata.Amount,
		recipient sdk.AccAddress,
	) error
	SetXRPLTrustSet(
		ctx context.Context,
		senderKeyName string,
		limitAmount rippledata.Amount,
	) error
	UpdateCoreumToken(
		ctx context.Context,
		sender sdk.AccAddress,
		denom string,
		state *coreum.TokenState,
		sendingPrecision *int32,
		maxHoldingAmount *sdkmath.Int,
		bridgingFee *sdkmath.Int,
	) error
	UpdateXRPLToken(
		ctx context.Context,
		sender sdk.AccAddress,
		issuer, currency string,
		state *coreum.TokenState,
		sendingPrecision *int32,
		maxHoldingAmount *sdkmath.Int,
		bridgingFee *sdkmath.Int,
	) error
	RotateKeys(
		ctx context.Context,
		sender sdk.AccAddress,
		cfg bridgeclient.KeysRotationConfig,
	) error
	UpdateXRPLBaseFee(
		ctx context.Context,
		sender sdk.AccAddress,
		xrplBaseFee uint32,
	) error
	GetCoreumBalances(ctx context.Context, address sdk.AccAddress) (sdk.Coins, error)
	GetXRPLBalances(ctx context.Context, acc rippledata.Account) ([]rippledata.Amount, error)
	GetPendingRefunds(ctx context.Context, address sdk.AccAddress) ([]coreum.PendingRefund, error)
	ClaimRefund(ctx context.Context, address sdk.AccAddress, pendingRefundID string) error
	GetFeesCollected(ctx context.Context, address sdk.Address) (sdk.Coins, error)
	ClaimRelayerFees(
		ctx context.Context,
		sender sdk.AccAddress,
		amounts sdk.Coins,
	) error
	RecoverXRPLTokenRegistration(
		ctx context.Context,
		sender sdk.AccAddress,
		issuer, currency string,
	) error
	HaltBridge(
		ctx context.Context,
		sender sdk.AccAddress,
	) error
	ResumeBridge(
		ctx context.Context,
		sender sdk.AccAddress,
	) error
}

// BridgeClientProvider is function which returns the BridgeClient from the input cmd.
type BridgeClientProvider func(cmd *cobra.Command) (BridgeClient, error)

// Runner is a runner interface.
type Runner interface {
	Start(ctx context.Context) error
}

// RunnerProvider is function which returns the Runner from the input cmd.
type RunnerProvider func(cmd *cobra.Command) (Runner, error)

// NewRunnerFromHome returns runner from home.
func NewRunnerFromHome(cmd *cobra.Command) (*runner.Runner, error) {
	clientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get client context")
	}
	xrplClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), xrpl.KeyringSuffix)
	if err != nil {
		return nil, errors.Wrap(err, "failed to configure xrpl keyring")
	}
	coreumClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
	if err != nil {
		return nil, errors.Wrap(err, "failed to configure coreum keyring")
	}

	cfg, err := GetHomeRunnerConfig(cmd)
	if err != nil {
		return nil, err
	}

	zapLogger, err := logger.NewZapLogger(logger.ZapLoggerConfig(cfg.LoggingConfig))
	if err != nil {
		return nil, err
	}

	components, err := runner.NewComponents(cfg, xrplClientCtx.Keyring, coreumClientCtx.Keyring, zapLogger, true)
	if err != nil {
		return nil, err
	}

	rnr, err := runner.NewRunner(cmd.Context(), components, cfg)
	if err != nil {
		return nil, err
	}

	return rnr, nil
}

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
			log, err := GetCLILogger()
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

			metricsEnable, err := cmd.Flags().GetBool(FlagMetricsEnable)
			if err != nil {
				return errors.Wrapf(err, "failed to read %s", FlagMetricsEnable)
			}

			metricsListenAddr, err := cmd.Flags().GetString(FlagsMetricsListenAddr)
			if err != nil {
				return errors.Wrapf(err, "failed to read %s", FlagsMetricsListenAddr)
			}

			cfg := runner.DefaultConfig()
			cfg.Coreum.Network.ChainID = chainID
			cfg.Coreum.GRPC.URL = coreumGRPCURL

			cfg.XRPL.RPC.URL = xrplRPCURL

			cfg.Metrics.Server.Enable = metricsEnable
			cfg.Metrics.Server.ListenAddress = metricsListenAddr

			if err = runner.InitConfig(home, cfg); err != nil {
				return err
			}
			log.Info(ctx, "Settings are generated successfully")
			return nil
		},
	}

	addCoreumChainIDFlag(cmd)
	cmd.PersistentFlags().String(FlagXRPLRPCURL, "", "XRPL RPC address.")
	cmd.PersistentFlags().String(FlagCoreumGRPCURL, "", "Coreum GRPC address.")
	cmd.PersistentFlags().Bool(FlagMetricsEnable, false, "Start metric server in relayer.")
	cmd.PersistentFlags().String(FlagsMetricsListenAddr, "localhost:9090", "Address metrics server listens on.")

	addHomeFlag(cmd)

	return cmd
}

// StartCmd returns the start cmd.
func StartCmd(pp RunnerProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start relayer.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// scan helps to wait for any input infinitely and just then call the relayer. That handles
			// the relayer restart in the container. Because after the restart the container is detached, relayer
			// requests the keyring password and fail immediately.
			log, err := GetCLILogger()
			if err != nil {
				return err
			}
			log.Info(ctx, "Press any key to start the relayer.")
			input := bufio.NewScanner(os.Stdin)
			input.Scan()

			runner, err := pp(cmd)
			if err != nil {
				return err
			}

			return runner.Start(ctx)
		},
	}
	addHomeFlag(cmd)
	addKeyringFlags(cmd)

	return cmd
}

// WithKeyring adds suffix-specific keyring to the context.
func WithKeyring(clientCtx client.Context, flagSet *pflag.FlagSet, suffix string) (client.Context, error) {
	if flagSet.Lookup(flags.FlagKeyringDir) == nil || flagSet.Lookup(flags.FlagKeyringBackend) == nil {
		return clientCtx, nil
	}
	keyringDir, err := flagSet.GetString(flags.FlagKeyringDir)
	if err != nil {
		return client.Context{}, errors.WithStack(err)
	}
	if keyringDir == "" {
		keyringDir = filepath.Join(clientCtx.HomeDir, "keyring")
	}
	keyringDir += "-" + suffix
	clientCtx = clientCtx.WithKeyringDir(keyringDir)

	keyringBackend, err := flagSet.GetString(flags.FlagKeyringBackend)
	if err != nil {
		return client.Context{}, errors.WithStack(err)
	}
	kr, err := client.NewKeyringFromBackend(clientCtx, keyringBackend)
	if err != nil {
		return client.Context{}, errors.WithStack(err)
	}
	return clientCtx.WithKeyring(kr), nil
}

// KeyringCmd returns cosmos keyring cmd inti with the correct keys home.
// Based on provided suffix and coinType it uses keyring dedicated to xrpl or coreum.
func KeyringCmd(suffix string, coinType uint32) (*cobra.Command, error) {
	// We need to set CoinType before initializing keys commands because keys.Commands() sets default
	// flag value from sdk config. See github.com/cosmos/cosmos-sdk@v0.47.5/client/keys/add.go:78
	sdk.GetConfig().SetCoinType(coinType)

	// we set it for the keyring manually since it doesn't use the runner which does it for other CLI commands
	cmd := keys.Commands(DefaultHomeDir)
	for _, childCmd := range cmd.Commands() {
		childCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.WithStack(err)
			}

			clientCtx, err = WithKeyring(clientCtx, cmd.Flags(), suffix)
			if err != nil {
				return err
			}

			if err := client.SetCmdClientContext(cmd, clientCtx); err != nil {
				return errors.WithStack(err)
			}
			return setCoreumConfigFromHomeFlag(cmd)
		}
	}
	cmd.Use += "-" + suffix

	return cmd, nil
}

// RelayerKeyInfoCmd prints the relayer keys info.
func RelayerKeyInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "relayer-keys-info",
		Short: "Prints the coreum and XRPL relayer keys info.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := setCoreumConfigFromHomeFlag(cmd); err != nil {
				return err
			}

			ctx := cmd.Context()
			log, err := GetCLILogger()
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}

			// XRPL
			cfg, err := GetHomeRunnerConfig(cmd)
			if err != nil {
				return err
			}

			xrplClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), xrpl.KeyringSuffix)
			if err != nil {
				return err
			}

			xrplKeyringTxSigner := xrpl.NewKeyringTxSigner(xrplClientCtx.Keyring)

			xrplAddress, err := xrplKeyringTxSigner.Account(cfg.XRPL.MultiSignerKeyName)
			if err != nil {
				return err
			}

			xrplPubKey, err := xrplKeyringTxSigner.PubKey(cfg.XRPL.MultiSignerKeyName)
			if err != nil {
				return err
			}

			// Coreum
			coreumClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
			if err != nil {
				return err
			}

			coreumKeyRecord, err := coreumClientCtx.Keyring.Key(cfg.Coreum.RelayerKeyName)
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
func BootstrapBridgeCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bootstrap-bridge [config-path]",
		Args:  cobra.ExactArgs(1),
		Short: "Sets up the XRPL bridge account with all required settings and deploys the bridge contract.",
		Long: strings.TrimSpace(fmt.Sprintf(
			`Sets up the XRPL bridge account with all required settings and deploys the bridge contract.
Example:
$ bootstrap-bridge bootstrapping.yaml --%s bridge-account
`, FlagKeyName)),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}
			log, err := GetCLILogger()
			if err != nil {
				return err
			}
			xrplKeyName, err := cmd.Flags().GetString(FlagXRPLKeyName)
			if err != nil {
				return errors.Wrapf(err, "failed to get %s", FlagXRPLKeyName)
			}
			xrplClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), xrpl.KeyringSuffix)
			if err != nil {
				return err
			}
			xrplKeyringTxSigner := xrpl.NewKeyringTxSigner(xrplClientCtx.Keyring)
			xrplBridgeAddress, err := xrplKeyringTxSigner.Account(xrplKeyName)
			if err != nil {
				return err
			}
			log.Info(ctx, "XRPL bridge address", zap.String("address", xrplBridgeAddress.String()))
			coreumKeyName, err := cmd.Flags().GetString(FlagCoreumKeyName)
			if err != nil {
				return errors.Wrapf(err, "failed to get %s", FlagCoreumKeyName)
			}
			coreumClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
			if err != nil {
				return err
			}
			coreumKRRecord, err := coreumClientCtx.Keyring.Key(coreumKeyName)
			if err != nil {
				return errors.Wrapf(err, "failed to get key by name:%s", coreumKeyName)
			}
			coreumAddress, err := coreumKRRecord.GetAddress()
			if err != nil {
				return errors.Wrapf(err, "failed to address for key name:%s", coreumKeyName)
			}
			log.Info(ctx, "Coreum deployer address", zap.String("address", coreumAddress.String()))

			filePath := args[0]
			initOnly, err := cmd.Flags().GetBool(FlagInitOnly)
			if err != nil {
				return errors.Wrapf(err, "failed to get %s", FlagInitOnly)
			}
			if initOnly {
				log.Info(ctx, "Initializing default bootstrapping config", zap.String("path", filePath))
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

			cfg, err := bridgeclient.ReadBootstrappingConfig(filePath)
			if err != nil {
				return err
			}
			log.Info(ctx, "Bootstrapping XRPL bridge", zap.Any("config", cfg))
			log.Info(ctx, "Press any key to continue.")
			input := bufio.NewScanner(os.Stdin)
			input.Scan()

			_, err = bridgeClient.Bootstrap(ctx, coreumAddress, xrplKeyName, cfg)
			return err
		},
	}
	addKeyringFlags(cmd)
	addHomeFlag(cmd)

	cmd.PersistentFlags().Bool(FlagInitOnly, false, "Init default config")
	cmd.PersistentFlags().Int(FlagRelayersCount, 0, "Relayers count")
	cmd.PersistentFlags().String(FlagCoreumKeyName, "", "Key name from the Coreum keyring")
	cmd.PersistentFlags().String(FlagXRPLKeyName, "", "Key name from the XRPL keyring")

	return cmd
}

// ContractConfigCmd prints contracts config.
func ContractConfigCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contract-config",
		Short: "Prints contract config.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			cfg, err := bridgeClient.GetContractConfig(ctx)
			if err != nil {
				return err
			}

			log, err := GetCLILogger()
			if err != nil {
				return err
			}
			log.Info(ctx, "Got contract config", zap.Any("config", cfg))

			return nil
		},
	}
	addHomeFlag(cmd)

	return cmd
}

// RecoverTicketsCmd recovers 250 tickets in the bridge contract.
func RecoverTicketsCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recover-tickets",
		Short: "Recovers tickets in the bridge contract.",
		Long: strings.TrimSpace(fmt.Sprintf(
			`Recovers tickets in the bridge contract.
Example:
$ recover-tickets --%s 250 --%s owner
`, FlagTicketsToAllocate, FlagKeyName)),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}

			ticketsToAllocated, err := getFlagUint32IfPresent(cmd, FlagTicketsToAllocate)
			if err != nil {
				return errors.Wrapf(err, "failed to get %s", FlagTicketsToAllocate)
			}

			xrplClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
			if err != nil {
				return err
			}
			owner, err := readAddressFromKeyNameFlag(cmd, xrplClientCtx)
			if err != nil {
				return err
			}

			return bridgeClient.RecoverTickets(ctx, owner, ticketsToAllocated)
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)
	cmd.PersistentFlags().Uint32(
		FlagTicketsToAllocate, 0, "tickets to allocate (if not provided the contract uses used tickets count)",
	)

	return cmd
}

// RegisterCoreumTokenCmd registers the Coreum originated token in the bridge contract.
func RegisterCoreumTokenCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-coreum-token [denom] [decimals] [sendingPrecision] [maxHoldingAmount] [bridgingFee]",
		Short: "Registers Coreum token in the bridge contract.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Registers Coreum token in the bridge contract.
Example:
$ register-coreum-token ucore 6 2 500000000000000 4000 --%s owner
`, FlagKeyName)),
		Args: cobra.ExactArgs(5),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}

			coreumClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
			if err != nil {
				return err
			}

			owner, err := readAddressFromKeyNameFlag(cmd, coreumClientCtx)
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
				owner,
				denom,
				uint32(decimals),
				int32(sendingPrecision),
				maxHoldingAmount,
				bridgingFee,
			)
			return err
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// UpdateCoreumTokenCmd updates the Coreum originated token in the bridge contract.
func UpdateCoreumTokenCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-coreum-token [denom]",
		Short: "Updates Coreum token in the bridge contract.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Updates Coreum token in the bridge contract.
Example:
$ update-coreum-token ucore --%s enabled --%s 2 --%s 10000000 --%s 4000 --%s owner
`, FlagTokenState, FlagSendingPrecision, FlagMaxHoldingAmount, FlagBridgingFee, FlagKeyName)),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}

			coreumClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
			if err != nil {
				return err
			}

			owner, err := readAddressFromKeyNameFlag(cmd, coreumClientCtx)
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
				owner,
				denom,
				tokenState,
				sendingPrecision,
				maxHoldingAmount,
				bridgingFee,
			)
		},
	}

	addUpdateTokenFlags(cmd)
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// RegisterXRPLTokenCmd registers the XRPL originated token in the bridge contract.
func RegisterXRPLTokenCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-xrpl-token [issuer] [currency] [sendingPrecision] [maxHoldingAmount] [bridgeFee]",
		Short: "Registers XRPL token in the bridge contract.",
		//nolint:lll // example
		Long: strings.TrimSpace(
			fmt.Sprintf(`Registers XRPL token in the bridge contract.
Example:
$ register-xrpl-token rcoreNywaoz2ZCQ8Lg2EbSLnGuRBmun6D 434F524500000000000000000000000000000000 2 500000000000000 4000 --%s owner
`, FlagKeyName)),
		Args: cobra.ExactArgs(5),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}

			coreumClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
			if err != nil {
				return err
			}

			owner, err := readAddressFromKeyNameFlag(cmd, coreumClientCtx)
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
				owner,
				*issuer,
				currency,
				int32(sendingPrecision),
				maxHoldingAmount,
				bridgingFee,
			)
			return err
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// RecoverXRPLTokenRegistrationCmd recovers xrpl token registration.
func RecoverXRPLTokenRegistrationCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recover-xrpl-token-registration [issuer] [currency]",
		Short: "Recovers XRPL token registration.",
		Long: strings.TrimSpace(fmt.Sprintf(
			`Recovers XRPL token registration.
Example:
$ recover-xrpl-token-registration [issuer] [currency] --%s owner
`, FlagKeyName)),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}

			coreumClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
			if err != nil {
				return err
			}

			owner, err := readAddressFromKeyNameFlag(cmd, coreumClientCtx)
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

			return bridgeClient.RecoverXRPLTokenRegistration(ctx, owner, issuer.String(), currency.String())
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// UpdateXRPLTokenCmd updates the XRPL originated token in the bridge contract.
func UpdateXRPLTokenCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-xrpl-token [denom]",
		Short: "Updates XRPL token in the bridge contract.",
		//nolint:lll // long example
		Long: strings.TrimSpace(
			fmt.Sprintf(`Updates XRPL token in the bridge contract.
Example:
$ update-xrpl-token rcoreNywaoz2ZCQ8Lg2EbSLnGuRBmun6D 434F524500000000000000000000000000000000 --%s enabled --%s 2 --%s 10000000 --%s 4000 --%s owner
`, FlagTokenState, FlagSendingPrecision, FlagMaxHoldingAmount, FlagBridgingFee, FlagKeyName)),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}

			coreumClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
			if err != nil {
				return err
			}

			owner, err := readAddressFromKeyNameFlag(cmd, coreumClientCtx)
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
				owner,
				issuer, currency,
				tokenState,
				sendingPrecision,
				maxHoldingAmount,
				bridgingFee,
			)
		},
	}

	addUpdateTokenFlags(cmd)
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

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
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}
			coreumClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
			if err != nil {
				return err
			}

			log, err := GetCLILogger()
			if err != nil {
				return err
			}

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
				log.Info(ctx, "Initializing default keys rotation config", zap.String("path", filePath))
				return bridgeclient.InitKeysRotationConfig(filePath)
			}

			record, err := coreumClientCtx.Keyring.Key(keyName)
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
			log.Info(ctx, "Start keys rotation", zap.Any("config", cfg))
			log.Info(ctx, "Press any key to continue.")
			input := bufio.NewScanner(os.Stdin)
			input.Scan()

			return bridgeClient.RotateKeys(ctx, addr, cfg)
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	cmd.PersistentFlags().Bool(FlagInitOnly, false, "Init default config")

	return cmd
}

// UpdateXRPLBaseFeeCmd updates the XRPL base fee in the bridge contract.
func UpdateXRPLBaseFeeCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-xrpl-base-fee [fee]",
		Short: "Update XRPL base fee in the bridge contract.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Update XRPL base fee in the bridge contract.
Example:
$ update-xrpl-base-fee 20 --%s owner
`, FlagKeyName)),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}

			coreumClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
			if err != nil {
				return err
			}

			owner, err := readAddressFromKeyNameFlag(cmd, coreumClientCtx)
			if err != nil {
				return err
			}

			xrplBaseFee, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return errors.Wrapf(err, "invalid XRPL base fee: %s", args[0])
			}

			return bridgeClient.UpdateXRPLBaseFee(
				ctx,
				owner,
				uint32(xrplBaseFee),
			)
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// RegisteredTokensCmd prints all registered tokens.
func RegisteredTokensCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registered-tokens",
		Short: "Prints all registered tokens.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			coreumTokens, xrplTokens, err := bridgeClient.GetAllTokens(ctx)
			if err != nil {
				return err
			}
			log, err := GetCLILogger()
			if err != nil {
				return err
			}
			log.Info(ctx, "Coreum tokens", zap.Int("total", len(coreumTokens)))
			for _, token := range coreumTokens {
				log.Info(ctx, token.Denom, zap.Any("token", token))
			}
			log.Info(ctx, "XRPL tokens", zap.Int("total", len(xrplTokens)))
			for _, token := range xrplTokens {
				log.Info(ctx, fmt.Sprintf("%s/%s", token.Currency, token.Issuer), zap.Any("token", token))
			}

			return nil
		},
	}
	addHomeFlag(cmd)

	return cmd
}

// SendFromCoreumToXRPLCmd sends tokens from the Coreum to XRPL.
func SendFromCoreumToXRPLCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send-from-coreum-to-xrpl [amount] [recipient]",
		Short: "Sends tokens from the Coreum to XRPL.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Sends tokens from the Coreum to XRPL.
Example:
$ send-from-coreum-to-xrpl 1000000ucore rrrrrrrrrrrrrrrrrrrrrhoLvTp --%s sender --%s 100000
`, FlagKeyName, FlagDeliverAmount)),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}

			coreumClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
			if err != nil {
				return err
			}
			deliverAmount, err := getFlagSDKIntIfPresent(cmd, FlagDeliverAmount)
			if err != nil {
				return err
			}

			sender, err := readAddressFromKeyNameFlag(cmd, coreumClientCtx)
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
		},
	}

	cmd.PersistentFlags().String(FlagDeliverAmount, "", "Deliver amount")
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// SendFromXRPLToCoreumCmd sends tokens from the XRPL to Coreum.
func SendFromXRPLToCoreumCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send-from-xrpl-to-coreum [amount] [issuer] [currency] [recipient]",
		Short: "Sends tokens from the XRPL to Coreum.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Sends tokens from the XRPL to Coreum.
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
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
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
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// CoreumBalancesCmd prints coreum balances.
func CoreumBalancesCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "coreum-balances [address]",
		Short: "Prints coreum balances of the provided address.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			address, err := sdk.AccAddressFromBech32(args[0])
			if err != nil {
				return errors.Wrapf(err, "failed to convert address string to sdk.AccAddress: %s", args[0])
			}

			coins, err := bridgeClient.GetCoreumBalances(ctx, address)
			if err != nil {
				return err
			}
			log, err := GetCLILogger()
			if err != nil {
				return err
			}
			log.Info(ctx, "Got balances", zap.Any("balances", coins))
			return nil
		},
	}
	addHomeFlag(cmd)

	return cmd
}

// XRPLBalancesCmd prints XRPL balances.
func XRPLBalancesCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "xrpl-balances [address]",
		Short: "Prints XRPL balances of the provided address.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
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

			log, err := GetCLILogger()
			if err != nil {
				return err
			}
			log.Info(ctx, "Got balances: [issuer/currency amount]", zap.Any("balances", balancesFormatted))
			return nil
		},
	}
	addHomeFlag(cmd)

	return cmd
}

// SetXRPLTrustSetCmd sends the XRPL TrustSet transaction.
func SetXRPLTrustSetCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-xrpl-trust-set [amount] [issuer] [currency]",
		Short: "Sends tokens from the XRPL to Coreum.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Sends tokens from the XRPL to Coreum.
Example:
$ set-xrpl-trust-set 1e80 %s %s --%s sender
`, xrpl.XRPTokenIssuer.String(), xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency), FlagKeyName),
		),
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
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
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// VersionCmd returns a CLI command to interactively print the application binary version information.
func VersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the application binary version information",
		RunE: func(cmd *cobra.Command, _ []string) error {
			log, err := GetCLILogger()
			if err != nil {
				return err
			}
			log.Info(
				cmd.Context(),
				"Version Info",
				zap.String("Git Tag", buildinfo.VersionTag),
				zap.String("Git Commit", buildinfo.GitCommit),
			)
			return nil
		},
	}
}

// GetPendingRefundsCmd gets the pending refunds of and address.
func GetPendingRefundsCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pending-refunds [address]",
		Short: "Get pending refunds of an address",
		Long: strings.TrimSpace(fmt.Sprintf(
			`Get pending refunds.
Example:
$ pending-refunds %s 
`, constant.AddressSampleTest,
		)),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			address, err := sdk.AccAddressFromBech32(args[0])
			if err != nil {
				return err
			}

			refunds, err := bridgeClient.GetPendingRefunds(ctx, address)
			if err != nil {
				return err
			}

			logger, err := GetCLILogger()
			if err != nil {
				return err
			}

			logger.Info(ctx, "pending refunds", zap.Any("refunds", refunds))
			return nil
		},
	}
	addHomeFlag(cmd)

	return cmd
}

// ClaimRefundCmd claims pending refund.
func ClaimRefundCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claim-refund",
		Short: "Claim pending refund, either all pending refunds or with a refund id.",
		Long: strings.TrimSpace(fmt.Sprintf(
			`Claims pending refunds.
Example:
$ claim-refund --%s claimer --%s 1705664693-2
`, FlagKeyName, FlagRefundID,
		)),
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}

			coreumClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
			if err != nil {
				return err
			}

			address, err := readAddressFromKeyNameFlag(cmd, coreumClientCtx)
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
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)
	cmd.PersistentFlags().String(FlagRefundID, "", "pending refund id")

	return cmd
}

// GetRelayerFeesCmd gets the fees of a relayer.
func GetRelayerFeesCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "relayer-fees [address]",
		Short: "Get the relayer fees",
		Long: strings.TrimSpace(fmt.Sprintf(
			`Get pending refunds.
Example:
$ relayer-fees %s 
`, constant.AddressSampleTest,
		)),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}

			address, err := sdk.AccAddressFromBech32(args[0])
			if err != nil {
				return err
			}

			relayerFees, err := bridgeClient.GetFeesCollected(ctx, address)
			if err != nil {
				return err
			}

			logger, err := GetCLILogger()
			if err != nil {
				return err
			}

			logger.Info(ctx, "relayer fees", zap.String("fees", relayerFees.String()))
			return nil
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

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
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}

			coreumClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
			if err != nil {
				return err
			}

			address, err := readAddressFromKeyNameFlag(cmd, coreumClientCtx)
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
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)
	cmd.PersistentFlags().String(FlagAmount, "", "specific amount to be collected")

	return cmd
}

// HaltBridgeCmd halts the bridge and stops its operation.
//
//nolint:dupl // abstracting this code will make it less readable.
func HaltBridgeCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "halt-bridge",
		Short: "Halts the bridge and stops its operation.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Halts the bridge and stops its operation.
Example:
$ halt-bridge --%s owner
`, FlagKeyName)),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}
			coreumClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
			if err != nil {
				return err
			}
			owner, err := readAddressFromKeyNameFlag(cmd, coreumClientCtx)
			if err != nil {
				return err
			}

			return bridgeClient.HaltBridge(
				ctx,
				owner,
			)
		},
	}

	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// ResumeBridgeCmd resumes the bridge and restarts its operation.
//
//nolint:dupl // abstracting this code will make it less readable.
func ResumeBridgeCmd(bcp BridgeClientProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resume-bridge",
		Short: "Resume the bridge and restarts its operation.",
		Long: strings.TrimSpace(
			fmt.Sprintf(`Resumes the bridge and restarts its operation.
Example:
$ resume-bridge --%s owner
`, FlagKeyName)),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// get bridgeClient first to set cosmos SDK config
			bridgeClient, err := bcp(cmd)
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}
			coreumClientCtx, err := WithKeyring(clientCtx, cmd.Flags(), coreum.KeyringSuffix)
			if err != nil {
				return err
			}
			owner, err := readAddressFromKeyNameFlag(cmd, coreumClientCtx)
			if err != nil {
				return err
			}
			return bridgeClient.ResumeBridge(
				ctx,
				owner,
			)
		},
	}

	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// GetCLILogger returns the console logger initialised with the default logger config but with set `yaml` format.
func GetCLILogger() (*logger.ZapLogger, error) {
	zapLogger, err := logger.NewZapLogger(logger.ZapLoggerConfig{
		Level:  "info",
		Format: logger.YamlConsoleLoggerFormat,
	})
	if err != nil {
		return nil, err
	}

	return zapLogger, nil
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

func setCoreumConfigFromHomeFlag(cmd *cobra.Command) error {
	cfg, err := GetHomeRunnerConfig(cmd)
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

// GetHomeRunnerConfig reads runner config from home directory.
func GetHomeRunnerConfig(cmd *cobra.Command) (runner.Config, error) {
	home, err := getRelayerHome(cmd)
	if err != nil {
		return runner.Config{}, err
	}
	return runner.ReadConfig(home)
}

func getRelayerHome(cmd *cobra.Command) (string, error) {
	return cmd.Flags().GetString(FlagHome)
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
		"", "The client Keyring directory; if omitted, the default 'home' directory will be used")
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

func addKeyNameFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().String(FlagKeyName, "", "Key name from the keyring")
}

func getFlagSDKIntIfPresent(cmd *cobra.Command, flag string) (*sdkmath.Int, error) {
	stringVal, err := getFlagStringIfPresent(cmd, flag)
	if err != nil {
		return nil, err
	}
	if stringVal != nil {
		bridgingFeeInt, ok := sdkmath.NewIntFromString(*stringVal)
		if !ok {
			return nil, errors.Errorf("failed to convert string to sdkmath.Int, string:%s", *stringVal)
		}
		return &bridgingFeeInt, nil
	}
	return nil, nil //nolint:nilnil // expected result
}

func getFlagStringIfPresent(cmd *cobra.Command, flagName string) (*string, error) {
	if !cmd.Flags().Lookup(flagName).Changed {
		return nil, nil //nolint:nilnil // nil is expected value
	}
	val, err := cmd.Flags().GetString(flagName)
	if err != nil {
		return nil, err
	}
	if val == "" {
		return nil, nil //nolint:nilnil // nil is expected value
	}

	return &val, nil
}

func getFlagInt32IfPresent(cmd *cobra.Command, flagName string) (*int32, error) {
	if !cmd.Flags().Lookup(flagName).Changed {
		return nil, nil //nolint:nilnil // nil is expected value
	}
	val, err := cmd.Flags().GetInt32(flagName)
	if err != nil {
		return nil, err
	}

	return &val, nil
}

func getFlagUint32IfPresent(cmd *cobra.Command, flagName string) (*uint32, error) {
	if !cmd.Flags().Lookup(flagName).Changed {
		return nil, nil //nolint:nilnil // nil is expected value
	}
	val, err := cmd.Flags().GetUint32(flagName)
	if err != nil {
		return nil, err
	}

	return &val, nil
}
