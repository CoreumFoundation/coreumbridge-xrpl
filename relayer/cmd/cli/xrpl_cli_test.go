package cli_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/cmd/cli"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestSetXRPLTrustSetCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	keyringDir := t.TempDir()
	keyName := "sender"
	addKeyToTestKeyring(t, keyringDir, keyName, cli.XRPLKeyringSuffix, xrpl.XRPLHDPath)

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
	executeTxCmd(t, cli.SetXRPLTrustSetCmd(mockBridgeClientProvider(bridgeClientMock)), args...)
}

func TestXRPBalancesCmd(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	account := xrpl.GenPrivKeyTxSigner().Account()
	bridgeClientMock.EXPECT().GetXRPLBalances(gomock.Any(), account).Return([]rippledata.Amount{}, nil)
	executeQueryCmd(t, cli.XRPLBalancesCmd(mockBridgeClientProvider(bridgeClientMock)),
		append(initConfig(t), account.String())...)
}

func TestTraceXRPLToCoreumTransfer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bridgeClientMock := NewMockBridgeClient(ctrl)

	xrplTxHash := "hash"
	args := append(initConfig(t), xrplTxHash)

	// complete
	bridgeClientMock.EXPECT().GetXRPLToCoreumTracingInfo(gomock.Any(), xrplTxHash).
		Return(bridgeclient.XRPLToCoreumTracingInfo{
			CoreumTx: &sdk.TxResponse{},
		}, nil)
	executeQueryCmd(t, cli.TraceXRPLToCoreumTransfer(mockBridgeClientProvider(bridgeClientMock)), args...)

	// pending
	bridgeClientMock.EXPECT().GetContractConfig(gomock.Any()).Return(coreum.ContractConfig{}, nil)
	bridgeClientMock.EXPECT().GetXRPLToCoreumTracingInfo(gomock.Any(), xrplTxHash).
		Return(bridgeclient.XRPLToCoreumTracingInfo{
			CoreumTx: nil,
		}, nil)

	executeQueryCmd(t, cli.TraceXRPLToCoreumTransfer(mockBridgeClientProvider(bridgeClientMock)), args...)
}
