//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum/v4/pkg/client"
	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestSendXRPLOriginatedTokensFromXRPLToCoreumAndBack(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumSender := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})
	t.Logf("Coreum sender: %s", coreumSender.String())
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient: %s", xrplRecipientAddress.String())

	bridgingFee := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 24)
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
		bridgingFee,
	)

	// we will send 1.5e10 and 0.1 will be deducted as fee when sending from xrpl
	// and 0.4e10 will be deducted as fees when sending back from coreum to xrpl.
	valueSentToCoreum, err := rippledata.NewValue("1.5e10", false)
	require.NoError(t, err)
	valueToReceiveOnXRPL, err := rippledata.NewValue("1.4e10", false)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueSentToCoreum,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}

	runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplIssuerAddress.String(), amountToSendFromXRPLtoCoreum, coreumSender)
	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumSender,
		sdk.NewCoin(
			registeredXRPLToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				valueToReceiveOnXRPL.String(),
				xrpl.XRPLIssuedTokenDecimals,
			),
		),
	)

	// send back the full amount in 4 transactions to XRPL
	amountToSend := integrationtests.ConvertStringWithDecimalsToSDKInt(
		t, valueToReceiveOnXRPL.String(), xrpl.XRPLIssuedTokenDecimals,
	).QuoRaw(4)

	// send 2 transactions without the trust set to be reverted
	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSender,
		xrplRecipientAddress,
		sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSend),
		nil,
	)
	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSender,
		xrplRecipientAddress,
		sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSend),
		nil,
	)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	// send TrustSet to be able to receive coins
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency)

	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSender,
		xrplRecipientAddress,
		sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSend),
		nil,
	)
	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSender,
		xrplRecipientAddress,
		sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSend),
		nil,
	)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency,
	)
	require.Equal(t, "5000000000", xrplRecipientBalance.Value.String())

	// assert bridging fee is deducted.
	for _, relayer := range runnerEnv.BootstrappingConfig.Relayers {
		relayerAddress, err := sdk.AccAddressFromBech32(relayer.CoreumAddress)
		require.NoError(t, err)
		fees, err := runnerEnv.BridgeClient.GetFeesCollected(ctx, relayerAddress)
		require.NoError(t, err)
		require.Len(t, fees, 1)
		expectedFees := bridgingFee.MulRaw(5).QuoRaw(int64(envCfg.RelayersCount))
		require.EqualValues(t, expectedFees.String(), fees.AmountOf(registeredXRPLToken.CoreumDenom).String())
	}
}

func TestSendXRPLOriginatedTokenWithTransferRateAndDeliverAmountFromXRPLToCoreumAndBack(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumSender := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})
	t.Logf("Coreum sender: %s", coreumSender.String())
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient: %s", xrplRecipientAddress.String())

	transferRate := lo.ToPtr(sdkmath.NewInt(1030000000)) // 3%
	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)

	setTransferRateTx := rippledata.AccountSet{
		TxBase: rippledata.TxBase{
			Account:         xrplIssuerAddress,
			TransactionType: rippledata.ACCOUNT_SET,
		},
		TransferRate: lo.ToPtr(uint32(transferRate.Uint64())),
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &setTransferRateTx, xrplIssuerAddress))

	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)

	sendingPrecision := int32(2)
	bridgingFee := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.1", xrpl.XRPLIssuedTokenDecimals)

	registeredXRPLCurrency := integrationtests.GenerateXRPLCurrency(t)
	registeredXRPLToken := runnerEnv.RegisterXRPLOriginatedToken(
		ctx,
		t,
		xrplIssuerAddress,
		registeredXRPLCurrency,
		sendingPrecision,
		integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30),
		bridgingFee,
	)

	valueSentToCoreum, err := rippledata.NewValue("10.1", false)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueSentToCoreum,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}

	runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplIssuerAddress.String(), amountToSendFromXRPLtoCoreum, coreumSender)
	require.NoError(t, err)
	receivedAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(
		t,
		// expect the amount minus bridging fee
		"10",
		xrpl.XRPLIssuedTokenDecimals,
	)
	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumSender,
		sdk.NewCoin(
			registeredXRPLToken.CoreumDenom,
			receivedAmount,
		),
	)

	// send TrustSet to be able to receive coins
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency)

	invalidAmountToSendBackToXRPL := integrationtests.ConvertStringWithDecimalsToSDKInt(
		t,
		"1.129999", // 1 + 2.9999% rate (but rate is 3%) + bridging fee
		xrpl.XRPLIssuedTokenDecimals,
	)
	deliverAmountXRPLValue, err := rippledata.NewValue("1", false)
	require.NoError(t, err)
	deliverAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(
		t,
		deliverAmountXRPLValue.String(),
		xrpl.XRPLIssuedTokenDecimals,
	)

	// send the tx and await for it to fail because of the invalid amount
	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSender,
		xrplRecipientAddress,
		sdk.NewCoin(registeredXRPLToken.CoreumDenom, invalidAmountToSendBackToXRPL),
		&deliverAmount,
	)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency,
	)
	require.Equal(t, "0", xrplRecipientBalance.Value.String())

	// claim refund to send one more time
	pendingRefunds, err := runnerEnv.ContractClient.GetPendingRefunds(ctx, coreumSender)
	require.NoError(t, err)
	require.Len(t, pendingRefunds, 1)
	err = runnerEnv.BridgeClient.ClaimRefund(ctx, coreumSender, pendingRefunds[0].ID)
	require.NoError(t, err)

	// send one more time but with the min allowed amount to pass
	amountToSendBackToXRPL := integrationtests.ConvertStringWithDecimalsToSDKInt(
		t,
		"1.13", // 1 + 3% rate + bridging fee
		xrpl.XRPLIssuedTokenDecimals,
	)

	coreumSenderBalanceBeforeRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSender.String(),
		Denom:   registeredXRPLToken.CoreumDenom,
	})
	require.NoError(t, err)

	xrplBridgeAccountBalanceBefore := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, runnerEnv.BridgeXRPLAddress, xrplIssuerAddress, registeredXRPLCurrency,
	)

	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSender,
		xrplRecipientAddress,
		sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSendBackToXRPL),
		&deliverAmount,
	)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	xrplRecipientBalance = runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency,
	)
	require.Equal(t, deliverAmountXRPLValue.String(), xrplRecipientBalance.Value.String())

	coreumSenderBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSender.String(),
		Denom:   registeredXRPLToken.CoreumDenom,
	})
	require.NoError(t, err)

	require.Equal(
		t,
		coreumSenderBalanceBeforeRes.Balance.Amount.Sub(amountToSendBackToXRPL).String(),
		coreumSenderBalanceAfterRes.Balance.Amount.String(),
	)

	xrplBridgeAccountBalanceAfter := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, runnerEnv.BridgeXRPLAddress, xrplIssuerAddress, registeredXRPLCurrency,
	)

	expectedSentAmount, err := rippledata.NewValue("1.03", false)
	require.NoError(t, err)
	expectedXRPLBridgeAccountBalanceValue, err := xrplBridgeAccountBalanceBefore.Value.Subtract(*expectedSentAmount)
	require.NoError(t, err)

	require.Equal(
		t,
		expectedXRPLBridgeAccountBalanceValue.String(),
		xrplBridgeAccountBalanceAfter.Value.String(),
	)

	// use the amount which is greater that required to covers the transfer rate

	// send one more time but with the min allowed amount to pass
	amountToSendBackToXRPL = integrationtests.ConvertStringWithDecimalsToSDKInt(
		t,
		"2.01", // 1 + 3% rate + bridging fee + reminder which will be locked on the bridge XRPL account
		xrpl.XRPLIssuedTokenDecimals,
	)

	coreumSenderBalanceBeforeRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSender.String(),
		Denom:   registeredXRPLToken.CoreumDenom,
	})
	require.NoError(t, err)

	xrplBridgeAccountBalanceBefore = runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, runnerEnv.BridgeXRPLAddress, xrplIssuerAddress, registeredXRPLCurrency,
	)

	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSender,
		xrplRecipientAddress,
		sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSendBackToXRPL),
		&deliverAmount,
	)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	xrplRecipientBalance = runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency,
	)

	expectedXRPLRecipientBalance, err := deliverAmountXRPLValue.Add(*deliverAmountXRPLValue)
	require.NoError(t, err)
	require.Equal(t, expectedXRPLRecipientBalance.String(), xrplRecipientBalance.Value.String())

	coreumSenderBalanceAfterRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSender.String(),
		Denom:   registeredXRPLToken.CoreumDenom,
	})
	require.NoError(t, err)

	require.Equal(
		t,
		coreumSenderBalanceBeforeRes.Balance.Amount.Sub(amountToSendBackToXRPL).String(),
		coreumSenderBalanceAfterRes.Balance.Amount.String(),
	)

	xrplBridgeAccountBalanceAfter = runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, runnerEnv.BridgeXRPLAddress, xrplIssuerAddress, registeredXRPLCurrency,
	)

	expectedSentAmount, err = rippledata.NewValue("1.03", false)
	require.NoError(t, err)
	expectedXRPLBridgeAccountBalanceValue, err = xrplBridgeAccountBalanceBefore.Value.Subtract(*expectedSentAmount)
	require.NoError(t, err)

	require.Equal(
		t,
		expectedXRPLBridgeAccountBalanceValue.String(),
		xrplBridgeAccountBalanceAfter.Value.String(),
	)
}

func TestSendXRPLOriginatedTokenWithoutTransferRateButWithDeliverAmountFromXRPLToCoreumAndBack(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumSender := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})
	t.Logf("Coreum sender: %s", coreumSender.String())
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient: %s", xrplRecipientAddress.String())

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)
	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)

	sendingPrecision := int32(2)
	registeredXRPLCurrency := integrationtests.GenerateXRPLCurrency(t)
	registeredXRPLToken := runnerEnv.RegisterXRPLOriginatedToken(
		ctx,
		t,
		xrplIssuerAddress,
		registeredXRPLCurrency,
		sendingPrecision,
		integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30),
		sdkmath.ZeroInt(),
	)

	valueSentToCoreum, err := rippledata.NewValue("10", false)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueSentToCoreum,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}

	runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplIssuerAddress.String(), amountToSendFromXRPLtoCoreum, coreumSender)
	require.NoError(t, err)
	receivedAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(
		t,
		// expect the amount minus bridging fee
		valueSentToCoreum.String(),
		xrpl.XRPLIssuedTokenDecimals,
	)
	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumSender,
		sdk.NewCoin(
			registeredXRPLToken.CoreumDenom,
			receivedAmount,
		),
	)

	// send TrustSet to be able to receive coins
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency)

	amountToSendBackToXRPL := integrationtests.ConvertStringWithDecimalsToSDKInt(
		t,
		"2",
		xrpl.XRPLIssuedTokenDecimals,
	)
	deliverAmountXRPLValue, err := rippledata.NewValue("1", false)
	require.NoError(t, err)
	deliverAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(
		t,
		deliverAmountXRPLValue.String(),
		xrpl.XRPLIssuedTokenDecimals,
	)

	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSender,
		xrplRecipientAddress,
		sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSendBackToXRPL),
		&deliverAmount,
	)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency,
	)
	require.Equal(t, deliverAmountXRPLValue.String(), xrplRecipientBalance.Value.String())
}

func TestSendXRPTokenFromXRPLToCoreumAndBack(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumSender := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})
	t.Logf("Coreum sender: %s", coreumSender.String())
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient: %s", xrplRecipientAddress.String())
	// XRP to send the part of it and cover fees
	xrplSenderAddress := chains.XRPL.GenAccount(ctx, t, 2.2)
	t.Logf("XRPL sender: %s", xrplSenderAddress.String())

	registeredXRPToken, err := runnerEnv.ContractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrpl.XRPTokenIssuer.String(), xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
	)
	require.NoError(t, err)

	valueToSendFromXRPLtoCoreum, err := rippledata.NewValue("2.111111", true)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueToSendFromXRPLtoCoreum,
		Currency: xrpl.XRPTokenCurrency,
		Issuer:   xrpl.XRPTokenIssuer,
	}

	runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplSenderAddress.String(), amountToSendFromXRPLtoCoreum, coreumSender)
	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumSender,
		sdk.NewCoin(
			registeredXRPToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				valueToSendFromXRPLtoCoreum.String(),
				xrpl.XRPCurrencyDecimals,
			)),
	)

	xrplRecipientBalanceBefore := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, xrpl.XRPTokenIssuer, xrpl.XRPTokenCurrency,
	)

	for _, v := range []string{"1.1", "0.5", "0.51111", "0.000001"} {
		runnerEnv.SendFromCoreumToXRPL(
			ctx,
			t,
			coreumSender,
			xrplRecipientAddress,
			sdk.NewCoin(
				registeredXRPToken.CoreumDenom,
				integrationtests.ConvertStringWithDecimalsToSDKInt(
					t,
					v,
					xrpl.XRPCurrencyDecimals,
				)),
			nil,
		)
	}

	runnerEnv.AwaitNoPendingOperations(ctx, t)

	xrplRecipientBalanceAfter := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, xrpl.XRPTokenIssuer, xrpl.XRPTokenCurrency,
	)
	received, err := xrplRecipientBalanceAfter.Value.Subtract(*xrplRecipientBalanceBefore.Value)
	require.NoError(t, err)
	require.Equal(t, valueToSendFromXRPLtoCoreum.String(), received.String())
}

func TestSendXRPLOriginatedTokenFromXRPLToCoreumWithMaliciousRelayer(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.MaliciousRelayerNumber = 1
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumSender := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})
	t.Logf("Coreum sender: %s", coreumSender.String())
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
		sdkmath.ZeroInt(),
	)

	valueToSendFromXRPLtoCoreum, err := rippledata.NewValue("1e10", false)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueToSendFromXRPLtoCoreum,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}

	runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplIssuerAddress.String(), amountToSendFromXRPLtoCoreum, coreumSender)
	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumSender,
		sdk.NewCoin(
			registeredXRPLToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				valueToSendFromXRPLtoCoreum.String(),
				xrpl.XRPLIssuedTokenDecimals,
			),
		),
	)

	// send TrustSet to be able to receive coins
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency)

	amountToSend := integrationtests.ConvertStringWithDecimalsToSDKInt(
		t, valueToSendFromXRPLtoCoreum.String(), xrpl.XRPLIssuedTokenDecimals,
	).QuoRaw(4)
	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSender,
		xrplRecipientAddress,
		sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSend),
		nil,
	)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency,
	)
	require.Equal(t, "2500000000", xrplRecipientBalance.Value.String())
}

func TestSendXRPLOriginatedTokensFromXRPLToCoreumWithDifferentAmountAndPartialAmount(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 100)
	t.Logf("XRPL currency issuer address: %s", xrplIssuerAddress)

	coreumRecipient := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipient.String())

	sendingPrecision := int32(6)
	maxHoldingAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	registeredXRPLCurrency, err := rippledata.NewCurrency("RCP")
	require.NoError(t, err)

	// register token with 20 chars
	currencyHexSymbol := hex.EncodeToString([]byte(strings.Repeat("R", 20)))
	registeredXRPLHexCurrency, err := rippledata.NewCurrency(currencyHexSymbol)
	require.NoError(t, err)

	// fund owner to cover asset FT issuance fees
	chains.Coreum.FundAccountWithOptions(ctx, t, runnerEnv.ContractOwner, coreumintegration.BalancesOptions{
		Amount: chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount.MulRaw(2),
	})

	// start relayers
	runnerEnv.StartAllRunnerProcesses()
	// recover tickets so we can register tokens
	runnerEnv.AllocateTickets(ctx, t, 200)

	// register XRPL originated token with 3 chars
	_, err = runnerEnv.ContractClient.RegisterXRPLToken(
		ctx,
		runnerEnv.ContractOwner,
		xrplIssuerAddress.String(),
		xrpl.ConvertCurrencyToString(registeredXRPLCurrency),
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)
	require.NoError(t, err)

	// register XRPL originated token with 20 chars
	_, err = runnerEnv.ContractClient.RegisterXRPLToken(
		ctx,
		runnerEnv.ContractOwner,
		xrplIssuerAddress.String(),
		xrpl.ConvertCurrencyToString(registeredXRPLHexCurrency),
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)
	require.NoError(t, err)

	// await for the trust set
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	registeredXRPLToken, err := runnerEnv.ContractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrplIssuerAddress.String(), xrpl.ConvertCurrencyToString(registeredXRPLCurrency),
	)
	require.NoError(t, err)
	require.Equal(t, coreum.TokenStateEnabled, registeredXRPLToken.State)

	registeredXRPLHexCurrencyToken, err := runnerEnv.ContractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrplIssuerAddress.String(), xrpl.ConvertCurrencyToString(registeredXRPLHexCurrency),
	)
	require.NoError(t, err)
	require.Equal(t, coreum.TokenStateEnabled, registeredXRPLHexCurrencyToken.State)

	lowValue, err := rippledata.NewValue("1.00000111", false)
	require.NoError(t, err)
	maxDecimalsRegisterCurrencyAmount := rippledata.Amount{
		Value:    lowValue,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}

	highValue, err := rippledata.NewValue("100000", false)
	require.NoError(t, err)
	highValueRegisteredCurrencyAmount := rippledata.Amount{
		Value:    highValue,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}

	normalValue, err := rippledata.NewValue("9.9", false)
	require.NoError(t, err)
	registeredHexCurrencyAmount := rippledata.Amount{
		Value:    normalValue,
		Currency: registeredXRPLHexCurrency,
		Issuer:   xrplIssuerAddress,
	}

	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumRecipient)
	require.NoError(t, err)

	// incorrect memo
	runnerEnv.SendXRPLPaymentTx(
		ctx,
		t,
		xrplIssuerAddress,
		runnerEnv.BridgeXRPLAddress,
		maxDecimalsRegisterCurrencyAmount,
		rippledata.Memo{},
	)

	// send tx with partial payment
	runnerEnv.SendXRPLPartialPaymentTx(
		ctx,
		t,
		xrplIssuerAddress,
		runnerEnv.BridgeXRPLAddress,
		highValueRegisteredCurrencyAmount,
		maxDecimalsRegisterCurrencyAmount,
		memo,
	)

	// send tx with high amount
	runnerEnv.SendXRPLPaymentTx(
		ctx,
		t,
		xrplIssuerAddress,
		runnerEnv.BridgeXRPLAddress,
		highValueRegisteredCurrencyAmount,
		memo,
	)

	// send tx with hex currency
	runnerEnv.SendXRPLPaymentTx(ctx, t, xrplIssuerAddress, runnerEnv.BridgeXRPLAddress, registeredHexCurrencyAmount, memo)

	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumRecipient,
		sdk.NewCoin(
			registeredXRPLToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(t, "100001.000001", xrpl.XRPLIssuedTokenDecimals),
		),
	)
	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumRecipient,
		sdk.NewCoin(
			registeredXRPLHexCurrencyToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9.9", xrpl.XRPLIssuedTokenDecimals),
		),
	)
}

func TestSendXRPLOriginatedTokenFromXRPLToCoreumWithTooLowAmount(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 100)
	t.Logf("XRPL currency issuer address: %s", xrplIssuerAddress)

	coreumRecipient := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipient.String())

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	// start relayers
	runnerEnv.StartAllRunnerProcesses()
	// recover tickets so we can register tokens
	runnerEnv.AllocateTickets(ctx, t, 200)

	registeredXRPLCurrency := integrationtests.GenerateXRPLCurrency(t)
	registeredXRPLToken := runnerEnv.RegisterXRPLOriginatedToken(
		ctx,
		t,
		xrplIssuerAddress,
		registeredXRPLCurrency,
		int32(2),
		integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 16),
		sdkmath.NewInt(10),
	)

	zeroAfterTruncationValue, err := rippledata.NewValue("0.00000001", false)
	require.NoError(t, err)
	zeroAfterTruncationAmount := rippledata.Amount{
		Value:    zeroAfterTruncationValue,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}

	lowThanBridgignFeeValue, err := rippledata.NewValue("0.000000000000005", false)
	require.NoError(t, err)
	lowThanBridgignFeeAmount := rippledata.Amount{
		Value:    lowThanBridgignFeeValue,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}

	normalValue, err := rippledata.NewValue("1", false)
	require.NoError(t, err)
	normalAmount := rippledata.Amount{
		Value:    normalValue,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}

	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumRecipient)
	require.NoError(t, err)

	// send tx with normal value
	runnerEnv.SendXRPLPaymentTx(
		ctx,
		t,
		xrplIssuerAddress,
		runnerEnv.BridgeXRPLAddress,
		normalAmount,
		memo,
	)

	// send tx with zero amount after the truncation
	runnerEnv.SendXRPLPaymentTx(
		ctx,
		t,
		xrplIssuerAddress,
		runnerEnv.BridgeXRPLAddress,
		zeroAfterTruncationAmount,
		memo,
	)

	// send tx amount not enough to cover the bridging fee
	runnerEnv.SendXRPLPaymentTx(
		ctx,
		t,
		xrplIssuerAddress,
		runnerEnv.BridgeXRPLAddress,
		lowThanBridgignFeeAmount,
		memo,
	)

	// send tx with normal value one more time to be sure we have processes all txs before first sent tx and after
	runnerEnv.SendXRPLPaymentTx(
		ctx,
		t,
		xrplIssuerAddress,
		runnerEnv.BridgeXRPLAddress,
		normalAmount,
		memo,
	)

	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumRecipient,
		sdk.NewCoin(
			registeredXRPLToken.CoreumDenom,
			// 0.99 + 0.99 since the bridging fee is 1 and sending decimals is 2
			integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1.98", xrpl.XRPLIssuedTokenDecimals),
		),
	)
}

func TestSendXRPLOriginatedTokensFromXRPLToCoreumWithAmountGreaterThanMax(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumRecipient := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipient.String())

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
		integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 16),
		sdkmath.ZeroInt(),
	)

	lowValueToSendFromXRPLtoCoreum, err := rippledata.NewValue("1", false)
	require.NoError(t, err)
	lowAmountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    lowValueToSendFromXRPLtoCoreum,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}

	// the value is greater than max
	highValueToSendFromXRPLtoCoreum, err := rippledata.NewValue("1000", false)
	require.NoError(t, err)
	highValueAmountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    highValueToSendFromXRPLtoCoreum,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}

	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumRecipient)
	require.NoError(t, err)

	for _, amount := range []rippledata.Amount{
		lowAmountToSendFromXRPLtoCoreum,
		highValueAmountToSendFromXRPLtoCoreum, // the tx will be ignored since amount is too high
		lowAmountToSendFromXRPLtoCoreum,
	} {
		runnerEnv.SendXRPLPaymentTx(
			ctx,
			t,
			xrplIssuerAddress,
			runnerEnv.BridgeXRPLAddress,
			amount,
			memo,
		)
	}

	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumRecipient,
		sdk.NewCoin(
			registeredXRPLToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				lowValueToSendFromXRPLtoCoreum.String(),
				xrpl.XRPLIssuedTokenDecimals,
			).MulRaw(2),
		),
	)
}

func TestSendXRPLOriginatedTokenFromXRPLToCoreumAndBackWithTokenDisabling(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumSender := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})
	t.Logf("Coreum sender: %s", coreumSender.String())
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
		sdkmath.ZeroInt(),
	)

	valueToSendFromXRPLtoCoreum, err := rippledata.NewValue("1e10", false)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueToSendFromXRPLtoCoreum,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}
	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumSender)
	require.NoError(t, err)

	runnerEnv.SendXRPLPaymentTx(ctx, t, xrplIssuerAddress, runnerEnv.BridgeXRPLAddress, amountToSendFromXRPLtoCoreum, memo)

	// disable token temporary to let the relayers find the tx and try to relay the evidence with the disabled token
	runnerEnv.UpdateXRPLToken(
		ctx,
		t,
		runnerEnv.ContractOwner,
		xrplIssuerAddress.String(),
		xrpl.ConvertCurrencyToString(registeredXRPLCurrency),
		lo.ToPtr(coreum.TokenStateDisabled),
		nil,
		nil,
		nil,
	)

	select {
	case <-ctx.Done():
		require.NoError(t, ctx.Err())
	case <-time.After(5 * time.Second):
	}

	runnerEnv.UpdateXRPLToken(
		ctx,
		t,
		runnerEnv.ContractOwner,
		xrplIssuerAddress.String(),
		xrpl.ConvertCurrencyToString(registeredXRPLCurrency),
		lo.ToPtr(coreum.TokenStateEnabled),
		nil,
		nil,
		nil,
	)

	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumSender,
		sdk.NewCoin(
			registeredXRPLToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				valueToSendFromXRPLtoCoreum.String(),
				xrpl.XRPLIssuedTokenDecimals,
			),
		),
	)

	// send TrustSet to be able to receive coins
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency)

	amountToSend := integrationtests.ConvertStringWithDecimalsToSDKInt(
		t, valueToSendFromXRPLtoCoreum.String(), xrpl.XRPLIssuedTokenDecimals,
	).QuoRaw(2)
	_, err = runnerEnv.ContractClient.SendToXRPL(
		ctx,
		coreumSender,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSend),
		nil,
	)
	require.NoError(t, err)
	_, err = runnerEnv.ContractClient.SendToXRPL(
		ctx,
		coreumSender,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSend),
		nil,
	)
	require.NoError(t, err)

	// disable token to let the relayers confirm the operation with the disabled token
	runnerEnv.UpdateXRPLToken(
		ctx,
		t,
		runnerEnv.ContractOwner,
		xrplIssuerAddress.String(),
		xrpl.ConvertCurrencyToString(registeredXRPLCurrency),
		lo.ToPtr(coreum.TokenStateDisabled),
		nil,
		nil,
		nil,
	)

	runnerEnv.AwaitNoPendingOperations(ctx, t)

	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency,
	)
	require.Equal(t, "10000000000", xrplRecipientBalance.Value.String())
}

func TestSendXRPLOriginatedTokenViaCrossCurrencyPayment(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	runnerEnv.StartAllRunnerProcesses()
	// recover tickets so we can register tokens
	runnerEnv.AllocateTickets(ctx, t, 200)
	chains.Coreum.FundAccountWithOptions(ctx, t, runnerEnv.ContractOwner, coreumintegration.BalancesOptions{
		Amount: chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount.MulRaw(2),
	})

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 100)
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)
	t.Logf("XRPL currency issuer address: %s", xrplIssuerAddress)

	rippleFOOCurrency, err := rippledata.NewCurrency("FOO")
	require.NoError(t, err)

	sendingPrecision := int32(6)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 30)

	// register XRPL originated token 'FOO'
	registeredFOOCurrency := runnerEnv.RegisterXRPLOriginatedToken(
		ctx,
		t,
		xrplIssuerAddress,
		rippleFOOCurrency,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)

	// Prepare 'FOO' orderbook by creating one sell & buy order.
	offerCreator := chains.XRPL.GenAccount(ctx, t, 10)
	valueFOOToSend, err := rippledata.NewValue("100", false)
	require.NoError(t, err)
	fooCurrencyTrustSetTx := rippledata.TrustSet{
		LimitAmount: rippledata.Amount{
			Value:    valueFOOToSend,
			Currency: rippleFOOCurrency,
			Issuer:   xrplIssuerAddress,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TRUST_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &fooCurrencyTrustSetTx, offerCreator))

	runnerEnv.SendXRPLPaymentTx(
		ctx,
		t,
		xrplIssuerAddress,
		offerCreator,
		fooCurrencyTrustSetTx.LimitAmount,
		rippledata.Memo{},
	)

	t.Logf("offer-creator: account balances before offer create: %s", chains.XRPL.GetAccountBalances(ctx, t, offerCreator))

	// Sell 20 FOO for 1 XRP (price 20 FOO per 1 XRP).
	offer1ValueXRP, err := rippledata.NewValue("1.0", true)
	require.NoError(t, err)
	offer1ValueFOO, err := rippledata.NewValue("20", false)
	require.NoError(t, err)
	offer1CreateTx := rippledata.OfferCreate{
		TakerPays: rippledata.Amount{
			Value:    offer1ValueXRP,
			Currency: xrpl.XRPTokenCurrency,
			Issuer:   xrpl.XRPTokenIssuer,
		},
		TakerGets: rippledata.Amount{
			Value:    offer1ValueFOO,
			Currency: rippleFOOCurrency,
			Issuer:   xrplIssuerAddress,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.OFFER_CREATE,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &offer1CreateTx, offerCreator))

	// Buy 60 FOO for 2.0 XRP (price 30 FOO per 1 XRP).
	offer2ValueXRP, err := rippledata.NewValue("2.0", true)
	require.NoError(t, err)
	offer2ValueFOO, err := rippledata.NewValue("60", false)
	require.NoError(t, err)
	offer2CreateTx := rippledata.OfferCreate{
		TakerPays: rippledata.Amount{
			Value:    offer2ValueFOO,
			Currency: rippleFOOCurrency,
			Issuer:   xrplIssuerAddress,
		},
		TakerGets: rippledata.Amount{
			Value:    offer2ValueXRP,
			Currency: xrpl.XRPTokenCurrency,
			Issuer:   xrpl.XRPTokenIssuer,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.OFFER_CREATE,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &offer2CreateTx, offerCreator))

	// Assert that FOO balance didn't change by creating orders.
	balance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, offerCreator, xrplIssuerAddress, rippleFOOCurrency,
	)
	require.True(t, valueFOOToSend.Equals(*balance.Value))

	// Prepare cross-currency payment sender.
	xrplSender := chains.XRPL.GenAccount(ctx, t, 50)
	coreumRecipient := chains.Coreum.GenAccount()

	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumRecipient)
	require.NoError(t, err)

	// Send 15 FOO (by paying up to 1 XRP) cross-currency payment to bridge.
	// Market price is 20 FOO per 1 XRP, 15 is less than 20, so payment should be successful.
	// Note that sender account doesn't have neither FOO trustline nor balance.
	payment1SendMax, err := rippledata.NewValue("1.0", true)
	require.NoError(t, err)
	payment1Amount, err := rippledata.NewValue("20", false)
	require.NoError(t, err)
	payment1Tx := rippledata.Payment{
		Destination: runnerEnv.BridgeXRPLAddress,
		SendMax: &rippledata.Amount{
			Value:    payment1SendMax,
			Currency: xrpl.XRPTokenCurrency,
			Issuer:   xrpl.XRPTokenIssuer,
		},
		Amount: rippledata.Amount{
			Value:    payment1Amount,
			Currency: rippleFOOCurrency,
			Issuer:   xrplIssuerAddress,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
			Memos: rippledata.Memos{
				memo,
			},
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &payment1Tx, xrplSender))

	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumRecipient,
		sdk.NewCoin(
			registeredFOOCurrency.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				payment1Amount.String(),
				xrpl.XRPLIssuedTokenDecimals,
			),
		),
	)

	// Fund sender account with 100 FOO, so we can send XRP cross-currency payment by paying with FOO.
	valueFOOToSend, err = rippledata.NewValue("100", false)
	require.NoError(t, err)
	fooCurrencyTrustSetTx = rippledata.TrustSet{
		LimitAmount: rippledata.Amount{
			Value:    valueFOOToSend,
			Currency: rippleFOOCurrency,
			Issuer:   xrplIssuerAddress,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TRUST_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &fooCurrencyTrustSetTx, xrplSender))

	runnerEnv.SendXRPLPaymentTx(
		ctx,
		t,
		xrplIssuerAddress,
		xrplSender,
		fooCurrencyTrustSetTx.LimitAmount,
		rippledata.Memo{},
	)

	// Send 2.0 XRP (by paying up to 60 FOO) cross-currency payment to bridge.
	// Market price is 30 FOO per 1 XRP, it exactly & fully matches market order, so payment should be successful.
	payment2SendMax, err := rippledata.NewValue("60", false)
	require.NoError(t, err)
	payment2Amount, err := rippledata.NewValue("2.0", true)
	require.NoError(t, err)
	payment2Tx := rippledata.Payment{
		Destination: runnerEnv.BridgeXRPLAddress,
		SendMax: &rippledata.Amount{
			Value:    payment2SendMax,
			Currency: rippleFOOCurrency,
			Issuer:   xrplIssuerAddress,
		},
		Amount: rippledata.Amount{
			Value:    payment2Amount,
			Currency: xrpl.XRPTokenCurrency,
			Issuer:   xrpl.XRPTokenIssuer,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
			Memos: rippledata.Memos{
				memo,
			},
		},
	}

	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &payment2Tx, xrplSender))

	registeredXRPToken, err := runnerEnv.ContractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrpl.XRPTokenIssuer.String(), xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
	)
	require.NoError(t, err)

	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumRecipient,
		sdk.NewCoin(
			registeredXRPToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				payment2Amount.String(),
				xrpl.XRPCurrencyDecimals,
			),
		),
	)
}

func TestSendFromXRPLToCoreumModuleAccountAndContractAddress(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumModuleAccount := authtypes.NewModuleAddress(govtypes.ModuleName)
	coreumRecipient := chains.Coreum.GenAccount()

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

	valueToSendFromXRPLtoCoreum, err := rippledata.NewValue("1e10", false)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueToSendFromXRPLtoCoreum,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}

	// send to normal account, module account, contract address and then normal account again
	// the first and last sends must succeed, so we know that txs in the middle are processed as well
	runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplIssuerAddress.String(), amountToSendFromXRPLtoCoreum, coreumRecipient)
	runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplIssuerAddress.String(), amountToSendFromXRPLtoCoreum, coreumModuleAccount)
	runnerEnv.SendFromXRPLToCoreum(
		ctx, t, xrplIssuerAddress.String(), amountToSendFromXRPLtoCoreum, runnerEnv.ContractClient.GetContractAddress(),
	)
	runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplIssuerAddress.String(), amountToSendFromXRPLtoCoreum, coreumRecipient)

	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumRecipient,
		sdk.NewCoin(
			registeredXRPLToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				valueToSendFromXRPLtoCoreum.String(),
				xrpl.XRPLIssuedTokenDecimals,
			).MulRaw(2),
		),
	)
}

func TestSendCoreumOriginatedTokenFromCoreumToXRPLAndBackWithDifferentAmountsAndPartialAmount(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient address: %s", xrplRecipientAddress)

	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	coreumRecipientAddress := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipientAddress.String())

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

	// equal to 11.1111 on XRPL, but with the sending prec 2 we expect 11.11 to be received
	amountToSendToXRPL1 := sdkmath.NewInt(111111)
	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSenderAddress,
		xrplRecipientAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL1),
		nil,
	)

	runnerEnv.AwaitNoPendingOperations(ctx, t)

	// check the XRPL recipient balance
	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency,
	)
	require.Equal(t, "11.11", xrplRecipientBalance.Value.String())

	amountToSendToXRPL2 := maxHoldingAmount.QuoRaw(2)
	require.NoError(t, err)
	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSenderAddress,
		xrplRecipientAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL2),
		nil,
	)

	runnerEnv.AwaitNoPendingOperations(ctx, t)

	// check the XRPL recipient balance
	xrplRecipientBalance = runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency,
	)
	require.Equal(t, "50000000001111e-2", xrplRecipientBalance.Value.String())

	// now start sending from XRPL to coreum, coreum originated token

	// expected to receive `1.11`
	lowValue, err := rippledata.NewValue("1.112233", false)
	require.NoError(t, err)
	currencyAmountWithTruncation := rippledata.Amount{
		Value:    lowValue,
		Currency: xrplCurrency,
		Issuer:   runnerEnv.BridgeXRPLAddress,
	}

	highValue, err := rippledata.NewValue("100000", false)
	require.NoError(t, err)
	highValueRegisteredCurrencyAmount := rippledata.Amount{
		Value:    highValue,
		Currency: xrplCurrency,
		Issuer:   runnerEnv.BridgeXRPLAddress,
	}

	normalValue, err := rippledata.NewValue("9.9", false)
	require.NoError(t, err)
	registeredHexCurrencyAmount := rippledata.Amount{
		Value:    normalValue,
		Currency: xrplCurrency,
		Issuer:   runnerEnv.BridgeXRPLAddress,
	}

	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumRecipientAddress)
	require.NoError(t, err)
	// send tx with partial payment
	runnerEnv.SendXRPLPartialPaymentTx(
		ctx,
		t,
		xrplRecipientAddress,
		runnerEnv.BridgeXRPLAddress,
		highValueRegisteredCurrencyAmount,
		currencyAmountWithTruncation,
		memo,
	)

	// send tx with high amount
	runnerEnv.SendXRPLPaymentTx(
		ctx,
		t,
		xrplRecipientAddress,
		runnerEnv.BridgeXRPLAddress,
		highValueRegisteredCurrencyAmount,
		memo,
	)

	// send tx with hex currency
	runnerEnv.SendXRPLPaymentTx(
		ctx,
		t,
		xrplRecipientAddress,
		runnerEnv.BridgeXRPLAddress,
		registeredHexCurrencyAmount,
		memo,
	)

	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumRecipientAddress,
		sdk.NewCoin(
			registeredCoreumOriginatedToken.Denom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(t, "100011.01", int64(tokenDecimals)),
		),
	)
}

func TestSendCoreumOriginatedTokenFromCoreumToXRPLAndBackWithMaliciousRelayer(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient address: %s", xrplRecipientAddress)

	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	coreumRecipientAddress := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipientAddress.String())

	envCfg := DefaultRunnerEnvConfig()
	envCfg.MaliciousRelayerNumber = 1
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	// start relayers
	runnerEnv.StartAllRunnerProcesses()
	// recover tickets so we can register tokens
	runnerEnv.AllocateTickets(ctx, t, 200)

	sendingPrecision := int32(4)
	tokenDecimals := uint32(10)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 16)
	initialAmount := sdkmath.NewIntWithDecimal(1, 16)
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

	amountToSendToXRPL := sdkmath.NewInt(9999999)
	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSenderAddress,
		xrplRecipientAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
		nil,
	)

	runnerEnv.AwaitNoPendingOperations(ctx, t)

	// check the XRPL recipient balance
	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency,
	)
	require.Equal(t, "0.0009", xrplRecipientBalance.Value.String())

	runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplRecipientAddress.String(), xrplRecipientBalance, coreumRecipientAddress)
	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumRecipientAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, sdk.NewInt(9000000)),
	)
}

func TestSendCoreumOriginatedTokenFromCoreumToXRPLAndBackWithTokenDisabling(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient address: %s", xrplRecipientAddress)

	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	coreumRecipientAddress := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipientAddress.String())

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	// start relayers
	runnerEnv.StartAllRunnerProcesses()
	// recover tickets so we can register tokens
	runnerEnv.AllocateTickets(ctx, t, 200)

	// issue asset ft and register it
	sendingPrecision := int32(6)
	tokenDecimals := uint32(6)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 16)
	initialAmount := sdkmath.NewIntWithDecimal(1, 16)
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

	amountToSendToXRPL := sdkmath.NewIntWithDecimal(1, 6)
	_, err = runnerEnv.ContractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
		nil,
	)
	require.NoError(t, err)

	// disable token to let the relayers confirm the operation with the disabled token
	runnerEnv.UpdateCoreumToken(
		ctx,
		t,
		runnerEnv.ContractOwner,
		registeredCoreumOriginatedToken.Denom,
		lo.ToPtr(coreum.TokenStateDisabled),
		nil,
		nil,
		nil,
	)

	runnerEnv.AwaitNoPendingOperations(ctx, t)

	runnerEnv.UpdateCoreumToken(
		ctx,
		t,
		runnerEnv.ContractOwner,
		registeredCoreumOriginatedToken.Denom,
		lo.ToPtr(coreum.TokenStateEnabled),
		nil,
		nil,
		nil,
	)

	// check the XRPL recipient balance
	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency,
	)
	require.Equal(t, "1", xrplRecipientBalance.Value.String())

	// now start sending from XRPL to coreum, coreum originated token

	valueToSendToCoreum, err := rippledata.NewValue("0.1", false)
	require.NoError(t, err)
	amountToSendToCoreum := rippledata.Amount{
		Value:    valueToSendToCoreum,
		Currency: xrplCurrency,
		Issuer:   runnerEnv.BridgeXRPLAddress,
	}

	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumRecipientAddress)
	require.NoError(t, err)

	runnerEnv.SendXRPLPaymentTx(
		ctx,
		t,
		xrplRecipientAddress,
		runnerEnv.BridgeXRPLAddress,
		amountToSendToCoreum,
		memo,
	)

	// disable token temporary to let the relayers find the tx and try to relay the evidence with the disabled token
	runnerEnv.UpdateCoreumToken(
		ctx,
		t,
		runnerEnv.ContractOwner,
		registeredCoreumOriginatedToken.Denom,
		lo.ToPtr(coreum.TokenStateDisabled),
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)

	select {
	case <-ctx.Done():
		require.NoError(t, ctx.Err())
	case <-time.After(5 * time.Second):
	}

	runnerEnv.UpdateCoreumToken(
		ctx,
		t,
		runnerEnv.ContractOwner,
		registeredCoreumOriginatedToken.Denom,
		lo.ToPtr(coreum.TokenStateEnabled),
		nil,
		nil,
		nil,
	)

	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumRecipientAddress,
		sdk.NewCoin(
			registeredCoreumOriginatedToken.Denom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.1", int64(tokenDecimals)),
		),
	)
}

func TestSendCoreumOriginatedTokenWithBurningRateAndSendingCommissionFromCoreumToXRPLAndBack(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient address: %s", xrplRecipientAddress)

	coreumIssuerAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumIssuerAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 7),
	})

	coreumRecipientAddress := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipientAddress.String())

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	// start relayers
	runnerEnv.StartAllRunnerProcesses()
	// recover tickets so we can register tokens
	runnerEnv.AllocateTickets(ctx, t, 200)

	sendingPrecision := int32(2)
	tokenDecimals := uint32(4)
	initialAmount := sdkmath.NewIntWithDecimal(1, 16)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 16)
	registeredCoreumOriginatedToken := runnerEnv.IssueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		coreumIssuerAddress,
		tokenDecimals,
		initialAmount,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)

	// send coins to sender to test the commission
	msgSend := &banktypes.MsgSend{
		FromAddress: coreumIssuerAddress.String(),
		ToAddress:   coreumSenderAddress.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(registeredCoreumOriginatedToken.Denom, 10_000_000)),
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumIssuerAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		msgSend,
	)
	require.NoError(t, err)

	// send TrustSet to be able to receive coins from the bridge
	xrplCurrency, err := rippledata.NewCurrency(registeredCoreumOriginatedToken.XRPLCurrency)
	require.NoError(t, err)
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency)

	amountToSendToXRPL := sdkmath.NewIntWithDecimal(1, 6)
	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSenderAddress,
		xrplRecipientAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
		nil,
	)

	// contract balance holds the token now
	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		runnerEnv.ContractClient.GetContractAddress(),
		sdk.NewCoin(
			registeredCoreumOriginatedToken.Denom,
			amountToSendToXRPL,
		),
	)

	runnerEnv.AwaitNoPendingOperations(ctx, t)

	// check the XRPL recipient balance
	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency,
	)
	require.Equal(t, "100", xrplRecipientBalance.Value.String())

	// send back full balance
	runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplRecipientAddress.String(), xrplRecipientBalance, coreumRecipientAddress)

	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumRecipientAddress,
		sdk.NewCoin(
			registeredCoreumOriginatedToken.Denom,
			amountToSendToXRPL,
		),
	)
	// contract balance should be zero now
	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		runnerEnv.ContractClient.GetContractAddress(),
		sdk.NewCoin(
			registeredCoreumOriginatedToken.Denom,
			sdk.ZeroInt(),
		),
	)
}

func TestSendFromCoreumToXRPLProhibitedAddresses(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient address: %s", xrplRecipientAddress)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	// start relayers
	runnerEnv.StartAllRunnerProcesses()
	// recover tickets so we can register tokens
	runnerEnv.AllocateTickets(ctx, t, 200)

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

	// send TrustSet to be able to receive coins
	xrplCurrency, err := rippledata.NewCurrency(registeredCoreumOriginatedToken.XRPLCurrency)
	require.NoError(t, err)
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency)

	amountToSendToXRPL := sdkmath.NewIntWithDecimal(1, 6)
	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSenderAddress,
		xrplRecipientAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
		nil,
	)

	runnerEnv.AwaitNoPendingOperations(ctx, t)

	// check the XRPL recipient balance
	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency,
	)
	require.Equal(t, "100", xrplRecipientBalance.Value.String())

	// add the recipient to the list of the prohibited recipients and repeat the sending action
	prohibitedXRPLRecipients, err := runnerEnv.BridgeClient.GetProhibitedXRPLRecipients(ctx)
	require.NoError(t, err)

	prohibitedXRPLRecipients = append(prohibitedXRPLRecipients, xrplRecipientAddress.String())
	require.NoError(
		t,
		runnerEnv.BridgeClient.UpdateProhibitedXRPLRecipients(ctx, runnerEnv.ContractOwner, prohibitedXRPLRecipients),
	)

	err = runnerEnv.BridgeClient.SendFromCoreumToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
		nil,
	)
	require.True(t, coreum.IsProhibitedRecipientError(err), err)
}
