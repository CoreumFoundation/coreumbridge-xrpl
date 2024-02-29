package cli_test

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/golang/mock/gomock"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/cmd/cli"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestCoreumTxFlags(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner" //nolint:goconst // testing only variable
	owner := addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())
	expectGenerateOnlyInCtx := func(generateOnly bool) any {
		return mock.MatchedBy(func(ctx context.Context) bool {
			v := ctx.Value(client.ClientContextKey)
			require.NotNil(t, v)
			clientCtx, ok := v.(*client.Context)
			require.True(t, ok)
			return clientCtx.GenerateOnly == generateOnly
		})
	}
	tests := []struct {
		name string
		args []string
		mock func(m *MockBridgeClient)
	}{
		{
			name: "key_name",
			args: []string{
				flagWithPrefix(cli.FlagKeyName), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().RecoverTickets(
					expectGenerateOnlyInCtx(false),
					owner,
					nil,
				)
			},
		},
		{
			name: "from_owner",
			args: []string{
				flagWithPrefix(cli.FlagFromOwner), keyName,
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().GetContractOwnership(
					gomock.Any(),
				).Return(coreum.ContractOwnership{
					Owner: owner,
				}, nil)
				m.EXPECT().RecoverTickets(
					expectGenerateOnlyInCtx(false),
					owner,
					nil,
				)
			},
		},
		{
			name: "from_owner_generate_only",
			args: []string{
				flagWithPrefix(cli.FlagFromOwner), keyName,
				flagWithPrefix(flags.FlagGenerateOnly),
			},
			mock: func(m *MockBridgeClient) {
				m.EXPECT().GetContractOwnership(
					gomock.Any(),
				).Return(coreum.ContractOwnership{
					Owner: owner,
				}, nil)
				m.EXPECT().RecoverTickets(
					expectGenerateOnlyInCtx(true),
					owner,
					nil,
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
			executeCoreumTxCmd(
				t,
				mockBridgeClientProvider(bridgeClientMock),
				cli.RecoverTicketsCmd(mockBridgeClientProvider(bridgeClientMock)),
				tt.args...,
			)
		})
	}
}

func TestRecoverTicketsCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, xrpl.XRPLHDPath)

	homeArgs := initConfig(t)

	args := append([]string{
		flagWithPrefix(cli.FlagKeyName), keyName,
	}, homeArgs...)
	args = append(args, testKeyringFlags(keyringDir)...)
	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().RecoverTickets(gomock.Any(), gomock.Any(), nil)
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.RecoverTicketsCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)

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
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.RecoverTicketsCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)
}

func TestRegisterCoreumTokenCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

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
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.RegisterCoreumTokenCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)
}

func TestUpdateCoreumTokenCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())
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
			executeCoreumTxCmd(
				t,
				mockBridgeClientProvider(bridgeClientMock),
				cli.UpdateCoreumTokenCmd(mockBridgeClientProvider(bridgeClientMock)),
				append(initConfig(t), tt.args...)...)
		})
	}
}

func TestRegisterXRPLTokenCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

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
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.RegisterXRPLTokenCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)
}

func TestRecoverXRPLTokenRegistrationCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner"
	owner := addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

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
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.RecoverXRPLTokenRegistrationCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)
}

func TestUpdateXRPLTokenCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())
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
			executeCoreumTxCmd(
				t,
				mockBridgeClientProvider(bridgeClientMock),
				cli.UpdateXRPLTokenCmd(mockBridgeClientProvider(bridgeClientMock)),
				append(initConfig(t), tt.args...)...,
			)
		})
	}
}

func TestRotateKeysCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	configPath := path.Join(t.TempDir(), "new-keys.yaml")

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	homeArgs := initConfig(t)

	// call rotate-keys with init only
	args := append([]string{
		configPath,
		flagWithPrefix(cli.FlagInitOnly),
		flagWithPrefix(cli.FlagKeyName), keyName,
	}, homeArgs...)
	args = append(args, testKeyringFlags(keyringDir)...)
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(nil),
		cli.RotateKeysCmd(mockBridgeClientProvider(nil)),
		args...,
	)

	// use generated file
	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().RotateKeys(gomock.Any(), gomock.Any(), bridgeclient.DefaultKeysRotationConfig())
	args = append([]string{
		configPath,
		flagWithPrefix(cli.FlagKeyName), keyName,
	}, homeArgs...)
	args = append(args, testKeyringFlags(keyringDir)...)
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.RotateKeysCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)
}

func TestUpdateXRPLBaseFeeCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "owner"
	addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	// call rotate-keys with init only
	args := append(initConfig(t),
		"17",
		flagWithPrefix(cli.FlagKeyName), keyName,
	)
	args = append(args, testKeyringFlags(keyringDir)...)
	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().UpdateXRPLBaseFee(gomock.Any(), gomock.Any(), uint32(17))
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.UpdateXRPLBaseFeeCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)
}

func TestSendFromCoreumToXRPLCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "sender"
	addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

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
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.SendFromCoreumToXRPLCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)

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
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.SendFromCoreumToXRPLCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)
}

func TestClaimPendingRefundCmd_WithRefundID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "claimer"
	address := addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

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
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.ClaimRefundCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)
}

func TestClaimPendingRefundCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "claimer"
	address := addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

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
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.ClaimRefundCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)
}

func TestClaimRelayerFees_WithSpecificAmount(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "relayer"
	address := addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

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
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.ClaimRelayerFeesCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)
}

func TestClaimRelayerFees(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "relayer"
	address := addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

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
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.ClaimRelayerFeesCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)
}

func TestHaltBridgeCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	keyringDir := t.TempDir()
	keyName := "owner"
	owner := addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	args := append(initConfig(t), flagWithPrefix(cli.FlagKeyName), keyName)
	args = append(args, testKeyringFlags(keyringDir)...)
	bridgeClientMock.EXPECT().HaltBridge(gomock.Any(), owner).Return(nil)
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.HaltBridgeCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)
}

func TestResumeBridgeCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	keyringDir := t.TempDir()
	keyName := "owner"
	owner := addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	args := append(initConfig(t), flagWithPrefix(cli.FlagKeyName), keyName)
	args = append(args, testKeyringFlags(keyringDir)...)
	bridgeClientMock.EXPECT().ResumeBridge(gomock.Any(), owner).Return(nil)
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.ResumeBridgeCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)
}

func TestCancelPendingOperationCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	keyringDir := t.TempDir()
	keyName := "owner"
	owner := addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	operationID := uint32(1)
	args := append([]string{
		strconv.Itoa(int(operationID)),
		flagWithPrefix(cli.FlagKeyName), keyName,
	}, initConfig(t)...)
	args = append(args, testKeyringFlags(keyringDir)...)
	bridgeClientMock.EXPECT().CancelPendingOperation(gomock.Any(), owner, operationID).Return(nil)
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.CancelPendingOperationCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)
}

func TestUpdateProhibitedXRPLRecipientsCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	keyringDir := t.TempDir()
	keyName := "owner"
	owner := addKeyToTestKeyring(t, keyringDir, keyName, cli.CoreumKeyringSuffix, sdk.GetConfig().GetFullBIP44Path())

	prohibitedXRPLRecipient1 := xrpl.GenPrivKeyTxSigner().Account().String()
	prohibitedXRPLRecipient2 := xrpl.GenPrivKeyTxSigner().Account().String()
	args := []string{
		flagWithPrefix(cli.FlagProhibitedXRPLRecipient), prohibitedXRPLRecipient1,
		flagWithPrefix(cli.FlagProhibitedXRPLRecipient), prohibitedXRPLRecipient2,
		flagWithPrefix(cli.FlagKeyName), keyName,
	}
	args = append(args, testKeyringFlags(keyringDir)...)
	args = append(args, initConfig(t)...)
	bridgeClientMock.EXPECT().UpdateProhibitedXRPLRecipients(gomock.Any(), owner, []string{
		prohibitedXRPLRecipient1,
		prohibitedXRPLRecipient2,
	}).Return(nil)
	executeCoreumTxCmd(
		t,
		mockBridgeClientProvider(bridgeClientMock),
		cli.UpdateProhibitedXRPLRecipientsCmd(mockBridgeClientProvider(bridgeClientMock)),
		args...,
	)
}

func TestPendingOperationsCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().GetPendingOperations(gomock.Any()).Return([]coreum.Operation{}, nil)
	executeQueryCmd(t, cli.PendingOperationsCmd(mockBridgeClientProvider(bridgeClientMock)), initConfig(t)...)
}

func TestRelayerFeesCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	account := coreum.GenAccount()
	fees, err := sdk.ParseCoinsNormalized("100ucore,100mycoin")
	require.NoError(t, err)
	bridgeClientMock.EXPECT().GetFeesCollected(gomock.Any(), account).Return(fees, nil)
	executeQueryCmd(t, cli.RelayerFeesCmd(mockBridgeClientProvider(bridgeClientMock)),
		append(initConfig(t), account.String())...)
}

func TestPendingRefundsCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	account := coreum.GenAccount()
	bridgeClientMock.EXPECT().GetPendingRefunds(gomock.Any(), account).Return([]coreum.PendingRefund{}, nil)
	executeQueryCmd(t, cli.PendingRefundsCmd(mockBridgeClientProvider(bridgeClientMock)),
		append(initConfig(t), account.String())...)
}

func TestCoreumBalancesCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	account := coreum.GenAccount()
	bridgeClientMock.EXPECT().GetCoreumBalances(gomock.Any(), account).Return(sdk.NewCoins(), nil)
	executeQueryCmd(t, cli.CoreumBalancesCmd(mockBridgeClientProvider(bridgeClientMock)),
		append(initConfig(t), account.String())...)
}

func TestRegisteredTokensCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().GetAllTokens(gomock.Any()).Return([]coreum.CoreumToken{}, []coreum.XRPLToken{}, nil)
	executeQueryCmd(t, cli.RegisteredTokensCmd(mockBridgeClientProvider(bridgeClientMock)), initConfig(t)...)
}

func TestContractConfigCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().GetContractConfig(gomock.Any()).Return(coreum.ContractConfig{}, nil)
	executeQueryCmd(t, cli.ContractConfigCmd(mockBridgeClientProvider(bridgeClientMock)), initConfig(t)...)
}

func TestContractOwnershipCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)
	bridgeClientMock.EXPECT().GetContractOwnership(gomock.Any()).Return(coreum.ContractOwnership{}, nil)
	executeQueryCmd(t, cli.ContractOwnershipCmd(mockBridgeClientProvider(bridgeClientMock)), initConfig(t)...)
}

func TestProhibitedXRPLRecipientsCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	bridgeClientMock.EXPECT().GetProhibitedXRPLRecipients(gomock.Any()).Return([]string{}, nil)
	executeQueryCmd(t, cli.ProhibitedXRPLRecipientsCmd(mockBridgeClientProvider(bridgeClientMock)), initConfig(t)...)
}

func executeCoreumTxCmd(t *testing.T, bcp cli.BridgeClientProvider, cmd *cobra.Command, args ...string) {
	cli.AddCoreumTxFlags(cmd)
	cmd.PreRunE = cli.CoreumTxPreRun(bcp)
	executeCmd(t, cmd, args...)
}
