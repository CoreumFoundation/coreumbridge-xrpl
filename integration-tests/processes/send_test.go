//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"context"
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

	"github.com/CoreumFoundation/coreum-tools/pkg/retry"
	"github.com/CoreumFoundation/coreum/v4/pkg/client"
	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	assetfttypes "github.com/CoreumFoundation/coreum/v4/x/asset/ft/types"
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

	registeredXRPLCurrency, err := rippledata.NewCurrency("RCP")
	require.NoError(t, err)

	bridgingFee := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 24)
	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)
	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)
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
		chains.Coreum,
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

func TestSendFromXRPLToCoreumModuleAccount(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumModuleAccount := authtypes.NewModuleAddress(govtypes.ModuleName)
	coreumRecipient := chains.Coreum.GenAccount()

	registeredXRPLCurrency, err := rippledata.NewCurrency("RCP")
	require.NoError(t, err)

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)
	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)
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

	// send to normal account, then module account and then normal account again, the first and last sends must succeed.
	runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplIssuerAddress.String(), amountToSendFromXRPLtoCoreum, coreumRecipient)
	runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplIssuerAddress.String(), amountToSendFromXRPLtoCoreum, coreumModuleAccount)
	runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplIssuerAddress.String(), amountToSendFromXRPLtoCoreum, coreumRecipient)

	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		chains.Coreum,
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

	registeredXRPLCurrency, err := rippledata.NewCurrency("SLO")
	require.NoError(t, err)

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
		chains.Coreum,
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

	registeredXRPLCurrency, err := rippledata.NewCurrency("TNL")
	require.NoError(t, err)

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)
	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)

	sendingPrecision := int32(2)
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
		chains.Coreum,
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
		chains.Coreum,
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

	currencyHexSymbol := hex.EncodeToString([]byte(strings.Repeat("X", 20)))
	registeredXRPLCurrency, err := rippledata.NewCurrency(currencyHexSymbol)
	require.NoError(t, err)

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)
	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)
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
		chains.Coreum,
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

func TestSendXRPLOriginatedTokenFromXRPLToCoreumWithTicketsReallocation(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.UsedTicketSequenceThreshold = 3
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(5))
	sendingCount := 10

	coreumSender := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})
	t.Logf("Coreum sender: %s", coreumSender.String())
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient: %s", xrplRecipientAddress.String())

	currencyHexSymbol := hex.EncodeToString([]byte(strings.Repeat("X", 20)))
	registeredXRPLCurrency, err := rippledata.NewCurrency(currencyHexSymbol)
	require.NoError(t, err)

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)
	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)
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
		chains.Coreum,
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

	totalSent := sdkmath.ZeroInt()
	amountToSend := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "10", xrpl.XRPLIssuedTokenDecimals)
	for i := 0; i < sendingCount; i++ {
		retryCtx, retryCtxCancel := context.WithTimeout(ctx, 15*time.Second)
		require.NoError(t, retry.Do(retryCtx, 500*time.Millisecond, func() error {
			_, err = runnerEnv.ContractClient.SendToXRPL(
				ctx,
				coreumSender,
				xrplRecipientAddress.String(),
				sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSend),
				nil,
			)
			if err == nil {
				return nil
			}
			if coreum.IsLastTicketReservedError(err) || coreum.IsNoAvailableTicketsError(err) {
				t.Logf("No tickets left, waiting for new tickets...")
				return retry.Retryable(err)
			}
			require.NoError(t, err)
			return nil
		}))
		retryCtxCancel()
		totalSent = totalSent.Add(amountToSend)
	}
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx,
		t,
		xrplRecipientAddress,
		xrplIssuerAddress,
		registeredXRPLCurrency,
	)
	require.Equal(
		t,
		totalSent.Quo(sdkmath.NewIntWithDecimal(1, xrpl.XRPLIssuedTokenDecimals)).String(),
		xrplRecipientBalance.Value.String(),
	)
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
		chains.Coreum,
		coreumRecipient,
		sdk.NewCoin(
			registeredXRPLToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(t, "100001.000001", xrpl.XRPLIssuedTokenDecimals),
		),
	)
	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		chains.Coreum,
		coreumRecipient,
		sdk.NewCoin(
			registeredXRPLHexCurrencyToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9.9", xrpl.XRPLIssuedTokenDecimals),
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

	registeredXRPLCurrency, err := rippledata.NewCurrency("RCP")
	require.NoError(t, err)

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)
	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)
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
		chains.Coreum,
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

func TestRecoverXRPLOriginatedTokenRegistrationAndSendFromXRPLToCoreumAndBack(t *testing.T) {
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

	registeredXRPLCurrency, err := rippledata.NewCurrency("RCP")
	require.NoError(t, err)

	// register the XRPL token and await for the tx to be failed
	runnerEnv.Chains.Coreum.FundAccountWithOptions(ctx, t, runnerEnv.ContractOwner, coreumintegration.BalancesOptions{
		Amount: runnerEnv.Chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount,
	})

	// gen account but don't fund it to let the tx to fail since the account won't exist on the XRPL side
	xrplIssuerAddress := chains.XRPL.GenEmptyAccount(t)

	_, err = runnerEnv.ContractClient.RegisterXRPLToken(
		ctx,
		runnerEnv.ContractOwner,
		xrplIssuerAddress.String(),
		xrpl.ConvertCurrencyToString(registeredXRPLCurrency),
		int32(6),
		integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30),
		sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	runnerEnv.AwaitNoPendingOperations(ctx, t)
	registeredXRPLToken, err := runnerEnv.ContractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrplIssuerAddress.String(), xrpl.ConvertCurrencyToString(registeredXRPLCurrency),
	)
	require.NoError(t, err)
	require.Equal(t, coreum.TokenStateInactive, registeredXRPLToken.State)

	// create the account on the XRPL and send some XRP on top to cover fees
	runnerEnv.Chains.XRPL.CreateAccount(ctx, t, xrplIssuerAddress, 1)
	// recover from owner
	_, err = runnerEnv.ContractClient.RecoverXRPLTokenRegistration(
		ctx, runnerEnv.ContractOwner, xrplIssuerAddress.String(), xrpl.ConvertCurrencyToString(registeredXRPLCurrency),
	)
	require.NoError(t, err)
	runnerEnv.AwaitNoPendingOperations(ctx, t)
	// now the token is enabled
	registeredXRPLToken, err = runnerEnv.ContractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrplIssuerAddress.String(), xrpl.ConvertCurrencyToString(registeredXRPLCurrency),
	)
	require.NoError(t, err)
	require.Equal(t, coreum.TokenStateEnabled, registeredXRPLToken.State)

	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)

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
		chains.Coreum,
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

	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSender,
		xrplRecipientAddress,
		sdk.NewCoin(
			registeredXRPLToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				valueToSendFromXRPLtoCoreum.String(),
				xrpl.XRPLIssuedTokenDecimals),
		),
		nil,
	)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx,
		t,
		xrplRecipientAddress,
		xrplIssuerAddress,
		registeredXRPLCurrency,
	)
	require.Equal(t, valueToSendFromXRPLtoCoreum.String(), xrplRecipientBalance.Value.String())
}

func TestSendCoreumOriginatedTokenFromCoreumToXRPLAndBackWithDifferentAmountsAndPartialAmount(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient address: %s", xrplRecipientAddress)

	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewInt(10_000_000)),
	})

	coreumRecipientAddress := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipientAddress.String())

	// issue asset ft and register it
	sendingPrecision := int32(2)
	tokenDecimals := uint32(4)
	maxHoldingAmount, ok := sdk.NewIntFromString("10000000000000000")
	require.True(t, ok)
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSenderAddress.String(),
		Symbol:        "symbol",
		Subunit:       "subunit",
		Precision:     tokenDecimals, // token decimals in terms of the contract
		InitialAmount: maxHoldingAmount,
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	// start relayers
	runnerEnv.StartAllRunnerProcesses()
	// recover tickets so we can register tokens
	runnerEnv.AllocateTickets(ctx, t, 200)

	// register Coreum originated token
	denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)

	registeredCoreumOriginatedToken := runnerEnv.RegisterCoreumOriginatedToken(
		ctx,
		t,
		denom,
		tokenDecimals,
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
		chains.Coreum,
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
		Amount: issueFee.Amount.Add(sdkmath.NewInt(10_000_000)),
	})

	coreumRecipientAddress := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipientAddress.String())

	// issue asset ft and register it
	sendingPrecision := int32(4)
	tokenDecimals := uint32(10)
	maxHoldingAmount, ok := sdk.NewIntFromString("10000000000000000")
	require.True(t, ok)
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSenderAddress.String(),
		Symbol:        "symbol",
		Subunit:       "subunit",
		Precision:     tokenDecimals, // token decimals in terms of the contract
		InitialAmount: maxHoldingAmount,
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.MaliciousRelayerNumber = 1
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	// start relayers
	runnerEnv.StartAllRunnerProcesses()
	// recover tickets so we can register tokens
	runnerEnv.AllocateTickets(ctx, t, 200)

	// register Coreum originated token
	denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)
	registeredCoreumOriginatedToken := runnerEnv.RegisterCoreumOriginatedToken(
		ctx,
		t,
		denom,
		tokenDecimals,
		sendingPrecision,
		maxHoldingAmount,
		sdk.ZeroInt(),
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
		chains.Coreum,
		coreumRecipientAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, sdk.NewInt(9000000)),
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

	registeredXRPLCurrency, err := rippledata.NewCurrency("cRn")
	require.NoError(t, err)

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)
	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)
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
		chains.Coreum,
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

func TestSendCoreumOriginatedTokenFromCoreumToXRPLAndBackWithTokenDisabling(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient address: %s", xrplRecipientAddress)

	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewInt(10_000_000)),
	})

	coreumRecipientAddress := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipientAddress.String())

	// issue asset ft and register it
	sendingPrecision := int32(6)
	tokenDecimals := uint32(6)
	maxHoldingAmount, ok := sdk.NewIntFromString("10000000000000000")
	require.True(t, ok)
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSenderAddress.String(),
		Symbol:        "symbol",
		Subunit:       "subunit",
		Precision:     tokenDecimals, // token decimals in terms of the contract
		InitialAmount: maxHoldingAmount,
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	// start relayers
	runnerEnv.StartAllRunnerProcesses()
	// recover tickets so we can register tokens
	runnerEnv.AllocateTickets(ctx, t, 200)

	// register Coreum originated token
	denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)
	_, err = runnerEnv.ContractClient.RegisterCoreumToken(
		ctx, runnerEnv.ContractOwner, denom, tokenDecimals, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err := runnerEnv.ContractClient.GetCoreumTokenByDenom(ctx, denom)
	require.NoError(t, err)

	// send TrustSet to be able to receive coins from the bridge
	xrplCurrency, err := rippledata.NewCurrency(registeredCoreumOriginatedToken.XRPLCurrency)
	require.NoError(t, err)
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency)

	amountToSendToXRPL := sdkmath.NewInt(1000000)
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
		denom,
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
		denom,
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
		denom,
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
		denom,
		lo.ToPtr(coreum.TokenStateEnabled),
		nil,
		nil,
		nil,
	)

	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		chains.Coreum,
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
		Amount: issueFee.Amount.Add(sdkmath.NewInt(10_000_000)),
	})

	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(10_000_000),
	})

	coreumRecipientAddress := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipientAddress.String())

	// issue asset ft and register it
	sendingPrecision := int32(2)
	tokenDecimals := uint32(4)
	maxHoldingAmount, ok := sdk.NewIntFromString("10000000000000000")
	require.True(t, ok)
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:             coreumIssuerAddress.String(),
		Symbol:             "symbol",
		Subunit:            "subunit",
		Precision:          tokenDecimals,
		InitialAmount:      maxHoldingAmount,
		BurnRate:           sdk.MustNewDecFromStr("0.1"),
		SendCommissionRate: sdk.MustNewDecFromStr("0.2"),
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumIssuerAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)

	// send coins to sender to test the commission
	denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumIssuerAddress)
	msgSend := &banktypes.MsgSend{
		FromAddress: coreumIssuerAddress.String(),
		ToAddress:   coreumSenderAddress.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(denom, 10_000_000)),
	}
	_, err = client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumIssuerAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		msgSend,
	)
	require.NoError(t, err)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	// start relayers
	runnerEnv.StartAllRunnerProcesses()
	// recover tickets so we can register tokens
	runnerEnv.AllocateTickets(ctx, t, 200)

	// register Coreum originated token
	registeredCoreumOriginatedToken := runnerEnv.RegisterCoreumOriginatedToken(
		ctx,
		t,
		denom,
		tokenDecimals,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)

	// send TrustSet to be able to receive coins from the bridge
	xrplCurrency, err := rippledata.NewCurrency(registeredCoreumOriginatedToken.XRPLCurrency)
	require.NoError(t, err)
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency)

	amountToSendToXRPL := sdkmath.NewInt(1_000_000)
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
		chains.Coreum,
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
		chains.Coreum,
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
		chains.Coreum,
		runnerEnv.ContractClient.GetContractAddress(),
		sdk.NewCoin(
			registeredCoreumOriginatedToken.Denom,
			sdk.ZeroInt(),
		),
	)
}

func TestSendCoreumOriginatedWithInvalidRecipientToXRPL(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplRecipientAddress := genInvalidXRPLAddress(t)

	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewInt(10_000_000)),
	})

	coreumRecipientAddress := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipientAddress.String())

	// issue asset ft and register it
	sendingPrecision := int32(2)
	tokenDecimals := uint32(4)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 20)
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSenderAddress.String(),
		Symbol:        "symbol",
		Subunit:       "subunit",
		Precision:     tokenDecimals,
		InitialAmount: maxHoldingAmount,
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	// start relayers
	runnerEnv.StartAllRunnerProcesses()
	// recover tickets so we can register tokens
	runnerEnv.AllocateTickets(ctx, t, xrpl.MaxTicketsToAllocate)

	// register Coreum originated token
	denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)
	bridgingFee := sdkmath.NewInt(10)
	registeredCoreumOriginatedToken := runnerEnv.RegisterCoreumOriginatedToken(
		ctx,
		t,
		denom,
		tokenDecimals,
		sendingPrecision,
		maxHoldingAmount,
		bridgingFee,
	)

	pendingRefunds, err := runnerEnv.BridgeClient.GetPendingRefunds(ctx, coreumSenderAddress)
	require.NoError(t, err)
	require.Empty(t, pendingRefunds)

	availableTicketsBefore, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, availableTicketsBefore)

	amountToSendToXRPL := sdkmath.NewInt(1_000_000).Add(bridgingFee)
	// use the client directly to prevent the validation
	_, err = runnerEnv.ContractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
		nil,
	)
	require.NoError(t, err)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	// check that the sending was cancelled
	pendingRefunds, err = runnerEnv.BridgeClient.GetPendingRefunds(ctx, coreumSenderAddress)
	require.NoError(t, err)
	require.Len(t, pendingRefunds, 1)

	// check that the amount is in the pending refunds and the bridging fee is taken
	require.Equal(t,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL.Sub(bridgingFee)).String(),
		pendingRefunds[0].Coin.String(),
	)

	//  validate that ticket is returned back
	availableTicketsAfter, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.EqualValues(t, availableTicketsBefore, availableTicketsAfter)
}

func genInvalidXRPLAddress(t *testing.T) string {
	stringAcc := xrpl.GenPrivKeyTxSigner().Account().String()
	invalidStringAcc := string([]rune(stringAcc)[0:1]) + string(lo.Shuffle([]rune(stringAcc)[1:]))
	_, err := rippledata.NewAccountFromAddress(invalidStringAcc)
	require.ErrorContains(t, err, xrpl.BadBase58ChecksumError)

	return invalidStringAcc
}
