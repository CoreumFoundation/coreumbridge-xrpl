package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strconv"
	"testing"

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
	initConfig(t)
}

func TestStartCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	processorMock := NewMockRunner(ctrl)
	processorMock.EXPECT().Start(gomock.Any())
	cmd := cli.StartCmd(func(cmd *cobra.Command) (cli.Runner, error) {
		return processorMock, nil
	})
	executeCmd(t, cmd, initConfig(t)...) // to disable telemetry server
}

func TestKeyringCmds(t *testing.T) {
	cmd, err := cli.KeyringCmd(coreum.KeyringSuffix, constant.CoinType)
	require.NoError(t, err)

	args := append(initConfig(t), "list")
	args = append(args, testKeyringFlags(t.TempDir())...)

	out := executeCmd(t, cmd, args...)
	keysOut := make([]string, 0)
	require.NoError(t, json.Unmarshal([]byte(out), &keysOut))
	require.Empty(t, keysOut)
}

func TestRelayerKeyInfoCmd(t *testing.T) {
	keyringDir := t.TempDir()
	args := append(initConfig(t), testKeyringFlags(keyringDir)...)
	runnerDefaultCfg := runner.DefaultConfig()

	// add required keys
	addKeyToTestKeyring(t, keyringDir, runnerDefaultCfg.XRPL.MultiSignerKeyName, xrpl.KeyringSuffix, xrpl.XRPLHDPath)
	addKeyToTestKeyring(t, keyringDir, runnerDefaultCfg.Coreum.RelayerKeyName, coreum.KeyringSuffix,
		sdk.GetConfig().GetFullBIP44Path())

	executeCmd(t, cli.RelayerKeyInfoCmd(), args...)
}

func TestBootstrapCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bootstrapConfigPath := path.Join(t.TempDir(), "bootstrapping.yaml")

	keyringDir := t.TempDir()
	xrplKeyName := "xrpl-bridge"
	addKeyToTestKeyring(t, keyringDir, xrplKeyName, xrpl.KeyringSuffix, xrpl.XRPLHDPath)
	contractDeployer := "contract-deployer"
	addKeyToTestKeyring(t, keyringDir, contractDeployer, coreum.KeyringSuffix, xrpl.XRPLHDPath)

	homeArgs := initConfig(t)
	// call bootstrap with init only

	args := append([]string{
		bootstrapConfigPath,
		flagWithPrefix(cli.FlagInitOnly),
		flagWithPrefix(cli.FlagRelayersCount), "3",
		flagWithPrefix(cli.FlagXRPLKeyName), xrplKeyName,
		flagWithPrefix(cli.FlagCoreumKeyName), contractDeployer,
	}, homeArgs...)
	args = append(args, testKeyringFlags(keyringDir)...)
	executeCmd(t, cli.BootstrapBridgeCmd(mockBridgeClientProvider(nil)), args...)

	// use generated file
	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().Bootstrap(gomock.Any(), gomock.Any(), xrplKeyName, bridgeclient.DefaultBootstrappingConfig())
	args = append([]string{
		bootstrapConfigPath,
		flagWithPrefix(cli.FlagXRPLKeyName), xrplKeyName,
		flagWithPrefix(cli.FlagCoreumKeyName), contractDeployer,
	}, homeArgs...)
	args = append(args, testKeyringFlags(keyringDir)...)
	executeCmd(t, cli.BootstrapBridgeCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestContractConfigCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().GetContractConfig(gomock.Any()).Return(coreum.ContractConfig{}, nil)
	executeCmd(t, cli.ContractConfigCmd(mockBridgeClientProvider(bridgeClientMock)), initConfig(t)...)
}

func TestRecoverTicketsCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner" //nolint:goconst // testing only variable
	addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, xrpl.XRPLHDPath)

	homeArgs := initConfig(t)

	args := append([]string{
		flagWithPrefix(cli.FlagKeyName), keyName,
	}, homeArgs...)
	args = append(args, testKeyringFlags(keyringDir)...)
	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().RecoverTickets(gomock.Any(), gomock.Any(), nil)
	executeCmd(t, cli.RecoverTicketsCmd(mockBridgeClientProvider(bridgeClientMock)), args...)

	// with tickets
	args = append([]string{
		flagWithPrefix(cli.FlagTicketsToAllocate), "123",
		flagWithPrefix(cli.FlagKeyName), keyName,
	}, homeArgs...)
	args = append(args, testKeyringFlags(keyringDir)...)
	bridgeClientMock = NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().RecoverTickets(
		gomock.Any(),
		gomock.Any(),
		mock.MatchedBy(func(v *uint32) bool {
			return *v == 123
		}),
	)
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
	args := append(initConfig(t),
		denom,
		strconv.Itoa(decimals),
		strconv.Itoa(sendingPrecision),
		strconv.Itoa(maxHoldingAmount),
		"1",
		flagWithPrefix(cli.FlagKeyName), keyName,
	)
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
			executeCmd(t, cli.UpdateCoreumTokenCmd(mockBridgeClientProvider(bridgeClientMock)),
				append(initConfig(t), tt.args...)...)
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
	args := append(initConfig(t),
		issuer.String(),
		currency.String(),
		strconv.Itoa(sendingPrecision),
		strconv.Itoa(maxHoldingAmount),
		"1",
		flagWithPrefix(cli.FlagKeyName), keyName,
	)
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

func TestRecoverXRPLTokenRegistrationCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner"
	owner := addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	issuer := xrpl.GenPrivKeyTxSigner().Account()
	currency, err := rippledata.NewCurrency("CRN")
	require.NoError(t, err)
	args := append(initConfig(t),
		issuer.String(),
		currency.String(),
		flagWithPrefix(cli.FlagKeyName), keyName,
	)
	args = append(args, testKeyringFlags(keyringDir)...)

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().RecoverXRPLTokenRegistration(
		gomock.Any(),
		owner,
		issuer.String(),
		currency.String(),
	).Return(nil)
	executeCmd(t, cli.RecoverXRPLTokenRegistrationCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
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
			executeCmd(t, cli.UpdateXRPLTokenCmd(mockBridgeClientProvider(bridgeClientMock)),
				append(initConfig(t), tt.args...)...)
		})
	}
}

func TestRotateKeysCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configPath := path.Join(t.TempDir(), "new-keys.yaml")

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	homeArgs := initConfig(t)

	// call rotate-keys with init only
	args := append([]string{
		configPath,
		flagWithPrefix(cli.FlagInitOnly),
		flagWithPrefix(cli.FlagKeyName), keyName,
	}, homeArgs...)
	args = append(args, testKeyringFlags(keyringDir)...)
	executeCmd(t, cli.RotateKeysCmd(mockBridgeClientProvider(nil)), args...)

	// use generated file
	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().RotateKeys(gomock.Any(), gomock.Any(), bridgeclient.DefaultKeysRotationConfig())
	args = append([]string{
		configPath,
		flagWithPrefix(cli.FlagKeyName), keyName,
	}, homeArgs...)
	args = append(args, testKeyringFlags(keyringDir)...)
	executeCmd(t, cli.RotateKeysCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestUpdateXRPLBaseFeeCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	// call rotate-keys with init only
	args := append(initConfig(t),
		"17",
		flagWithPrefix(cli.FlagKeyName), keyName,
	)
	args = append(args, testKeyringFlags(keyringDir)...)
	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().UpdateXRPLBaseFee(gomock.Any(), gomock.Any(), uint32(17))
	executeCmd(t, cli.UpdateXRPLBaseFeeCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestRegisteredTokensCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().GetAllTokens(gomock.Any()).Return([]coreum.CoreumToken{}, []coreum.XRPLToken{}, nil)
	executeCmd(t, cli.RegisteredTokensCmd(mockBridgeClientProvider(bridgeClientMock)), initConfig(t)...)
}

func TestSendFromCoreumToXRPLCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "sender"
	addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	homeArgs := initConfig(t)

	recipient := xrpl.GenPrivKeyTxSigner().Account()
	amount := sdk.NewInt64Coin("denom", 1000)
	deliverAmount := sdkmath.NewInt(900)
	args := append([]string{
		amount.String(),
		recipient.String(),
		flagWithPrefix(cli.FlagKeyName), keyName,
		flagWithPrefix(cli.FlagDeliverAmount), deliverAmount.String(),
	}, homeArgs...)
	args = append(args, testKeyringFlags(keyringDir)...)

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().SendFromCoreumToXRPL(
		gomock.Any(),
		gomock.Any(),
		recipient,
		amount,
		mock.MatchedBy(func(v *sdkmath.Int) bool {
			return v.String() == deliverAmount.String()
		}),
	)
	executeCmd(t, cli.SendFromCoreumToXRPLCmd(mockBridgeClientProvider(bridgeClientMock)), args...)

	// without the deliver amount
	args = append([]string{
		amount.String(),
		recipient.String(),
		flagWithPrefix(cli.FlagKeyName), keyName,
	}, homeArgs...)
	args = append(args, testKeyringFlags(keyringDir)...)

	bridgeClientMock = NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().SendFromCoreumToXRPL(
		gomock.Any(),
		gomock.Any(),
		recipient,
		amount,
		nil,
	)
	executeCmd(t, cli.SendFromCoreumToXRPLCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestCoreumBalancesCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	account := coreum.GenAccount()
	bridgeClientMock.EXPECT().GetCoreumBalances(gomock.Any(), account).Return(sdk.NewCoins(), nil)
	executeCmd(t, cli.CoreumBalancesCmd(mockBridgeClientProvider(bridgeClientMock)),
		append(initConfig(t), account.String())...)
}

func TestXRPBalancesCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	account := xrpl.GenPrivKeyTxSigner().Account()
	bridgeClientMock.EXPECT().GetXRPLBalances(gomock.Any(), account).Return([]rippledata.Amount{}, nil)
	executeCmd(t, cli.XRPLBalancesCmd(mockBridgeClientProvider(bridgeClientMock)),
		append(initConfig(t), account.String())...)
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
	args := append(initConfig(t),
		amount.Value.String(),
		amount.Issuer.String(),
		amount.Currency.String(),
		flagWithPrefix(cli.FlagKeyName), keyName,
	)
	args = append(args, testKeyringFlags(keyringDir)...)

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().SetXRPLTrustSet(
		gomock.Any(),
		gomock.Any(),
		amount,
	)
	executeCmd(t, cli.SetXRPLTrustSetCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestClaimPendingRefundCmd_WithRefundID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "claimer"
	address := addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	bridgeClientMock := NewMockBridgeClient(ctrl)
	refundID := "sample-1"
	bridgeClientMock.EXPECT().ClaimRefund(
		gomock.Any(),
		address,
		refundID,
	).Return(nil)
	args := append(initConfig(t), flagWithPrefix(cli.FlagKeyName), keyName, flagWithPrefix(cli.FlagRefundID), refundID)
	args = append(args, testKeyringFlags(keyringDir)...)
	fmt.Println(args)
	executeCmd(t, cli.ClaimRefundCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestClaimPendingRefundCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "claimer"
	address := addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	bridgeClientMock := NewMockBridgeClient(ctrl)
	refundID := "sample-1"
	pendingRefunds := []coreum.PendingRefund{{ID: refundID, Coin: sdk.NewCoin("coin1", sdk.NewInt(10))}}
	bridgeClientMock.EXPECT().GetPendingRefunds(
		gomock.Any(),
		address,
	).Return(pendingRefunds, nil)
	bridgeClientMock.EXPECT().ClaimRefund(
		gomock.Any(),
		address,
		refundID,
	).Return(nil)
	args := append(initConfig(t), flagWithPrefix(cli.FlagKeyName), keyName)
	args = append(args, testKeyringFlags(keyringDir)...)
	executeCmd(t, cli.ClaimRefundCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestGetPendingRefundsCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	account := coreum.GenAccount()
	bridgeClientMock.EXPECT().GetPendingRefunds(gomock.Any(), account).Return([]coreum.PendingRefund{}, nil)
	executeCmd(t, cli.GetPendingRefundsCmd(mockBridgeClientProvider(bridgeClientMock)),
		append(initConfig(t), account.String())...)
}

func TestClaimRelayerFees_WithSpecificAmount(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "relayer"
	address := addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	bridgeClientMock := NewMockBridgeClient(ctrl)
	amount := sdk.NewCoins(sdk.NewCoin("ucore", sdk.NewInt(100)))
	bridgeClientMock.EXPECT().ClaimRelayerFees(
		gomock.Any(),
		address,
		amount,
	).Return(nil)
	args := append(initConfig(t),
		flagWithPrefix(cli.FlagKeyName), keyName,
		flagWithPrefix(cli.FlagAmount), amount.String(),
	)
	args = append(args, testKeyringFlags(keyringDir)...)
	executeCmd(t, cli.ClaimRelayerFeesCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestClaimRelayerFees(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "relayer"
	address := addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	bridgeClientMock := NewMockBridgeClient(ctrl)
	fees, err := sdk.ParseCoinsNormalized("100mycoin,100ucore")
	require.NoError(t, err)
	bridgeClientMock.EXPECT().GetFeesCollected(
		gomock.Any(),
		address,
	).Return(fees, nil)
	bridgeClientMock.EXPECT().ClaimRelayerFees(
		gomock.Any(),
		address,
		fees,
	).Return(nil)
	args := append(initConfig(t), flagWithPrefix(cli.FlagKeyName), keyName)
	args = append(args, testKeyringFlags(keyringDir)...)
	executeCmd(t, cli.ClaimRelayerFeesCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestGetRelayerFees(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	account := coreum.GenAccount()
	fees, err := sdk.ParseCoinsNormalized("100ucore,100mycoin")
	require.NoError(t, err)
	bridgeClientMock.EXPECT().GetFeesCollected(gomock.Any(), account).Return(fees, nil)
	executeCmd(t, cli.GetRelayerFeesCmd(mockBridgeClientProvider(bridgeClientMock)),
		append(initConfig(t), account.String())...)
}

func TestHaltBridgeCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	keyringDir := t.TempDir()
	keyName := "owner"
	owner := addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	args := append(initConfig(t), flagWithPrefix(cli.FlagKeyName), keyName)
	args = append(args, testKeyringFlags(keyringDir)...)
	bridgeClientMock.EXPECT().HaltBridge(gomock.Any(), owner).Return(nil)
	executeCmd(t, cli.HaltBridgeCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestResumeBridgeCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	keyringDir := t.TempDir()
	keyName := "owner"
	owner := addKeyToTestKeyring(t, keyringDir, keyName, coreum.KeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	args := append(initConfig(t), flagWithPrefix(cli.FlagKeyName), keyName)
	args = append(args, testKeyringFlags(keyringDir)...)
	bridgeClientMock.EXPECT().ResumeBridge(gomock.Any(), owner).Return(nil)
	executeCmd(t, cli.ResumeBridgeCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func executeCmd(t *testing.T, cmd *cobra.Command, args ...string) string {
	return executeCmdWithOutputOption(t, cmd, "text", args...)
}

func executeCmdWithOutputOption(t *testing.T, cmd *cobra.Command, outOpt string, args ...string) string {
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
		WithOutputFormat(outOpt)
	ctx := context.WithValue(context.Background(), client.ClientContextKey, &clientCtx)

	if err := cmd.ExecuteContext(ctx); err != nil {
		require.NoError(t, err)
	}

	t.Logf("Command %s is executed successfully", cmd.Name())

	return buf.String()
}

func addKeyToTestKeyring(t *testing.T, keyringDir, keyName, suffix, hdPath string) sdk.AccAddress {
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

	keyInfo, _, err := kr.NewMnemonic(
		keyName,
		keyring.English,
		hdPath,
		"",
		hd.Secp256k1,
	)
	require.NoError(t, err)

	addr, err := keyInfo.GetAddress()
	require.NoError(t, err)

	return addr
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
	return func(_ runner.Components) (cli.BridgeClient, error) {
		return bridgeClientMock, nil
	}
}

func initConfig(t *testing.T) []string {
	configPath := path.Join(t.TempDir(), "config-path")
	configFilePath := path.Join(configPath, runner.ConfigFileName)
	require.NoFileExists(t, configFilePath)

	args := []string{
		flagWithPrefix(cli.FlagHome), configPath,
	}
	executeCmd(t, cli.InitCmd(), args...)
	require.FileExists(t, configFilePath)

	return args
}
