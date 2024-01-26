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
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/golang/mock/gomock"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	coreumapp "github.com/CoreumFoundation/coreum/v4/app"
	"github.com/CoreumFoundation/coreum/v4/pkg/config"
	"github.com/CoreumFoundation/coreum/v4/pkg/config/constant"
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

	cmd, err := cli.KeyringCmd(coreum.KeyringSuffix, constant.CoinType)
	require.NoError(t, err)

	configPath := t.TempDir()
	configFilePath := path.Join(configPath, runner.ConfigFileName)
	require.NoFileExists(t, configFilePath)
	args := []string{
		flagWithPrefix(cli.FlagHome), configPath,
	}
	executeCmd(t, cli.InitCmd(), args...)

	args = []string{
		"list",
		flagWithPrefix(cli.FlagHome), configPath,
	}
	args = append(args, testKeyringFlags(configPath)...)
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
	addKeyToTestKeyring(t, keyringDir, runnerDefaultCfg.XRPL.MultiSignerKeyName, xrpl.KeyringSuffix, xrpl.XRPLHDPath)
	addKeyToTestKeyring(t, keyringDir, runnerDefaultCfg.Coreum.RelayerKeyName, coreum.KeyringSuffix,
		sdk.GetConfig().GetFullBIP44Path())

	args = append(args, testKeyringFlags(keyringDir)...)
	executeCmd(t, cli.RelayerKeyInfoCmd(), args...)
}

func TestBootstrapCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configPath := path.Join(t.TempDir(), "bootstrapping.yaml")

	keyringDir := t.TempDir()
	keyName := "deployer"
	addKeyToTestKeyring(t, keyringDir, keyName, xrpl.KeyringSuffix, xrpl.XRPLHDPath)

	// call bootstrap with init only
	args := []string{
		configPath,
		flagWithPrefix(cli.FlagInitOnly),
		flagWithPrefix(cli.FlagRelayersCount), "3",
		flagWithPrefix(cli.FlagKeyName), keyName,
	}
	args = append(args, testKeyringFlags(keyringDir)...)
	executeCmd(t, cli.BootstrapBridgeCmd(nil), args...)

	// use generated file
	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().Bootstrap(gomock.Any(), gomock.Any(), keyName, bridgeclient.DefaultBootstrappingConfig())
	args = []string{
		configPath,
		flagWithPrefix(cli.FlagKeyName), keyName,
	}
	args = append(args, testKeyringFlags(keyringDir)...)
	executeCmd(t, cli.BootstrapBridgeCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestContractConfigCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().GetContractConfig(gomock.Any()).Return(coreum.ContractConfig{}, nil)
	executeCmd(t, cli.ContractConfigCmd(mockBridgeClientProvider(bridgeClientMock)))
}

func TestRecoverTicketsCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner" //nolint:goconst // testing only variable
	addKeyToTestKeyring(t, keyringDir, keyName, xrpl.KeyringSuffix, xrpl.XRPLHDPath)

	args := []string{
		flagWithPrefix(cli.FlagKeyName), keyName,
	}
	args = append(args, testKeyringFlags(keyringDir)...)

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().RecoverTickets(gomock.Any(), gomock.Any(), xrpl.MaxTicketsToAllocate)
	executeCmd(t, cli.RecoverTicketsCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestRegisterCoreumTokenCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	denom := "denom"
	decimals := 10
	sendingPrecision := 12
	maxHoldingAmount := 10000
	args := []string{
		denom,
		strconv.Itoa(decimals),
		strconv.Itoa(sendingPrecision),
		strconv.Itoa(maxHoldingAmount),
		"1",
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
		sdkmath.NewInt(1),
	)
	executeCmd(t, cli.RegisterCoreumTokenCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestUpdateCoreumTokenCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, sdk.GetConfig().GetFullBIP44Path())
	denom := "denom"

	tests := []struct {
		name string
		args []string
		mock func(m *MockBridgeClient)
	}{
		{
			name: "no_additional_flags",
			args: []string{
				denom,
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateCoreumToken(
					gomock.Any(),
					gomock.Any(),
					denom,
					nil,
					nil,
					nil,
					nil,
				)
			},
		},
		{
			name: "negative_sending_precision",
			args: []string{
				denom,
				flagWithPrefix(cli.FlagSendingPrecision), strconv.Itoa(-2),
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateCoreumToken(
					gomock.Any(),
					gomock.Any(),
					denom,
					nil,
					mock.MatchedBy(func(v *int32) bool {
						return *v == -2
					}),
					nil,
					nil,
				)
			},
		},
		{
			name: "zero_sending_precision",
			args: []string{
				denom,
				flagWithPrefix(cli.FlagSendingPrecision), strconv.Itoa(0),
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateCoreumToken(
					gomock.Any(),
					gomock.Any(),
					denom,
					nil,
					mock.MatchedBy(func(v *int32) bool {
						return *v == 0
					}),
					nil,
					nil,
				)
			},
		},
		{
			name: "positive_sending_precision",
			args: []string{
				denom,
				flagWithPrefix(cli.FlagSendingPrecision), strconv.Itoa(2),
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateCoreumToken(
					gomock.Any(),
					gomock.Any(),
					denom,
					nil,
					mock.MatchedBy(func(v *int32) bool {
						return *v == 2
					}),
					nil,
					nil,
				)
			},
		},
		{
			name: "token_state_update",
			args: []string{
				denom,
				flagWithPrefix(cli.FlagTokenState), string(coreum.TokenStateEnabled),
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateCoreumToken(
					gomock.Any(),
					gomock.Any(),
					denom,
					mock.MatchedBy(func(v *coreum.TokenState) bool {
						return *v == coreum.TokenStateEnabled
					}),
					nil,
					nil,
					nil,
				)
			},
		},
		{
			name: "sending_precision_and_token_state_update",
			args: []string{
				denom,
				flagWithPrefix(cli.FlagSendingPrecision), strconv.Itoa(2),
				flagWithPrefix(cli.FlagTokenState), string(coreum.TokenStateEnabled),
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateCoreumToken(
					gomock.Any(),
					gomock.Any(),
					denom,
					mock.MatchedBy(func(v *coreum.TokenState) bool {
						return *v == coreum.TokenStateEnabled
					}),
					mock.MatchedBy(func(v *int32) bool {
						return *v == 2
					}),
					nil,
					nil,
				)
			},
		},
		{
			name: "max_holding_amount_update",
			args: []string{
				denom,
				flagWithPrefix(cli.FlagMaxHoldingAmount), "77",
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateCoreumToken(
					gomock.Any(),
					gomock.Any(),
					denom,
					nil,
					nil,
					mock.MatchedBy(func(v *sdkmath.Int) bool {
						return v.String() == "77"
					}),
					nil,
				)
			},
		},
		{
			name: "sending_precision_and_max_holding_amount_update",
			args: []string{
				denom,
				flagWithPrefix(cli.FlagSendingPrecision), strconv.Itoa(2),
				flagWithPrefix(cli.FlagMaxHoldingAmount), "77",
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateCoreumToken(
					gomock.Any(),
					gomock.Any(),
					denom,
					nil,
					mock.MatchedBy(func(v *int32) bool {
						return *v == 2
					}),
					mock.MatchedBy(func(v *sdkmath.Int) bool {
						return v.String() == "77"
					}),
					nil,
				)
			},
		},
		{
			name: "bridging_fee_update",
			args: []string{
				denom,
				flagWithPrefix(cli.FlagBridgingFee), "9999",
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateCoreumToken(
					gomock.Any(),
					gomock.Any(),
					denom,
					nil,
					nil,
					nil,
					mock.MatchedBy(func(v *sdkmath.Int) bool {
						return v.String() == "9999"
					}),
				)
			},
		},
		{
			name: "sending_precision_and_bridging_fee_update",
			args: []string{
				denom,
				flagWithPrefix(cli.FlagSendingPrecision), strconv.Itoa(2),
				flagWithPrefix(cli.FlagBridgingFee), "9999",
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateCoreumToken(
					gomock.Any(),
					gomock.Any(),
					denom,
					nil,
					mock.MatchedBy(func(v *int32) bool {
						return *v == 2
					}),
					nil,
					mock.MatchedBy(func(v *sdkmath.Int) bool {
						return v.String() == "9999"
					}),
				)
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// no additional args
			tt.args = append(tt.args, testKeyringFlags(keyringDir)...)
			bridgeClientMock := NewMockBridgeClient(ctrl)
			tt.mock(bridgeClientMock)
			executeCmd(t, cli.UpdateCoreumTokenCmd(mockBridgeClientProvider(bridgeClientMock)), tt.args...)
		})
	}
}

func TestRegisterXRPLTokenCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

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
		"1",
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
		sdkmath.NewInt(1),
	)
	executeCmd(t, cli.RegisterXRPLTokenCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestUpdateXRPLTokenCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, sdk.GetConfig().GetFullBIP44Path())
	issuer := "rcoreNywaoz2ZCQ8Lg2EbSLnGuRBmun6D"
	currency := "434F524500000000000000000000000000000000"

	tests := []struct {
		name string
		args []string
		mock func(m *MockBridgeClient)
	}{
		{
			name: "no_additional_flags",
			args: []string{
				issuer,
				currency,
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateXRPLToken(
					gomock.Any(),
					gomock.Any(),
					issuer,
					currency,
					nil,
					nil,
					nil,
					nil,
				)
			},
		},
		{
			name: "negative_sending_precision",
			args: []string{
				issuer,
				currency,
				flagWithPrefix(cli.FlagSendingPrecision), strconv.Itoa(-2),
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateXRPLToken(
					gomock.Any(),
					gomock.Any(),
					issuer,
					currency,
					nil,
					mock.MatchedBy(func(v *int32) bool {
						return *v == -2
					}),
					nil,
					nil,
				)
			},
		},
		{
			name: "zero_sending_precision",
			args: []string{
				issuer,
				currency,
				flagWithPrefix(cli.FlagSendingPrecision), strconv.Itoa(0),
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateXRPLToken(
					gomock.Any(),
					gomock.Any(),
					issuer,
					currency,
					nil,
					mock.MatchedBy(func(v *int32) bool {
						return *v == 0
					}),
					nil,
					nil,
				)
			},
		},
		{
			name: "positive_sending_precision",
			args: []string{
				issuer,
				currency,
				flagWithPrefix(cli.FlagSendingPrecision), strconv.Itoa(2),
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateXRPLToken(
					gomock.Any(),
					gomock.Any(),
					issuer,
					currency,
					nil,
					mock.MatchedBy(func(v *int32) bool {
						return *v == 2
					}),
					nil,
					nil,
				)
			},
		},
		{
			name: "token_state_update",
			args: []string{
				issuer,
				currency,
				flagWithPrefix(cli.FlagTokenState), string(coreum.TokenStateEnabled),
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateXRPLToken(
					gomock.Any(),
					gomock.Any(),
					issuer,
					currency,
					mock.MatchedBy(func(v *coreum.TokenState) bool {
						return *v == coreum.TokenStateEnabled
					}),
					nil,
					nil,
					nil,
				)
			},
		},
		{
			name: "sending_precision_and_token_state_update",
			args: []string{
				issuer,
				currency,
				flagWithPrefix(cli.FlagSendingPrecision), strconv.Itoa(2),
				flagWithPrefix(cli.FlagTokenState), string(coreum.TokenStateEnabled),
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateXRPLToken(
					gomock.Any(),
					gomock.Any(),
					issuer,
					currency,
					mock.MatchedBy(func(v *coreum.TokenState) bool {
						return *v == coreum.TokenStateEnabled
					}),
					mock.MatchedBy(func(v *int32) bool {
						return *v == 2
					}),
					nil,
					nil,
				)
			},
		},
		{
			name: "max_holding_amount_update",
			args: []string{
				issuer,
				currency,
				flagWithPrefix(cli.FlagMaxHoldingAmount), "66",
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateXRPLToken(
					gomock.Any(),
					gomock.Any(),
					issuer,
					currency,
					nil,
					nil,
					mock.MatchedBy(func(v *sdkmath.Int) bool {
						return v.String() == "66"
					}),
					nil,
				)
			},
		},
		{
			name: "sending_precision_and_max_holding_amount_update",
			args: []string{
				issuer,
				currency,
				flagWithPrefix(cli.FlagSendingPrecision), strconv.Itoa(2),
				flagWithPrefix(cli.FlagMaxHoldingAmount), "66",
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateXRPLToken(
					gomock.Any(),
					gomock.Any(),
					issuer,
					currency,
					nil,
					mock.MatchedBy(func(v *int32) bool {
						return *v == 2
					}),
					mock.MatchedBy(func(v *sdkmath.Int) bool {
						return v.String() == "66"
					}),
					nil,
				)
			},
		},
		{
			name: "bridging_fee_update",
			args: []string{
				issuer,
				currency,
				flagWithPrefix(cli.FlagBridgingFee), "9999",
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateXRPLToken(
					gomock.Any(),
					gomock.Any(),
					issuer,
					currency,
					nil,
					nil,
					nil,
					mock.MatchedBy(func(v *sdkmath.Int) bool {
						return v.String() == "9999"
					}),
				)
			},
		},
		{
			name: "sending_precision_and_bridging_fee_update",
			args: []string{
				issuer,
				currency,
				flagWithPrefix(cli.FlagSendingPrecision), strconv.Itoa(2),
				flagWithPrefix(cli.FlagBridgingFee), "9999",
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().UpdateXRPLToken(
					gomock.Any(),
					gomock.Any(),
					issuer,
					currency,
					nil,
					mock.MatchedBy(func(v *int32) bool {
						return *v == 2
					}),
					nil,
					mock.MatchedBy(func(v *sdkmath.Int) bool {
						return v.String() == "9999"
					}),
				)
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// no additional args
			tt.args = append(tt.args, testKeyringFlags(keyringDir)...)
			bridgeClientMock := NewMockBridgeClient(ctrl)
			tt.mock(bridgeClientMock)
			executeCmd(t, cli.UpdateXRPLTokenCmd(mockBridgeClientProvider(bridgeClientMock)), tt.args...)
		})
	}
}

func TestRotateKeysCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configPath := path.Join(t.TempDir(), "new-keys.yaml")

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName)

	// call rotate-keys with init only
	args := []string{
		configPath,
		flagWithPrefix(cli.FlagInitOnly),
		flagWithPrefix(cli.FlagKeyName), keyName,
	}
	args = append(args, testKeyringFlags(keyringDir)...)
	executeCmd(t, cli.RotateKeysCmd(nil), args...)

	// use generated file
	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().RotateKeys(gomock.Any(), gomock.Any(), bridgeclient.DefaultKeysRotationConfig())
	args = []string{
		configPath,
		flagWithPrefix(cli.FlagKeyName), keyName,
	}
	args = append(args, testKeyringFlags(keyringDir)...)
	executeCmd(t, cli.RotateKeysCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestRegisteredTokensCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().GetAllTokens(gomock.Any()).Return([]coreum.CoreumToken{}, []coreum.XRPLToken{}, nil)
	executeCmd(t, cli.RegisteredTokensCmd(mockBridgeClientProvider(bridgeClientMock)))
}

func TestSendFromCoreumToXRPLCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "sender"
	addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

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
	executeCmd(t, cli.SendFromCoreumToXRPLCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestCoreumBalancesCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	account := coreum.GenAccount()
	bridgeClientMock.EXPECT().GetCoreumBalances(gomock.Any(), account).Return(sdk.NewCoins(), nil)
	executeCmd(t, cli.CoreumBalancesCmd(mockBridgeClientProvider(bridgeClientMock)), account.String())
}

func TestXRPBalancesCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	account := xrpl.GenPrivKeyTxSigner().Account()
	bridgeClientMock.EXPECT().GetXRPLBalances(gomock.Any(), account).Return([]rippledata.Amount{}, nil)
	executeCmd(t, cli.XRPLBalancesCmd(mockBridgeClientProvider(bridgeClientMock)), account.String())
}

func TestSetXRPLTrustSetCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "sender"
	addKeyToTestKeyring(t, keyringDir, keyName, xrpl.KeyringSuffix, xrpl.XRPLHDPath)

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
	executeCmd(t, cli.SetXRPLTrustSetCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
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

func addKeyToTestKeyring(t *testing.T, keyringDir, keyName, suffix, hdPath string) {
	keyringDir += "-" + suffix
	encodingConfig := config.NewEncodingConfig(coreumapp.ModuleBasics)
	clientCtx := client.Context{}.
		WithCodec(encodingConfig.Codec).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithLegacyAmino(encodingConfig.Amino).
		WithInput(os.Stdin).
		WithOutputFormat("text").
		WithKeyringDir(keyringDir)

	kr, err := client.NewKeyringFromBackend(clientCtx, keyring.BackendTest)
	require.NoError(t, err)

	_, _, err = kr.NewMnemonic(
		keyName,
		keyring.English,
		hdPath,
		"",
		hd.Secp256k1,
	)
	require.NoError(t, err)
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

func mockBridgeClientProvider(bridgeClientMock *MockBridgeClient) cli.BridgeClientProvider {
	return func(cmd *cobra.Command) (cli.BridgeClient, error) {
		return bridgeClientMock, nil
	}
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
