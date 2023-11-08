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

	xrplIssuerAcc := chains.XRPL.GenAccount(ctx, t, 100)
	t.Logf("XRPL currency issuer account: %s", xrplIssuerAcc)

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

	// fund owner to cover registration fees
	chains.Coreum.FundAccountWithOptions(ctx, t, runnerEnv.ContractOwner, coreumintegration.BalancesOptions{
		Amount: chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount.MulRaw(2),
	})

	bridgeXRPLAccountInfo, err := chains.XRPL.RPCClient().AccountInfo(ctx, runnerEnv.bridgeXRPLAddress)
	require.NoError(t, err)

	// recover tickets so we can register tokens
	numberOfTicketsToAllocate := uint32(200)
	chains.XRPL.FundAccountForTicketAllocation(ctx, t, runnerEnv.bridgeXRPLAddress, numberOfTicketsToAllocate)
	_, err = runnerEnv.ContractClient.RecoverTickets(ctx, runnerEnv.ContractOwner, *bridgeXRPLAccountInfo.AccountData.Sequence, &numberOfTicketsToAllocate)
	require.NoError(t, err)

	// start relayers
	runnerEnv.StartAllRunnerProcesses(ctx, t)

	require.NoError(t, err)
	runnerEnv.AwaitNoPendingOperations(ctx, t)
	availableTickets, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Len(t, availableTickets, int(numberOfTicketsToAllocate))

	// register XRPL origin token with 3 chars
	_, err = runnerEnv.ContractClient.RegisterXRPLToken(ctx, runnerEnv.ContractOwner, xrplIssuerAcc.String(), xrpl.ConvertCurrencyToString(registeredXRPLCurrency), sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)

	// register XRPL origin token with 20 chars
	_, err = runnerEnv.ContractClient.RegisterXRPLToken(ctx, runnerEnv.ContractOwner, xrplIssuerAcc.String(), xrpl.ConvertCurrencyToString(registeredXRPLHexCurrency), sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)

	// await for the trust set
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	registeredXRPLToken, err := runnerEnv.ContractClient.GetXRPLToken(ctx, xrplIssuerAcc.String(), xrpl.ConvertCurrencyToString(registeredXRPLCurrency))
	require.NoError(t, err)
	require.NotNil(t, registeredXRPLToken)
	require.Equal(t, coreum.TokenStateEnabled, registeredXRPLToken.State)

	registeredXRPLHexCurrencyToken, err := runnerEnv.ContractClient.GetXRPLToken(ctx, xrplIssuerAcc.String(), xrpl.ConvertCurrencyToString(registeredXRPLHexCurrency))
	require.NoError(t, err)
	require.NotNil(t, registeredXRPLHexCurrencyToken)
	require.Equal(t, coreum.TokenStateEnabled, registeredXRPLHexCurrencyToken.State)

	lowValue, err := rippledata.NewValue("1.00000111", false)
	require.NoError(t, err)
	maxDecimalsRegisterCurrencyAmount := rippledata.Amount{
		Value:    lowValue,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAcc,
	}

	highValue, err := rippledata.NewValue("100000", false)
	require.NoError(t, err)
	highValueRegisteredCurrencyAmount := rippledata.Amount{
		Value:    highValue,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAcc,
	}

	normalValue, err := rippledata.NewValue("9.9", false)
	require.NoError(t, err)
	registeredHexCurrencyAmount := rippledata.Amount{
		Value:    normalValue,
		Currency: registeredXRPLHexCurrency,
		Issuer:   xrplIssuerAcc,
	}

	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumRecipient)
	require.NoError(t, err)

	// incorrect memo
	runnerEnv.SendXRPLPaymentTx(ctx, t, xrplIssuerAcc, runnerEnv.bridgeXRPLAddress, maxDecimalsRegisterCurrencyAmount, rippledata.Memo{})

	// send correct transactions

	// send tx with partial payment
	runnerEnv.SendXRPLPartialPaymentTx(ctx, t, xrplIssuerAcc, runnerEnv.bridgeXRPLAddress, highValueRegisteredCurrencyAmount, maxDecimalsRegisterCurrencyAmount, memo)

	// send tx with high amount
	runnerEnv.SendXRPLPaymentTx(ctx, t, xrplIssuerAcc, runnerEnv.bridgeXRPLAddress, highValueRegisteredCurrencyAmount, memo)

	// send tx with hex currency
	runnerEnv.SendXRPLPaymentTx(ctx, t, xrplIssuerAcc, runnerEnv.bridgeXRPLAddress, registeredHexCurrencyAmount, memo)

	runnerEnv.AwaitCoreumBalance(ctx, t, chains.Coreum, coreumRecipient, sdk.NewCoin(registeredXRPLToken.CoreumDenom, integrationtests.ConvertStringWithDecimalsToSDKInt(t, "100001.000001", XRPLTokenDecimals)))
	runnerEnv.AwaitCoreumBalance(ctx, t, chains.Coreum, coreumRecipient, sdk.NewCoin(registeredXRPLHexCurrencyToken.CoreumDenom, integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9.9", XRPLTokenDecimals)))
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

	registeredXRPLCurrency, err := rippledata.NewCurrency("CRC")
	require.NoError(t, err)

	// fund owner to cover registration fees
	chains.Coreum.FundAccountWithOptions(ctx, t, runnerEnv.ContractOwner, coreumintegration.BalancesOptions{
		Amount: chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount,
	})

	// start relayers
	runnerEnv.StartAllRunnerProcesses(ctx, t)
	runnerEnv.AllocateTickets(ctx, t, 200)

	// register XRPL token
	registeredXRPLToken := runnerEnv.RegisterXRPLTokenAndAwaitTrustSet(ctx, t, xrplCurrencyIssuerAcc, registeredXRPLCurrency, sendingPrecision, maxHoldingAmount)

	// send
	value, err := rippledata.NewValue("9999999999999.1111", false)
	require.NoError(t, err)
	registerCurrencyAmount := rippledata.Amount{
		Value:    value,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplCurrencyIssuerAcc,
	}
	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumRecipient)
	require.NoError(t, err)

	runnerEnv.SendXRPLPaymentTx(ctx, t, xrplCurrencyIssuerAcc, runnerEnv.bridgeXRPLAddress, registerCurrencyAmount, memo)
	runnerEnv.AwaitCoreumBalance(ctx, t, chains.Coreum, coreumRecipient, sdk.NewCoin(registeredXRPLToken.CoreumDenom, integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999999.111", XRPLTokenDecimals)))
}
