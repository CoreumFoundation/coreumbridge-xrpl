//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"encoding/hex"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v3/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestRegisterXRPLTokensAndSendFromXRPLToCoreum(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplCurrencyIssuerAcc := chains.XRPL.GenAccount(ctx, t, 100)
	t.Logf("XRPL currency issuer account: %s", xrplCurrencyIssuerAcc)

	coreumRecipient := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipient.String())

	sendingPrecision := int32(6)
	maxHoldingAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	xrplRegisteredCurrency, err := rippledata.NewCurrency("RCP")
	require.NoError(t, err)

	// register token with 20 chars
	currencyHexSymbol := hex.EncodeToString([]byte(strings.Repeat("R", 20)))
	xrplRegisteredHexCurrency, err := rippledata.NewCurrency(currencyHexSymbol)
	require.NoError(t, err)

	// fund owner to cover registration fees
	chains.Coreum.FundAccountWithOptions(ctx, t, runnerEnv.ContractOwner, coreumintegration.BalancesOptions{
		Amount: chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount.MulRaw(2),
	})

	xrplBridgeAccountInfo, err := chains.XRPL.RPCClient().AccountInfo(ctx, runnerEnv.XRPLBridgeAccount)
	require.NoError(t, err)

	// recover tickets so we can register tokens
	numberOfTicketsToAllocate := uint32(200)
	chains.XRPL.FundAccountForTicketAllocation(ctx, t, runnerEnv.XRPLBridgeAccount, numberOfTicketsToAllocate)
	_, err = runnerEnv.ContractClient.RecoverTickets(ctx, runnerEnv.ContractOwner, *xrplBridgeAccountInfo.AccountData.Sequence, &numberOfTicketsToAllocate)
	require.NoError(t, err)

	// start relayers
	runnerEnv.StartAllRunnerProcesses(ctx, t)

	require.NoError(t, err)
	runnerEnv.AwaitNoPendingOperations(ctx, t)
	availableTickets, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Len(t, availableTickets, int(numberOfTicketsToAllocate))

	// register XRPL native token with 3 chars
	_, err = runnerEnv.ContractClient.RegisterXRPLToken(ctx, runnerEnv.ContractOwner, xrplCurrencyIssuerAcc.String(), xrpl.ConvertCurrencyToString(xrplRegisteredCurrency), sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)

	// register XRPL native token with 20 chars
	_, err = runnerEnv.ContractClient.RegisterXRPLToken(ctx, runnerEnv.ContractOwner, xrplCurrencyIssuerAcc.String(), xrpl.ConvertCurrencyToString(xrplRegisteredHexCurrency), sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)

	// await for the trust set
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	xrplRegisteredToken, err := runnerEnv.ContractClient.GetXRPLToken(ctx, xrplCurrencyIssuerAcc.String(), xrpl.ConvertCurrencyToString(xrplRegisteredCurrency))
	require.NoError(t, err)
	require.NotNil(t, xrplRegisteredToken)
	require.Equal(t, coreum.TokenStateEnabled, xrplRegisteredToken.State)

	xrplRegisteredHexCurrencyToken, err := runnerEnv.ContractClient.GetXRPLToken(ctx, xrplCurrencyIssuerAcc.String(), xrpl.ConvertCurrencyToString(xrplRegisteredHexCurrency))
	require.NoError(t, err)
	require.NotNil(t, xrplRegisteredHexCurrencyToken)
	require.Equal(t, coreum.TokenStateEnabled, xrplRegisteredHexCurrencyToken.State)

	lowValue, err := rippledata.NewValue("1.00000111", false)
	require.NoError(t, err)
	maxDecimalsRegisterCurrencyAmount := rippledata.Amount{
		Value:    lowValue,
		Currency: xrplRegisteredCurrency,
		Issuer:   xrplCurrencyIssuerAcc,
	}

	highValue, err := rippledata.NewValue("100000", false)
	require.NoError(t, err)
	highValueRegisteredCurrencyAmount := rippledata.Amount{
		Value:    highValue,
		Currency: xrplRegisteredCurrency,
		Issuer:   xrplCurrencyIssuerAcc,
	}

	normalValue, err := rippledata.NewValue("9.9", false)
	require.NoError(t, err)
	registeredHexCurrencyAmount := rippledata.Amount{
		Value:    normalValue,
		Currency: xrplRegisteredHexCurrency,
		Issuer:   xrplCurrencyIssuerAcc,
	}

	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumRecipient)
	require.NoError(t, err)

	// incorrect memo
	runnerEnv.SendXRPLPaymentTx(ctx, t, xrplCurrencyIssuerAcc, runnerEnv.XRPLBridgeAccount, maxDecimalsRegisterCurrencyAmount, rippledata.Memo{})

	// send correct transactions

	// send tx with partial payment
	runnerEnv.SendXRPLPartialPaymentTx(ctx, t, xrplCurrencyIssuerAcc, runnerEnv.XRPLBridgeAccount, highValueRegisteredCurrencyAmount, maxDecimalsRegisterCurrencyAmount, memo)

	// send tx with high amount
	runnerEnv.SendXRPLPaymentTx(ctx, t, xrplCurrencyIssuerAcc, runnerEnv.XRPLBridgeAccount, highValueRegisteredCurrencyAmount, memo)

	// send tx with hex currency
	runnerEnv.SendXRPLPaymentTx(ctx, t, xrplCurrencyIssuerAcc, runnerEnv.XRPLBridgeAccount, registeredHexCurrencyAmount, memo)

	runnerEnv.AwaitCoreumBalance(ctx, t, chains.Coreum, coreumRecipient, sdk.NewCoin(xrplRegisteredToken.CoreumDenom, integrationtests.ConvertStringWithDecimalsToSDKInt(t, "100001.000001", XRPLTokenDecimals)))
	runnerEnv.AwaitCoreumBalance(ctx, t, chains.Coreum, coreumRecipient, sdk.NewCoin(xrplRegisteredHexCurrencyToken.CoreumDenom, integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9.9", XRPLTokenDecimals)))
}

func TestSendFromXRPLToCoreumWithMaliciousRelayer(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplCurrencyIssuerAcc := chains.XRPL.GenAccount(ctx, t, 100)
	t.Logf("XRPL currency issuer account: %s", xrplCurrencyIssuerAcc)

	coreumRecipient := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipient.String())

	sendingPrecision := int32(6)
	maxHoldingAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)

	envCfg := DefaultRunnerEnvConfig()
	// add malicious relayers to the config
	envCfg.RelayerNumber = 5
	envCfg.MaliciousRelayerNumber = 2
	envCfg.SigningThreshold = 3
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	xrplRegisteredCurrency, err := rippledata.NewCurrency("CRC")
	require.NoError(t, err)

	// fund owner to cover registration fees
	chains.Coreum.FundAccountWithOptions(ctx, t, runnerEnv.ContractOwner, coreumintegration.BalancesOptions{
		Amount: chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount,
	})

	// start relayers
	runnerEnv.StartAllRunnerProcesses(ctx, t)
	runnerEnv.AllocateTickets(ctx, t, 200)

	// register XRPL token
	xrplRegisteredToken := runnerEnv.RegisterXRPLTokenAndAwaitTrustSet(ctx, t, xrplCurrencyIssuerAcc, xrplRegisteredCurrency, sendingPrecision, maxHoldingAmount)

	// send
	value, err := rippledata.NewValue("9999999999999.1111", false)
	require.NoError(t, err)
	registerCurrencyAmount := rippledata.Amount{
		Value:    value,
		Currency: xrplRegisteredCurrency,
		Issuer:   xrplCurrencyIssuerAcc,
	}
	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumRecipient)
	require.NoError(t, err)

	runnerEnv.SendXRPLPaymentTx(ctx, t, xrplCurrencyIssuerAcc, runnerEnv.XRPLBridgeAccount, registerCurrencyAmount, memo)
	runnerEnv.AwaitCoreumBalance(ctx, t, chains.Coreum, coreumRecipient, sdk.NewCoin(xrplRegisteredToken.CoreumDenom, integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999999.111", XRPLTokenDecimals)))
}
