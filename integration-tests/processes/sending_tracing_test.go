//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestTraceXRPLToCoreumTransfer(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumRecipient := chains.Coreum.GenAccount()
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient: %s", xrplRecipientAddress.String())

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)
	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)
	registeredXRPLCurrency := integrationtests.GenerateXRPLCurrency(t)
	registeredXRPLToken := runnerEnv.RegisterXRPLOriginatedToken(
		ctx,
		t,
		xrplIssuerAddress,
		registeredXRPLCurrency,
		int32(6),
		integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30),
		sdk.ZeroInt(),
	)

	valueSentToCoreum, err := rippledata.NewValue("1.0", false)
	require.NoError(t, err)
	amountToSendToCoreum := rippledata.Amount{
		Value:    valueSentToCoreum,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}

	txHash, err := runnerEnv.BridgeClient.SendFromXRPLToCoreum(
		ctx, xrplIssuerAddress.String(), amountToSendToCoreum, coreumRecipient,
	)
	require.NoError(t, err)

	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumRecipient,
		sdk.NewCoin(
			registeredXRPLToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				valueSentToCoreum.String(),
				xrpl.XRPLIssuedTokenDecimals,
			),
		),
	)

	tracingInfo, err := runnerEnv.BridgeClient.GetXRPLToCoreumTracingInfo(ctx, txHash)
	require.NoError(t, err)
	require.NotNil(t, tracingInfo.CoreumTx)
	require.Len(t, tracingInfo.EvidenceToTxs, 2)
}

func TestTraceCoreumToXRPLTransfer(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	// start relayers
	runnerEnv.StartAllRunnerProcesses()
	// recover tickets so we can register tokens
	runnerEnv.AllocateTickets(ctx, t, 200)

	// issue asset ft and register it
	sendingPrecision := int32(2)
	tokenDecimals := uint32(4)
	initialAmount := sdkmath.NewIntWithDecimal(1, 16)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 16)
	registeredCoreumOriginatedToken := runnerEnv.IssueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		coreumSenderAddress,
		tokenDecimals,
		initialAmount,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)

	// send TrustSet to be able to receive coins from the bridge
	xrplCurrency, err := rippledata.NewCurrency(registeredCoreumOriginatedToken.XRPLCurrency)
	require.NoError(t, err)
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency)

	// equal to 11.1111 on XRPL
	amountToSendToXRPL := sdkmath.NewInt(111111)
	txHash, err := runnerEnv.BridgeClient.SendFromCoreumToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
		nil,
	)
	require.NoError(t, err)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	// check the XRPL recipient balance
	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency,
	)
	require.Equal(t, "11.11", xrplRecipientBalance.Value.String())

	tracingInfo, err := runnerEnv.BridgeClient.GetCoreumToXRPLTracingInfo(ctx, txHash)
	require.NoError(t, err)
	require.NotNil(t, tracingInfo.XRPLTx)
	require.Len(t, tracingInfo.EvidenceToTxs, 2)
}
