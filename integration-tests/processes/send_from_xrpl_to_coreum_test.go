//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
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

	sendingPrecision := uint32(15)
	maxHoldingAmount := "1000000000000000000000000000000"

	envCfg := DefaultRunnerEnvConfig()
	// we need it to manually do the TrustSet
	envCfg.DisableMasterKey = false
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	xrplRegisteredCurrency, err := rippledata.NewCurrency("RCP")
	require.NoError(t, err)

	// register token with 20 chars
	hexSymbol := hex.EncodeToString([]byte(strings.Repeat("R", 20)))
	xrplRegisteredHexCurrency, err := rippledata.NewCurrency(hexSymbol)
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
	// register XRPL native token with 3 chars
	_, err = runnerEnv.ContractClient.RegisterXRPLToken(ctx, runnerEnv.ContractOwner, xrplCurrencyIssuerAcc.String(), xrplRegisteredCurrency.String(), sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)
	// register XRPL native token with 20 chars
	_, err = runnerEnv.ContractClient.RegisterXRPLToken(ctx, runnerEnv.ContractOwner, xrplCurrencyIssuerAcc.String(), xrplRegisteredHexCurrency.String(), sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)

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
		if token.Issuer == xrplCurrencyIssuerAcc.String() && token.Currency == xrplRegisteredHexCurrency.String() {
			registeredXRPLTokenHexCurrency = token
			continue
		}
	}
	require.NotEmpty(t, registeredXRPLToken.CoreumDenom)
	require.NotEmpty(t, registeredXRPLTokenHexCurrency.CoreumDenom)

	runnerEnv.StartAllRunnerProcesses(ctx, t)

	maxDecimalsValue, err := rippledata.NewValue("1.000000000000001", false)
	require.NoError(t, err)
	maxDecimalsRegisterCurrencyAmount := rippledata.Amount{
		Value:    maxDecimalsValue,
		Currency: xrplRegisteredCurrency,
		Issuer:   xrplCurrencyIssuerAcc,
	}

	highValue, err := rippledata.NewValue("10000000000.0", false)
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
		Value:    maxDecimalsValue,
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

	require.NoError(t, chains.Coreum.AwaitForBalance(ctx, t, coreumRecipient, sdk.NewCoin(registeredXRPLToken.CoreumDenom, convertStringToSDKInt(t, "10000000001000000000000001"))))
	require.NoError(t, chains.Coreum.AwaitForBalance(ctx, t, coreumRecipient, sdk.NewCoin(registeredXRPLTokenHexCurrency.CoreumDenom, convertStringToSDKInt(t, "9900000000000000"))))
}

func convertStringToSDKInt(t *testing.T, invVal string) sdkmath.Int {
	t.Helper()

	expectedBigIntAmount, ok := big.NewInt(0).SetString(invVal, 0)
	require.True(t, ok)
	return sdkmath.NewIntFromBigInt(expectedBigIntAmount)
}
