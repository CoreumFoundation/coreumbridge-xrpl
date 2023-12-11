package cli

import (
	"bufio"
	"os"
	"path"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

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
			log.Info(ctx, "Generating settings", logger.StringField("home", home))

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
				logger.StringField("coreumAddress", coreumAddress.String()),
				logger.StringField("xrplAddress", xrplAddress.String()),
				logger.StringField("xrplPubKey", xrplPubKey.String()),
			)

			return nil
		},
	}
	addKeyringFlags(cmd)
	addKeyNameFlag(cmd)
	addHomeFlag(cmd)

	return cmd
}

// BootstrapBridge safely creates XRPL bridge account with all required settings and deploys the bridge contract.
func BootstrapBridge() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bootstrap-bridge [config-path]",
		Args:  cobra.ExactArgs(1),
		Short: "Sets up the XRPL bridge account with all required settings and deploys the bridge contract",
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
			log.Info(ctx, "XRPL bridge address", logger.AnyField("address", xrplBridgeAddress.String()))

			filePath := args[0]
			initOnly, err := cmd.Flags().GetBool(FlagInitOnly)
			if err != nil {
				return errors.Wrapf(err, "failed to get %s", FlagInitOnly)
			}
			if initOnly {
				log.Info(ctx, "Initializing default bootstrapping config", logger.AnyField("path", filePath))
				if err := bridgeclient.InitBootstrappingConfig(filePath); err != nil {
					return err
				}
				relayersCount, err := cmd.Flags().GetInt(FlagRelayersCount)
				if err != nil {
					return errors.Wrapf(err, "failed to get %s", FlagRelayersCount)
				}
				if relayersCount > 0 {
					minXrplBridgeBalance := bridgeclient.ComputeXRPLBrideAccountBalance(relayersCount)
					log.Info(ctx, "Computed minimum XRPL bridge balance", logger.Float64Field("balance", minXrplBridgeBalance))
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
			log.Info(ctx, "Bootstrapping XRPL bridge", logger.AnyField("config", cfg))
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

func getRunnerFromHome(cmd *cobra.Command) (*runner.Runner, error) {
	cfg, err := getRelayerHomeRunnerConfig(cmd)
	if err != nil {
		return nil, err
	}
	clientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get client context")
	}
	rnr, err := runner.NewRunner(cmd.Context(), cfg, clientCtx.Keyring)
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
	cmd.PersistentFlags().String(flags.FlagKeyringBackend, flags.DefaultKeyringBackend, "Select keyring's backend (os|file|kwallet|pass|test)")
	cmd.PersistentFlags().String(flags.FlagKeyringDir, DefaultHomeDir, "The client Keyring directory; if omitted, the default 'home' directory will be used")
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
