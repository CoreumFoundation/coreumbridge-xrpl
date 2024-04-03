package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/zap"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/buildinfo"
	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/cmd/cli/cosmos/keys"
	overridekeyring "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/cmd/cli/cosmos/override/crypto/keyring"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
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

	// TxCLIUse is cobra Use tx group name.
	TxCLIUse = "tx"
	// QueryCLIUse is cobra Use query group name.
	QueryCLIUse = "q"

	// XRPLKeyringSuffix is XRPL keyring suffix.
	XRPLKeyringSuffix = "xrpl"
	// CoreumKeyringSuffix is Coreum keyring suffix.
	CoreumKeyringSuffix = "coreum"
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
	// FlagCoreumContractAddress is the address of the bridge smart contract.
	FlagCoreumContractAddress = "coreum-contract-address"
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
	// FlagMetricsEnabled enables metrics server.
	FlagMetricsEnabled = "metrics-enabled"
	// FlagMetricsListenAddr sets listen address for metrics server.
	FlagMetricsListenAddr = "metrics-listen-addr"
	// FlagProhibitedXRPLAddress the prohibited XRPL address.
	FlagProhibitedXRPLAddress = "prohibited-xrpl-address"
	// FlagFromOwner from owner flag.
	FlagFromOwner = "from-owner"
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
	GetContractOwnership(ctx context.Context) (coreum.ContractOwnership, error)
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
	GetProhibitedXRPLAddresses(ctx context.Context) ([]string, error)
	UpdateProhibitedXRPLAddresses(ctx context.Context, address sdk.AccAddress, prohibitedXRPLAddresses []string) error
	CancelPendingOperation(
		ctx context.Context,
		sender sdk.AccAddress,
		operationID uint32,
	) error
	GetPendingOperations(ctx context.Context) ([]coreum.Operation, error)
	GetTransactionEvidences(ctx context.Context) ([]coreum.TransactionEvidence, error)
	DeployContract(
		ctx context.Context,
		sender sdk.AccAddress,
		contractByteCodePath string,
	) (*sdk.TxResponse, uint64, error)
}

// BridgeClientProvider is function which returns the BridgeClient from the input cmd.
type BridgeClientProvider func(components runner.Components) (BridgeClient, error)

// Runner is a runner interface.
type Runner interface {
	Start(ctx context.Context) error
}

// RunnerProvider is function which returns the Runner from the input cmd.
type RunnerProvider func(cmd *cobra.Command) (Runner, error)

// NewRunnerFromHome returns runner from home.
func NewRunnerFromHome(cmd *cobra.Command) (*runner.Runner, error) {
	cfg, err := GetHomeRunnerConfig(cmd)
	if err != nil {
		return nil, err
	}

	logCfg := logger.DefaultZapLoggerConfig()
	logCfg.Level = cfg.LoggingConfig.Level
	logCfg.Format = cfg.LoggingConfig.Format
	zapLogger, err := logger.NewZapLogger(logCfg)
	if err != nil {
		return nil, err
	}

	components, err := NewComponents(cmd, zapLogger)
	if err != nil {
		return nil, err
	}

	rnr, err := runner.NewRunner(cmd.Context(), components, cfg)
	if err != nil {
		return nil, err
	}

	return rnr, nil
}

// NewComponents creates components based on CLI input.
func NewComponents(cmd *cobra.Command, log logger.Logger) (runner.Components, error) {
	cfg, err := GetHomeRunnerConfig(cmd)
	if err != nil {
		return runner.Components{}, err
	}

	clientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return runner.Components{}, errors.Wrap(err, "failed to get client context")
	}
	xrplClientCtx, err := withKeyring(clientCtx, cmd.Flags(), XRPLKeyringSuffix, log)
	if err != nil {
		return runner.Components{}, errors.Wrap(err, "failed to configure xrpl keyring")
	}
	coreumClientCtx, err := withKeyring(clientCtx, cmd.Flags(), CoreumKeyringSuffix, log)
	if err != nil {
		return runner.Components{}, errors.Wrap(err, "failed to configure coreum keyring")
	}

	components, err := runner.NewComponents(cfg, xrplClientCtx, coreumClientCtx, log)
	if err != nil {
		return runner.Components{}, err
	}

	return components, nil
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
			coreumContractAddress, err := cmd.Flags().GetString(FlagCoreumContractAddress)
			if err != nil {
				return errors.Wrapf(err, "failed to read %s", FlagCoreumContractAddress)
			}

			xrplRPCURL, err := cmd.Flags().GetString(FlagXRPLRPCURL)
			if err != nil {
				return errors.Wrapf(err, "failed to read %s", FlagXRPLRPCURL)
			}

			metricsEnabled, err := cmd.Flags().GetBool(FlagMetricsEnabled)
			if err != nil {
				return errors.Wrapf(err, "failed to read %s", FlagMetricsEnabled)
			}

			metricsListenAddr, err := cmd.Flags().GetString(FlagMetricsListenAddr)
			if err != nil {
				return errors.Wrapf(err, "failed to read %s", FlagMetricsListenAddr)
			}

			cfg := runner.DefaultConfig()
			cfg.Coreum.Network.ChainID = chainID
			cfg.Coreum.GRPC.URL = coreumGRPCURL
			cfg.Coreum.Contract.ContractAddress = coreumContractAddress

			cfg.XRPL.RPC.URL = xrplRPCURL

			cfg.Metrics.Enabled = metricsEnabled
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
	cmd.PersistentFlags().String(FlagCoreumContractAddress, "", "Address of the bridge smart contract.")
	cmd.PersistentFlags().Bool(FlagMetricsEnabled, false, "Start metric server in relayer.")
	cmd.PersistentFlags().String(FlagMetricsListenAddr, "localhost:9090", "Address metrics server listens on.")

	AddHomeFlag(cmd)

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
	AddHomeFlag(cmd)
	AddKeyringFlags(cmd)

	return cmd
}

// KeyringCmd returns cosmos keyring cmd inti with the correct keys home.
// Based on provided suffix and coinType it uses keyring dedicated to xrpl or coreum.
func KeyringCmd(
	suffix string,
	coinType uint32,
	addressFormatter overridekeyring.AddressFormatter,
) (*cobra.Command, error) {
	// We need to set CoinType before initializing keys commands because keys.Commands() sets default
	// flag value from sdk config. See github.com/cosmos/cosmos-sdk@v0.47.5/client/keys/add.go:78
	sdk.GetConfig().SetCoinType(coinType)

	// we set it for the keyring manually since it doesn't use the runner which does it for other CLI commands
	cmd := keys.Commands(DefaultHomeDir)
	for _, childCmd := range cmd.Commands() {
		childCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
			overridekeyring.SelectedAddressFormatter = addressFormatter

			log, err := GetCLILogger()
			if err != nil {
				return err
			}

			components, err := NewComponents(cmd, log)
			if err != nil {
				return err
			}

			var clientSDKCtx client.Context
			switch suffix {
			case XRPLKeyringSuffix:
				clientSDKCtx = components.XRPLSDKClietCtx
			case CoreumKeyringSuffix:
				clientSDKCtx = components.CoreumSDKClientCtx
			}

			if err := client.SetCmdClientContext(cmd, clientSDKCtx); err != nil {
				return errors.WithStack(err)
			}
			return nil
		}
	}

	return cmd, nil
}

// RelayerKeysCmd prints the relayer keys info.
func RelayerKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "relayer-keys",
		Short: "Print the Coreum and XRPL relayer keys info.",
		RunE: runBridgeCmd(nil,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				xrplAddress, err := components.XRPLKeyringTxSigner.Account(components.RunnerConfig.XRPL.MultiSignerKeyName)
				if err != nil {
					return err
				}

				xrplPubKey, err := components.XRPLKeyringTxSigner.PubKey(components.RunnerConfig.XRPL.MultiSignerKeyName)
				if err != nil {
					return err
				}

				// Coreum
				coreumKeyRecord, err := components.CoreumClientCtx.Keyring().Key(components.RunnerConfig.Coreum.RelayerKeyName)
				if err != nil {
					return errors.Wrapf(err, "failed to get coreum key, keyName:%s", components.RunnerConfig.Coreum.RelayerKeyName)
				}
				coreumAddress, err := coreumKeyRecord.GetAddress()
				if err != nil {
					return errors.Wrapf(err, "failed to get coreum address from key, keyName:%s",
						components.RunnerConfig.Coreum.RelayerKeyName)
				}

				components.Log.Info(
					ctx,
					"Keys info",
					zap.String("coreumAddress", coreumAddress.String()),
					zap.String("xrplAddress", xrplAddress.String()),
					zap.String("xrplPubKey", xrplPubKey.String()),
				)

				return nil
			}),
	}
	AddKeyringFlags(cmd)
	AddKeyNameFlag(cmd)
	AddHomeFlag(cmd)

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
		RunE: runBridgeCmd(bcp,
			func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error {
				ctx := cmd.Context()

				xrplKeyName, err := cmd.Flags().GetString(FlagXRPLKeyName)
				if err != nil {
					return errors.Wrapf(err, "failed to get %s", FlagXRPLKeyName)
				}
				xrplKeyringTxSigner := components.XRPLKeyringTxSigner
				xrplBridgeAddress, err := xrplKeyringTxSigner.Account(xrplKeyName)
				if err != nil {
					return err
				}

				components.Log.Info(ctx, "XRPL bridge address", zap.String("address", xrplBridgeAddress.String()))

				coreumKeyName, err := cmd.Flags().GetString(FlagCoreumKeyName)
				if err != nil {
					return errors.Wrapf(err, "failed to get %s", FlagCoreumKeyName)
				}
				coreumKRRecord, err := components.CoreumClientCtx.Keyring().Key(coreumKeyName)
				if err != nil {
					return errors.Wrapf(err, "failed to get key by name:%s", coreumKeyName)
				}
				coreumAddress, err := coreumKRRecord.GetAddress()
				if err != nil {
					return errors.Wrapf(err, "failed to address for key name:%s", coreumKeyName)
				}

				components.Log.Info(ctx, "Coreum deployer address", zap.String("address", coreumAddress.String()))

				filePath := args[0]
				initOnly, err := cmd.Flags().GetBool(FlagInitOnly)
				if err != nil {
					return errors.Wrapf(err, "failed to get %s", FlagInitOnly)
				}
				if initOnly {
					components.Log.Info(ctx, "Initializing default bootstrapping config", zap.String("path", filePath))
					if err := bridgeclient.InitBootstrappingConfig(filePath); err != nil {
						return err
					}
					relayersCount, err := cmd.Flags().GetInt(FlagRelayersCount)
					if err != nil {
						return errors.Wrapf(err, "failed to get %s", FlagRelayersCount)
					}
					if relayersCount > 0 {
						minXrplBridgeBalance := bridgeclient.ComputeXRPLBridgeAccountBalance()
						components.Log.Info(ctx, "Computed minimum XRPL bridge balance", zap.Float64("balance", minXrplBridgeBalance))
					}

					return nil
				}

				cfg, err := bridgeclient.ReadBootstrappingConfig(filePath)
				if err != nil {
					return err
				}
				components.Log.Info(ctx, "Bootstrapping XRPL bridge", zap.Any("config", cfg))
				components.Log.Info(ctx, "Press any key to continue.")
				input := bufio.NewScanner(os.Stdin)
				input.Scan()

				_, err = bridgeClient.Bootstrap(ctx, coreumAddress, xrplKeyName, cfg)
				return err
			}),
	}
	AddKeyringFlags(cmd)
	AddHomeFlag(cmd)

	cmd.PersistentFlags().Bool(FlagInitOnly, false, "Init default config")
	cmd.PersistentFlags().Int(FlagRelayersCount, 0, "Relayers count")
	cmd.PersistentFlags().String(FlagCoreumKeyName, "", "Key name from the Coreum keyring")
	cmd.PersistentFlags().String(FlagXRPLKeyName, "", "Key name from the XRPL keyring")

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

// GetHomeRunnerConfig reads runner config from home directory.
func GetHomeRunnerConfig(cmd *cobra.Command) (runner.Config, error) {
	home, err := getRelayerHome(cmd)
	if err != nil {
		return runner.Config{}, err
	}

	log, err := GetCLILogger()
	if err != nil {
		return runner.Config{}, err
	}
	cfg, err := runner.ReadConfig(cmd.Context(), log, home)
	if err != nil {
		return runner.Config{}, err
	}

	return cfg, nil
}

// AddHomeFlag adds home flag to the command.
func AddHomeFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().String(FlagHome, DefaultHomeDir, "Relayer home directory")
}

// AddKeyringFlags adds keyring flags to the command.
func AddKeyringFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String(
		flags.FlagKeyringBackend,
		flags.DefaultKeyringBackend,
		"Select keyring's backend (os|file|kwallet|pass|test)",
	)
	cmd.PersistentFlags().String(
		flags.FlagKeyringDir,
		"", "The client Keyring directory; if omitted, the default 'home' directory will be used")
}

// AddKeyNameFlag adds key-name flag to the command.
func AddKeyNameFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().String(FlagKeyName, "", "Key name from the keyring")
}

func getRelayerHome(cmd *cobra.Command) (string, error) {
	return cmd.Flags().GetString(FlagHome)
}

func addCoreumChainIDFlag(cmd *cobra.Command) *string {
	return cmd.PersistentFlags().String(FlagCoreumChainID, string(runner.DefaultCoreumChainID), "Default coreum chain ID")
}

// withKeyring adds suffix-specific keyring witch decoded private key caching to the context.
func withKeyring(
	clientCtx client.Context,
	flagSet *pflag.FlagSet,
	suffix string,
	log logger.Logger,
) (client.Context, error) {
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

	return clientCtx.WithKeyring(newCacheKeyring(suffix, kr, clientCtx.Codec, log)), nil
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

func runBridgeCmd(
	bcp BridgeClientProvider,
	f func(cmd *cobra.Command, args []string, components runner.Components, bridgeClient BridgeClient) error,
) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		log, err := GetCLILogger()
		if err != nil {
			return err
		}

		components, err := NewComponents(cmd, log)
		if err != nil {
			return err
		}

		var bridgeClient BridgeClient
		if bcp != nil {
			bridgeClient, err = bcp(components)
			if err != nil {
				return err
			}
		}

		return f(cmd, args, components, bridgeClient)
	}
}
