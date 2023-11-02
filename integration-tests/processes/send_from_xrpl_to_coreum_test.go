//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"context"
	"crypto/rand"
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

func TestSendFromXRPLToCoreumWithManualTrustSet(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplCurrencyIssuerAcc := chains.XRPL.GenAccount(ctx, t, 100)
	t.Logf("XRPL currency issuer account: %s", xrplCurrencyIssuerAcc)

	coreumRecipient := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipient.String())

	sendingPrecision := int32(6)
	maxHoldingAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)

	envCfg := DefaultRunnerEnvConfig()
	// we need it to manually do the TrustSet
	envCfg.DisableMasterKey = false
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	xrplRegisteredCurrency, err := rippledata.NewCurrency("RCP")
	require.NoError(t, err)

	// register token with 20 chars
	currencyHexSymbol := hex.EncodeToString([]byte(strings.Repeat("R", 20)))
	xrplRegisteredHexCurrency, err := rippledata.NewCurrency(currencyHexSymbol)
	require.NoError(t, err)

	xrplNotRegisterCurrency, err := rippledata.NewCurrency("NRG")
	require.NoError(t, err)

	// set trust for all tokens
	SendTrustSet(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, runnerEnv.XRPLBridgeAccount, xrplRegisteredCurrency)
	SendTrustSet(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, runnerEnv.XRPLBridgeAccount, xrplRegisteredHexCurrency)
	SendTrustSet(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, runnerEnv.XRPLBridgeAccount, xrplNotRegisterCurrency)

	// fund owner to cover registration fees
	chains.Coreum.FundAccountWithOptions(ctx, t, runnerEnv.ContractOwner, coreumintegration.BalancesOptions{
		Amount: chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount.MulRaw(2),
	})

	// recover tickets so that we can register tokens
	numberOfTicketsToInit := uint32(200)
	firstBridgeAccountSeqNumber := uint32(1)
	_, err = runnerEnv.ContractClient.RecoverTickets(ctx, runnerEnv.ContractOwner, firstBridgeAccountSeqNumber, &numberOfTicketsToInit)
	require.NoError(t, err)

	// Send evidences from relayers
	acceptedTxHash := "D5F78F452DFFBE239EFF668E4B34B1AF66CD2F4D5C5D9E54A5AF34121B5862C5"
	tickets := make([]uint32, 200)
	for i := range tickets {
		tickets[i] = uint32(i)
	}

	acceptedTxEvidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            acceptedTxHash,
			SequenceNumber:    &firstBridgeAccountSeqNumber,
			TransactionResult: coreum.TransactionResultAccepted,
		},
		Tickets: tickets,
	}

	// send evidences from relayers
	for i := 0; i < runnerEnv.Cfg.SigningThreshold; i++ {
		_, err = runnerEnv.ContractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, runnerEnv.RelayerAddresses[i], acceptedTxEvidence)
		require.NoError(t, err)
	}

	// register XRPL native token with 3 chars
	_, err = runnerEnv.ContractClient.RegisterXRPLToken(ctx, runnerEnv.ContractOwner, xrplCurrencyIssuerAcc.String(), xrpl.ConvertCurrencyToString(xrplRegisteredCurrency), sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)

	// activate the token
	activateToken(ctx, t, runnerEnv, xrplCurrencyIssuerAcc.String(), xrpl.ConvertCurrencyToString(xrplRegisteredCurrency))

	// register XRPL native token with 20 chars
	_, err = runnerEnv.ContractClient.RegisterXRPLToken(ctx, runnerEnv.ContractOwner, xrplCurrencyIssuerAcc.String(), xrpl.ConvertCurrencyToString(xrplRegisteredHexCurrency), sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)

	// activate the token
	activateToken(ctx, t, runnerEnv, xrplCurrencyIssuerAcc.String(), xrpl.ConvertCurrencyToString(xrplRegisteredHexCurrency))

	registeredXRPLTokens, err := runnerEnv.ContractClient.GetXRPLTokens(ctx)
	require.NoError(t, err)
	// take the tokens with the generated denom
	var (
		registeredXRPLToken            coreum.XRPLToken
		registeredXRPLTokenHexCurrency coreum.XRPLToken
	)
	for _, token := range registeredXRPLTokens {
		if token.Issuer == xrplCurrencyIssuerAcc.String() && token.Currency == xrplRegisteredCurrency.String() {
			registeredXRPLToken = token
			continue
		}
		if token.Issuer == xrplCurrencyIssuerAcc.String() && token.Currency == xrpl.ConvertCurrencyToString(xrplRegisteredHexCurrency) {
			registeredXRPLTokenHexCurrency = token
			continue
		}
	}
	require.NotEmpty(t, registeredXRPLToken.CoreumDenom)
	require.NotEmpty(t, registeredXRPLTokenHexCurrency.CoreumDenom)

	runnerEnv.StartAllRunnerProcesses(ctx, t)

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

	// send incorrect transactions

	// currency is not registered
	xrplNotRegisterCurrencyAmount := rippledata.Amount{
		Value:    lowValue,
		Currency: xrplNotRegisterCurrency,
		Issuer:   xrplCurrencyIssuerAcc,
	}
	SendXRPLPaymentTx(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, runnerEnv.XRPLBridgeAccount, xrplNotRegisterCurrencyAmount, memo)

	// incorrect memo
	SendXRPLPaymentTx(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, runnerEnv.XRPLBridgeAccount, maxDecimalsRegisterCurrencyAmount, rippledata.Memo{})

	// send correct transactions

	// send tx with partial payment
	SendXRPLPartialPaymentTx(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, runnerEnv.XRPLBridgeAccount, highValueRegisteredCurrencyAmount, maxDecimalsRegisterCurrencyAmount, memo)

	// send tx with high amount
	SendXRPLPaymentTx(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, runnerEnv.XRPLBridgeAccount, highValueRegisteredCurrencyAmount, memo)

	// send tx with hex currency
	SendXRPLPaymentTx(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, runnerEnv.XRPLBridgeAccount, registeredHexCurrencyAmount, memo)

	runnerEnv.AwaitCoreumBalance(ctx, t, chains.Coreum, coreumRecipient, sdk.NewCoin(registeredXRPLToken.CoreumDenom, integrationtests.ConvertStringWithDecimalsToSDKInt(t, "100001.000001", XRPLTokenDecimals)))
	runnerEnv.AwaitCoreumBalance(ctx, t, chains.Coreum, coreumRecipient, sdk.NewCoin(registeredXRPLTokenHexCurrency.CoreumDenom, integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9.9", XRPLTokenDecimals)))
}

// TODO(dzmitryhil) remove the manual activation and use automatic VIA relayer.
func activateToken(ctx context.Context, t *testing.T, runnerEnv *RunnerEnv, issuer, currency string) {
	t.Helper()

	pendingOperations, err := runnerEnv.ContractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	ticketAllocated := pendingOperations[0].TicketNumber

	acceptedTxHashTrustSet, err := randomTxHash(40)
	require.NoError(t, err)
	acceptedTxEvidenceTrustSet := coreum.XRPLTransactionResultTrustSetEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            acceptedTxHashTrustSet,
			TicketNumber:      &ticketAllocated,
			TransactionResult: coreum.TransactionResultAccepted,
		},
		Issuer:   issuer,
		Currency: currency,
	}

	// send evidences from relayers
	for i := 0; i < runnerEnv.Cfg.SigningThreshold; i++ {
		_, err = runnerEnv.ContractClient.SendXRPLTrustSetTransactionResultEvidence(ctx, runnerEnv.RelayerAddresses[i], acceptedTxEvidenceTrustSet)
		require.NoError(t, err)
	}
}

func randomTxHash(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
