package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/CoreumFoundation/coreum-tools/pkg/run"
	coruemapp "github.com/CoreumFoundation/coreum/v3/app"
	"github.com/CoreumFoundation/coreum/v3/pkg/config"
)

var defaultKeyringDir = path.Join(".coreumbridge-xrpl-relayer", "keys")

const (
	// That key name is constant here temporary, we will take it from the relayer config later.
	relayerKeyName = "coreumbridge-xrpl-relayer"
)

func main() {
	run.Tool("CoreumbridgeXRPLRelayer", func(ctx context.Context) error {
		rootCmd := RootCmd(ctx)
		if err := rootCmd.Execute(); err != nil && !errors.Is(err, context.Canceled) {
			// TODO(dzmitryhil) replace to logger once we integrate the runner
			fmt.Printf("Failed to executed root cmd, err:%s.\n", err.Error())
			return err
		}

		return nil
	})
}

// RootCmd returns the root cmd.
func RootCmd(ctx context.Context) *cobra.Command {
	encodingConfig := config.NewEncodingConfig(coruemapp.ModuleBasics)
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

	cmd.AddCommand(StartCmd(ctx))
	cmd.AddCommand(keys.Commands(defaultKeyringDir))

	return cmd
}

// StartCmd returns the start cmd.
func StartCmd(ctx context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start relayer.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// scan helps to wait for any input infinitely and just then call the relayer. That handles
			// the relayer restart in the container. Because after the restart the container is detached, relayer
			// requests the keyring password and fail inanimately.
			// TODO(dzmitryhil) replace to logger once we integrate the runner
			fmt.Print("Press any key to start the relayer.")
			input := bufio.NewScanner(os.Stdin)
			input.Scan()

			// that code is just for an example and will be replaced later
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return errors.Wrap(err, "failed to get client context")
			}
			keyRecord, err := clientCtx.Keyring.Key(relayerKeyName)
			if err != nil {
				return err
			}
			address, err := keyRecord.GetAddress()
			if err != nil {
				return err
			}
			for {
				select {
				case <-ctx.Done():
					return nil
				default:
					fmt.Printf("Address from the keyring:%s\n", address.String())
					<-time.After(time.Second)
				}
			}
		},
	}
	addKeyringFlags(cmd)

	return cmd
}

func addKeyringFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String(flags.FlagKeyringBackend, flags.DefaultKeyringBackend, "Select keyring's backend (os|file|kwallet|pass|test)")
	cmd.PersistentFlags().String(flags.FlagKeyringDir, defaultKeyringDir, "The client Keyring directory; if omitted, the default 'home' directory will be used")
}
