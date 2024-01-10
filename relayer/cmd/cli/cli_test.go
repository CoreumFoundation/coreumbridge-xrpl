package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"reflect"
	"strconv"
	"testing"
	"unsafe"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	krflags "github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/keys"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/golang/mock/gomock"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	coreumapp "github.com/CoreumFoundation/coreum/v4/app"
	"github.com/CoreumFoundation/coreum/v4/pkg/config"
	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/cmd/cli"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestInitCmd(t *testing.T) {
	configPath := path.Join(t.TempDir(), "config-path")
	configFilePath := path.Join(configPath, runner.ConfigFileName)
	require.NoFileExists(t, configFilePath)

	args := []string{
		flagWithPrefix(cli.FlagHome), configPath,
	}
	executeCmd(t, cli.InitCmd(), args...)
	require.FileExists(t, configFilePath)
}

func TestStartCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	processorMock := NewMockProcessor(ctrl)
	processorMock.EXPECT().StartAllProcesses(gomock.Any())
	cmd := cli.StartCmd(func(cmd *cobra.Command) (cli.Processor, error) {
		return processorMock, nil
	})
	executeCmd(t, cmd)
}

func TestKeyringCmds(t *testing.T) {
	unsealConfig()

	cmd, err := cli.KeyringCmd()
	require.NoError(t, err)

	args := []string{
		"list",
	}
	args = append(args, testKeyringFlags(t.TempDir())...)
	out := executeCmd(t, cmd, args...)
	keysOut := make([]string, 0)
	require.NoError(t, json.Unmarshal([]byte(out), &keysOut))
	require.Empty(t, keysOut)
}

func TestRelayerKeyInfoCmd(t *testing.T) {
	unsealConfig()

	// init default config
	configPath := path.Join(t.TempDir(), "config-path")
	configFilePath := path.Join(configPath, runner.ConfigFileName)
	require.NoFileExists(t, configFilePath)

	args := []string{
		flagWithPrefix(cli.FlagHome), configPath,
	}
	executeCmd(t, cli.InitCmd(), args...)
	// add required keys
	keyringDir := t.TempDir()
	runnerDefaultCfg := runner.DefaultConfig()
	addKeyToTestKeyring(t, keyringDir, runnerDefaultCfg.XRPL.MultiSignerKeyName)
	addKeyToTestKeyring(t, keyringDir, runnerDefaultCfg.Coreum.RelayerKeyName)

	args = append(args, testKeyringFlags(keyringDir)...)
	executeCmd(t, cli.RelayerKeyInfoCmd(), args...)
}

func TestBootstrapCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configPath := path.Join(t.TempDir(), "bootstrapping.yaml")

	keyringDir := t.TempDir()
	keyName := "deployer"
	addKeyToTestKeyring(t, keyringDir, keyName)

	// call bootstrap with init only
	cmd := cli.BootstrapBridgeCmd(func(cmd *cobra.Command) (cli.BridgeClient, error) {
		return nil, nil
	})
	args := []string{
		configPath,
		flagWithPrefix(cli.FlagInitOnly),
		flagWithPrefix(cli.FlagRelayersCount), "3",
		flagWithPrefix(cli.FlagKeyName), keyName,
	}
	args = append(args, testKeyringFlags(keyringDir)...)
	executeCmd(t, cmd, args...)

	// use generated file
	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().Bootstrap(gomock.Any(), gomock.Any(), keyName, bridgeclient.DefaultBootstrappingConfig())
	cmd = cli.BootstrapBridgeCmd(func(cmd *cobra.Command) (cli.BridgeClient, error) {
		return bridgeClientMock, nil
	})
	args = []string{
		configPath,
		flagWithPrefix(cli.FlagKeyName), keyName,
	}
	args = append(args, testKeyringFlags(keyringDir)...)
	executeCmd(t, cmd, args...)
}

func TestContractConfigCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().GetContractConfig(gomock.Any()).Return(coreum.ContractConfig{}, nil)
	cmd := cli.ContractConfigCmd(func(cmd *cobra.Command) (cli.BridgeClient, error) {
		return bridgeClientMock, nil
	})
	executeCmd(t, cmd)
}

func TestRecoverTicketsCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner" //nolint:goconst // testing only variable
	addKeyToTestKeyring(t, keyringDir, keyName)

	args := []string{
		flagWithPrefix(cli.FlagKeyName), keyName,
	}
	args = append(args, testKeyringFlags(keyringDir)...)

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().RecoverTickets(gomock.Any(), gomock.Any(), xrpl.MaxTicketsToAllocate)
	cmd := cli.RecoverTicketsCmd(func(cmd *cobra.Command) (cli.BridgeClient, error) {
		return bridgeClientMock, nil
	})
	executeCmd(t, cmd, args...)
}

func TestRegisterCoreumTokenCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName)

	denom := "denom"
	decimals := 10
	sendingPrecision := 12
	maxHoldingAmount := 10000
	args := []string{
		denom,
		strconv.Itoa(decimals),
		strconv.Itoa(sendingPrecision),
		strconv.Itoa(maxHoldingAmount),
		flagWithPrefix(cli.FlagKeyName), keyName,
	}
	args = append(args, testKeyringFlags(keyringDir)...)

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().RegisterCoreumToken(
		gomock.Any(),
		gomock.Any(),
		denom,
		uint32(decimals),
		int32(sendingPrecision),
		sdkmath.NewInt(int64(maxHoldingAmount)),
	)
	cmd := cli.RegisterCoreumTokenCmd(func(cmd *cobra.Command) (cli.BridgeClient, error) {
		return bridgeClientMock, nil
	})
	executeCmd(t, cmd, args...)
}

func TestRegisterXRPLTokenCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName)

	issuer := xrpl.GenPrivKeyTxSigner().Account()
	currency, err := rippledata.NewCurrency("CRN")
	require.NoError(t, err)
	sendingPrecision := 12
	maxHoldingAmount := 10000
	args := []string{
		issuer.String(),
		currency.String(),
		strconv.Itoa(sendingPrecision),
		strconv.Itoa(maxHoldingAmount),
		flagWithPrefix(cli.FlagKeyName), keyName,
	}
	args = append(args, testKeyringFlags(keyringDir)...)

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().RegisterXRPLToken(
		gomock.Any(),
		gomock.Any(),
		issuer,
		currency,
		int32(sendingPrecision),
		sdkmath.NewInt(int64(maxHoldingAmount)),
	)
	cmd := cli.RegisterXRPLTokenCmd(func(cmd *cobra.Command) (cli.BridgeClient, error) {
		return bridgeClientMock, nil
	})
	executeCmd(t, cmd, args...)
}

func TestRegisteredTokensCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().GetAllTokens(gomock.Any()).Return([]coreum.CoreumToken{}, []coreum.XRPLToken{}, nil)
	cmd := cli.RegisteredTokensCmd(func(cmd *cobra.Command) (cli.BridgeClient, error) {
		return bridgeClientMock, nil
	})
	executeCmd(t, cmd)
}

func TestSendFromCoreumToXRPLCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "sender"
	addKeyToTestKeyring(t, keyringDir, keyName)

	recipient := xrpl.GenPrivKeyTxSigner().Account()
	amount := sdk.NewInt64Coin("denom", 1000)
	args := []string{
		amount.String(),
		recipient.String(),
		flagWithPrefix(cli.FlagKeyName), keyName,
	}
	args = append(args, testKeyringFlags(keyringDir)...)

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().SendFromCoreumToXRPL(
		gomock.Any(),
		gomock.Any(),
		amount,
		recipient,
	)
	cmd := cli.SendFromCoreumToXRPLCmd(func(cmd *cobra.Command) (cli.BridgeClient, error) {
		return bridgeClientMock, nil
	})
	executeCmd(t, cmd, args...)
}

func TestCoreumBalancesCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	account := coreum.GenAccount()
	bridgeClientMock.EXPECT().GetCoreumBalances(gomock.Any(), account).Return(sdk.NewCoins(), nil)
	cmd := cli.CoreumBalancesCmd(func(cmd *cobra.Command) (cli.BridgeClient, error) {
		return bridgeClientMock, nil
	})
	executeCmd(t, cmd, account.String())
}

func TestXRPBalancesCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	account := xrpl.GenPrivKeyTxSigner().Account()
	bridgeClientMock.EXPECT().GetXRPLBalances(gomock.Any(), account).Return([]rippledata.Amount{}, nil)
	cmd := cli.XRPLBalancesCmd(func(cmd *cobra.Command) (cli.BridgeClient, error) {
		return bridgeClientMock, nil
	})
	executeCmd(t, cmd, account.String())
}

func TestSetXRPLTrustSetCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "sender"
	addKeyToTestKeyring(t, keyringDir, keyName)

	value, err := rippledata.NewValue("100", false)
	require.NoError(t, err)
	issuer := xrpl.GenPrivKeyTxSigner().Account()
	currency, err := rippledata.NewCurrency("CRN")
	require.NoError(t, err)
	amount := rippledata.Amount{
		Value:    value,
		Currency: currency,
		Issuer:   issuer,
	}
	args := []string{
		amount.Value.String(),
		amount.Issuer.String(),
		amount.Currency.String(),
		flagWithPrefix(cli.FlagKeyName), keyName,
	}
	args = append(args, testKeyringFlags(keyringDir)...)

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().SetXRPLTrustSet(
		gomock.Any(),
		gomock.Any(),
		amount,
	)
	cmd := cli.SetXRPLTrustSetCmd(func(cmd *cobra.Command) (cli.BridgeClient, error) {
		return bridgeClientMock, nil
	})
	executeCmd(t, cmd, args...)
}

func executeCmd(t *testing.T, cmd *cobra.Command, args ...string) string {
	t.Helper()

	cmd.SetArgs(args)

	buf := new(bytes.Buffer)
	cmd.SetErr(buf)
	cmd.SetOut(buf)
	cmd.SetArgs(args)

	encodingConfig := config.NewEncodingConfig(coreumapp.ModuleBasics)
	clientCtx := client.Context{}.
		WithCodec(encodingConfig.Codec).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithLegacyAmino(encodingConfig.Amino).
		WithInput(os.Stdin).
		WithOutputFormat("text")
	ctx := context.WithValue(context.Background(), client.ClientContextKey, &clientCtx)

	if err := cmd.ExecuteContext(ctx); err != nil {
		require.NoError(t, err)
	}

	t.Logf("Command %s is executed successfully", cmd.Name())

	return buf.String()
}

func addKeyToTestKeyring(t *testing.T, keyringDir, keyName string) {
	args := []string{
		keyName,
	}
	args = append(args, testKeyringFlags(keyringDir)...)
	cmd := keys.AddKeyCommand()
	krflags.AddKeyringFlags(cmd.PersistentFlags())
	executeCmd(t, cmd, args...)
}

func testKeyringFlags(keyringDir string) []string {
	return []string{
		flagWithPrefix(krflags.FlagKeyringBackend), "test",
		flagWithPrefix(krflags.FlagKeyringDir), keyringDir,
	}
}

func flagWithPrefix(f string) string {
	return fmt.Sprintf("--%s", f)
}

func unsealConfig() {
	sdkConfig := sdk.GetConfig()
	unsafeSetField(sdkConfig, "sealed", false)
	unsafeSetField(sdkConfig, "sealedch", make(chan struct{}))
}

func unsafeSetField(object interface{}, fieldName string, value interface{}) {
	rs := reflect.ValueOf(object).Elem()
	field := rs.FieldByName(fieldName)
	// rf can't be read or set.
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).
		Elem().
		Set(reflect.ValueOf(value))
}
