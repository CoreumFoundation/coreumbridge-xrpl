package cli

import (
	"bufio"
	"os"
	"path"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
)

const (
	// DefaultHomeDir is default home for the relayer.
	DefaultHomeDir = ".coreumbridge-xrpl-relayer"
	// FlagHome is home flag.
	FlagHome = "home"
	// That key name is constant here temporary, we will take it from the relayer config later.
	relayerKeyName = "coreumbridge-xrpl-relayer"
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
			log.Info(ctx, "Generating default settings", logger.StringField("home", home))
			if err = runner.InitConfig(home, runner.DefaultConfig()); err != nil {
				return err
			}
			log.Info(ctx, "Settings are generated successfully")
			return nil
		},
	}

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

			// that code is just for an example and will be replaced later
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}
			keyRecord, err := clientCtx.Keyring.Key(relayerKeyName)
			if err != nil {
				return errors.Wrap(err, "failed to get key from keyring")
			}
			address, err := keyRecord.GetAddress()
			if err != nil {
				return errors.Wrap(err, "failed to get address from the key record")
			}
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(time.Second):
					log.Info(ctx, "Address from the keyring extracted.", logger.StringField("address", address.String()))
				}
			}
		},
	}
	addKeyringFlags(cmd)

	return cmd
}

// KeyringCmd returns cosmos keyring cmd inti with the correct keys home.
func KeyringCmd() *cobra.Command {
	return keys.Commands(path.Join(DefaultHomeDir, "keys"))
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

func addKeyringFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String(
		flags.FlagKeyringBackend,
		flags.DefaultKeyringBackend,
		"Select keyring's backend (os|file|kwallet|pass|test)",
	)
	cmd.PersistentFlags().String(
		flags.FlagKeyringDir,
		path.Join(DefaultHomeDir, "keys"),
		"The client Keyring directory; if omitted, the default 'home' directory will be used",
	)
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
