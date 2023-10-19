//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"context"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v3/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestSendFromXRPLToCoreum(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplBridgeAcc := chains.XRPL.GenAccount(ctx, t, 100)
	t.Logf("XRPL bridge account: %s", xrplBridgeAcc)

	xrplCurrencyIssuerAcc := chains.XRPL.GenAccount(ctx, t, 100)
	t.Logf("XRPL currency issuer account: %s", xrplCurrencyIssuerAcc)

	coreumRecipient := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipient.String())

	relayer1, relayer1KeyName := chains.Coreum.GenAccountWithKeyName()
	chains.Coreum.FundAccountWithOptions(ctx, t, relayer1, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})
	t.Logf("Relayer1: %s", relayer1.String())

	relayer2, relayer2KeyName := chains.Coreum.GenAccountWithKeyName()
	chains.Coreum.FundAccountWithOptions(ctx, t, relayer2, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})
	t.Logf("Relayer2: %s", relayer2.String())

	relayer3, relayer3KeyName := chains.Coreum.GenAccountWithKeyName()
	chains.Coreum.FundAccountWithOptions(ctx, t, relayer3, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})
	t.Logf("Relaye3: %s", relayer3.String())

	xrplRegisteredCurrency, err := rippledata.NewCurrency("RCP")
	require.NoError(t, err)

	// register token with 20 chars
	hexSymbol := hex.EncodeToString([]byte(strings.Repeat("R", 20)))
	xrplRegisteredHexCurrency, err := rippledata.NewCurrency(hexSymbol)
	require.NoError(t, err)

	xrplNotRegisterCurrency, err := rippledata.NewCurrency("NRG")
	require.NoError(t, err)

	// set trust for both tokens
	sendTrustSet(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, xrplBridgeAcc, xrplRegisteredCurrency)
	sendTrustSet(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, xrplBridgeAcc, xrplRegisteredHexCurrency)
	sendTrustSet(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, xrplBridgeAcc, xrplNotRegisterCurrency)

	relayers := []coreum.Relayer{
		coreum.Relayer{
			CoreumAddress: relayer1,
			XRPLAddress:   "xrpl_address",
			XRPLPubKey:    "xrpl_pub_key",
		},
		coreum.Relayer{
			CoreumAddress: relayer2,
			XRPLAddress:   "xrpl_address",
			XRPLPubKey:    "xrpl_pub_key",
		},
		coreum.Relayer{
			CoreumAddress: relayer3,
			XRPLAddress:   "xrpl_address",
			XRPLPubKey:    "xrpl_pub_key",
		},
	}

	sendingPrecision := uint32(15)
	maxHoldingAmount := "10000"

	// deploy contract
	contractOwner, contractClient := integrationtests.DeployAndInstantiateContract(ctx, t, chains, relayers, 2, 10)
	// fund owner to cover registration fees
	chains.Coreum.FundAccountWithOptions(ctx, t, contractOwner, coreumintegration.BalancesOptions{
		Amount: chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount.MulRaw(2),
	})
	// register XRPL native token with 3 chars
	_, err = contractClient.RegisterXRPLToken(ctx, contractOwner, xrplCurrencyIssuerAcc.String(), xrplRegisteredCurrency.String(), sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)
	// register XRPL native token with 20 chars
	_, err = contractClient.RegisterXRPLToken(ctx, contractOwner, xrplCurrencyIssuerAcc.String(), hexSymbol, sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)

	registeredXRPLTokens, err := contractClient.GetXRPLTokens(ctx)
	require.NoError(t, err)
	// take the token with the generated denom
	var (
		registeredXRPLToken            coreum.XRPLToken
		registeredXRPLTokenHexCurrency coreum.XRPLToken
	)
	for _, token := range registeredXRPLTokens {
		if token.Issuer == xrplCurrencyIssuerAcc.String() && token.Currency == xrplRegisteredCurrency.String() {
			registeredXRPLToken = token
			continue
		}
		if token.Issuer == xrplCurrencyIssuerAcc.String() && token.Currency == hexSymbol {
			registeredXRPLTokenHexCurrency = token
			continue
		}
	}
	require.NotEmpty(t, registeredXRPLToken.CoreumDenom)
	require.NotEmpty(t, registeredXRPLTokenHexCurrency.CoreumDenom)

	// start relayers
	contractAddress := contractClient.GetContractAddress()
	relayerRunners := []*runner.Runner{
		createDevRunner(t, xrplBridgeAcc, contractAddress, relayer1KeyName, chains),
		createDevRunner(t, xrplBridgeAcc, contractAddress, relayer2KeyName, chains),
		createDevRunner(t, xrplBridgeAcc, contractAddress, relayer3KeyName, chains),
	}
	for _, relayerRunner := range relayerRunners {
		go func(relayerRunner *runner.Runner) {
			require.NoError(t, relayerRunner.Processor.StartProcesses(ctx, relayerRunner.Processes.XRPLObserver))
		}(relayerRunner)
	}

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
	sendXRPLPaymentTx(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, xrplBridgeAcc, xrplNotRegisterCurrencyAmount, memo)

	// incorrect memo
	sendXRPLPaymentTx(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, xrplBridgeAcc, maxDecimalsRegisterCurrencyAmount, rippledata.Memo{})

	// send correct transactions

	// send tx with partial payment
	sendXRPLPartialPaymentTx(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, xrplBridgeAcc, highValueRegisteredCurrencyAmount, maxDecimalsRegisterCurrencyAmount, memo)

	// send tx with high amount
	sendXRPLPaymentTx(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, xrplBridgeAcc, highValueRegisteredCurrencyAmount, memo)

	// send tx with hex currency
	sendXRPLPaymentTx(ctx, t, chains.XRPL, xrplCurrencyIssuerAcc, xrplBridgeAcc, registeredHexCurrencyAmount, memo)

	require.NoError(t, chains.Coreum.AwaitForBalance(ctx, t, coreumRecipient, sdk.NewCoin(registeredXRPLToken.CoreumDenom, convertStringToSDKInt(t, "10000000001000000000000001"))))
	require.NoError(t, chains.Coreum.AwaitForBalance(ctx, t, coreumRecipient, sdk.NewCoin(registeredXRPLTokenHexCurrency.CoreumDenom, convertStringToSDKInt(t, "9900000000000000"))))
}

func sendTrustSet(
	ctx context.Context,
	t *testing.T,
	xrplChain integrationtests.XRPLChain,
	issuer, sender rippledata.Account,
	currency rippledata.Currency,
) {
	trustSetValue, err := rippledata.NewValue("10e20", false)
	require.NoError(t, err)
	senderCurrencyTrustSetTx := rippledata.TrustSet{
		LimitAmount: rippledata.Amount{
			Value:    trustSetValue,
			Currency: currency,
			Issuer:   issuer,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TRUST_SET,
		},
	}
	require.NoError(t, xrplChain.AutoFillSignAndSubmitTx(ctx, t, &senderCurrencyTrustSetTx, sender))
}

func sendXRPLPaymentTx(
	ctx context.Context,
	t *testing.T,
	xrplChain integrationtests.XRPLChain,
	senderAcc, recipientAcc rippledata.Account,
	amount rippledata.Amount,
	memo rippledata.Memo,
) {
	xrpPaymentTx := rippledata.Payment{
		Destination: recipientAcc,
		Amount:      amount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
			Memos: rippledata.Memos{
				memo,
			},
		},
	}
	require.NoError(t, xrplChain.AutoFillSignAndSubmitTx(ctx, t, &xrpPaymentTx, senderAcc))
}

func sendXRPLPartialPaymentTx(
	ctx context.Context,
	t *testing.T,
	xrplChain integrationtests.XRPLChain,
	senderAcc, recipientAcc rippledata.Account,
	amount rippledata.Amount,
	maxAmount rippledata.Amount,
	memo rippledata.Memo,
) {
	xrpPaymentTx := rippledata.Payment{
		Destination: recipientAcc,
		Amount:      amount,
		SendMax:     &maxAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
			Memos: rippledata.Memos{
				memo,
			},
			Flags: lo.ToPtr(rippledata.TxPartialPayment),
		},
	}
	require.NoError(t, xrplChain.AutoFillSignAndSubmitTx(ctx, t, &xrpPaymentTx, senderAcc))
}

func createDevRunner(
	t *testing.T,
	bridgeAcc rippledata.Account,
	contractAddress sdk.AccAddress,
	relayerKeyName string,
	chains integrationtests.Chains,
) *runner.Runner {
	t.Helper()

	relayerRunnerCfg := runner.DefaultConfig()

	relayerRunnerCfg.LoggingConfig.Level = "debug"

	relayerRunnerCfg.XRPL.BridgeAccount = bridgeAcc.String()
	relayerRunnerCfg.XRPL.RPC.URL = chains.XRPL.Config().RPCAddress
	// make the scanner fast
	relayerRunnerCfg.XRPL.Scanner.RetryDelay = 500 * time.Millisecond

	relayerRunnerCfg.Coreum.GRPC.URL = chains.Coreum.Config().GRPCAddress
	relayerRunnerCfg.Coreum.RelayerKeyName = relayerKeyName
	relayerRunnerCfg.Coreum.Contract.ContractAddress = contractAddress.String()
	// We use high gas adjustment since our relayers might send transactions in one block.
	// They estimate gas based on the same state, but since transactions are executed one by one the next transaction uses
	// the state different from the one it used for the estimation as a result the out-of-gas error might appear.
	relayerRunnerCfg.Coreum.Contract.GasAdjustment = 2
	relayerRunnerCfg.Coreum.Network.ChainID = chains.Coreum.ChainSettings.ChainID

	relayerRunner, err := runner.NewRunner(relayerRunnerCfg, chains.Coreum.ClientContext.Keyring())
	require.NoError(t, err)
	return relayerRunner
}

func convertStringToSDKInt(t *testing.T, invVal string) sdkmath.Int {
	t.Helper()

	expectedBigIntAmount, ok := big.NewInt(0).SetString(invVal, 0)
	require.True(t, ok)
	return sdkmath.NewIntFromBigInt(expectedBigIntAmount)
}
