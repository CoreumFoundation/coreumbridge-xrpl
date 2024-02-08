//go:build integrationtests
// +build integrationtests

package contract_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum/v4/testutil/event"
	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	assetfttypes "github.com/CoreumFoundation/coreum/v4/x/asset/ft/types"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

var (
	defaultTrustSetLimitAmount = sdkmath.NewIntWithDecimal(1, 16)

	allTokenStates = []coreum.TokenState{
		coreum.TokenStateEnabled,
		coreum.TokenStateDisabled,
		coreum.TokenStateProcessing,
		coreum.TokenStateInactive,
	}

	changeableTokenStates = []coreum.TokenState{
		coreum.TokenStateEnabled,
		coreum.TokenStateDisabled,
	}

	unchangeableTokenStates = []coreum.TokenState{
		coreum.TokenStateProcessing,
		coreum.TokenStateInactive,
	}
)

func TestRegisterAndUpdateCoreumToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	relayerXRPLSigner := xrpl.GenPrivKeyTxSigner()
	relayers := []coreum.Relayer{
		{
			CoreumAddress: chains.Coreum.GenAccount(),
			XRPLAddress:   relayerXRPLSigner.Account().String(),
			XRPLPubKey:    relayerXRPLSigner.PubKey().String(),
		},
	}

	notOwner := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, notOwner, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)

	denom1 := "denom1"
	denom1Decimals := uint32(17)
	sendingPrecision := int32(15)
	maxHoldingAmount := sdkmath.NewInt(10000)
	bridgingFee := sdkmath.ZeroInt()

	// try to register from not owner
	_, err := contractClient.RegisterCoreumToken(
		ctx,
		notOwner,
		denom1,
		denom1Decimals,
		sendingPrecision,
		maxHoldingAmount,
		sdk.ZeroInt(),
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// register from the owner
	_, err = contractClient.RegisterCoreumToken(
		ctx,
		owner,
		denom1,
		denom1Decimals,
		sendingPrecision,
		maxHoldingAmount,
		sdk.ZeroInt(),
	)
	require.NoError(t, err)

	// try to register the same denom one more time
	_, err = contractClient.RegisterCoreumToken(
		ctx, owner, denom1, denom1Decimals, sendingPrecision, maxHoldingAmount, bridgingFee,
	)
	require.True(t, coreum.IsCoreumTokenAlreadyRegisteredError(err), err)

	registeredToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom1)
	require.NoError(t, err)
	require.Equal(t, coreum.CoreumToken{
		Denom:            denom1,
		Decimals:         denom1Decimals,
		XRPLCurrency:     registeredToken.XRPLCurrency,
		SendingPrecision: sendingPrecision,
		MaxHoldingAmount: maxHoldingAmount,
		State:            coreum.TokenStateEnabled,
		BridgingFee:      bridgingFee,
	}, registeredToken)

	// try to use the registered denom with new XRPL currency on the XRPL chain
	issuerAcc := chains.XRPL.GenAccount(ctx, t, 10)
	recipientAcc := chains.XRPL.GenAccount(ctx, t, 10)

	// allow to receive the currency
	amountToSend, err := rippledata.NewValue("10000000000000000", false)
	require.NoError(t, err)
	currency, err := rippledata.NewCurrency(registeredToken.XRPLCurrency)
	require.NoError(t, err)
	trustSetTx := rippledata.TrustSet{
		LimitAmount: rippledata.Amount{
			Value:    amountToSend,
			Currency: currency,
			Issuer:   issuerAcc,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TRUST_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &trustSetTx, recipientAcc))

	paymentTx := rippledata.Payment{
		Destination: recipientAcc,
		Amount: rippledata.Amount{
			Value:    amountToSend,
			Currency: currency,
			Issuer:   issuerAcc,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}

	balancesBeforeSend := chains.XRPL.GetAccountBalances(ctx, t, recipientAcc)
	t.Logf("Recipient account balances before send: %s", balancesBeforeSend)
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &paymentTx, issuerAcc))
	balancesAfterSend := chains.XRPL.GetAccountBalances(ctx, t, recipientAcc)
	t.Logf("Recipient account balances after send: %s", balancesAfterSend)
	receiveAmount, ok := balancesAfterSend[fmt.Sprintf("%s/%s",
		xrpl.ConvertCurrencyToString(currency), issuerAcc.String(),
	)]
	require.True(t, ok)
	require.Equal(t, amountToSend.String(), receiveAmount.Value.String())

	// update

	newSendingPrecision := int32(12)
	newMaxHoldingAmount := sdkmath.NewInt(10101)
	newBridgingFee := sdkmath.NewInt(77)
	_, err = contractClient.UpdateCoreumToken(
		ctx,
		owner,
		registeredToken.Denom,
		lo.ToPtr(coreum.TokenStateDisabled),
		&newSendingPrecision,
		&newMaxHoldingAmount,
		&newBridgingFee,
	)
	require.NoError(t, err)

	registeredToken, err = contractClient.GetCoreumTokenByDenom(ctx, denom1)
	require.NoError(t, err)
	require.Equal(t, coreum.CoreumToken{
		Denom:            denom1,
		Decimals:         denom1Decimals,
		XRPLCurrency:     registeredToken.XRPLCurrency,
		SendingPrecision: newSendingPrecision,
		MaxHoldingAmount: newMaxHoldingAmount,
		State:            coreum.TokenStateDisabled,
		BridgingFee:      newBridgingFee,
	}, registeredToken)
}

func TestRegisterAndUpdateXRPLToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	assetftClient := assetfttypes.NewQueryClient(chains.Coreum.ClientContext)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 2)
	coreumRecipient := chains.Coreum.GenAccount()

	notOwner := chains.Coreum.GenAccount()

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	// fund with issuance fee and some coins on to cover fees
	chains.Coreum.FundAccountWithOptions(ctx, t, notOwner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.AddRaw(1_000_000),
	})

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)

	// fund owner to cover issuance fees twice
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Mul(sdkmath.NewIntFromUint64(2)),
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	inactiveCurrency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))
	activeCurrency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))
	sendingPrecision := int32(15)
	maxHoldingAmount := sdkmath.NewInt(10000)
	bridgingFee := sdkmath.ZeroInt()

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	// try to register from not owner
	_, err := contractClient.RegisterXRPLToken(
		ctx, notOwner, issuer, inactiveCurrency, sendingPrecision, maxHoldingAmount, bridgingFee,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// register from the owner
	_, err = contractClient.RegisterXRPLToken(
		ctx, owner, issuer, inactiveCurrency, sendingPrecision, maxHoldingAmount, bridgingFee,
	)
	require.NoError(t, err)

	// try to register the same denom one more time
	_, err = contractClient.RegisterXRPLToken(
		ctx, owner, issuer, inactiveCurrency, sendingPrecision, maxHoldingAmount, bridgingFee,
	)
	require.True(t, coreum.IsXRPLTokenAlreadyRegisteredError(err), err)

	xrplTokens, err := contractClient.GetXRPLTokens(ctx)
	require.NoError(t, err)
	// one XRP token and registered
	require.Len(t, xrplTokens, 2)

	registeredInactiveToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, inactiveCurrency)
	require.NoError(t, err)

	require.Equal(t, coreum.XRPLToken{
		Issuer:           issuer,
		Currency:         inactiveCurrency,
		CoreumDenom:      registeredInactiveToken.CoreumDenom,
		SendingPrecision: sendingPrecision,
		MaxHoldingAmount: maxHoldingAmount,
		State:            coreum.TokenStateProcessing,
		BridgingFee:      bridgingFee,
	}, registeredInactiveToken)

	// check that corresponding token is issued
	contractAddress := contractClient.GetContractAddress()

	tokenRes, err := assetftClient.Token(ctx, &assetfttypes.QueryTokenRequest{
		Denom: registeredInactiveToken.CoreumDenom,
	})
	require.NoError(t, err)

	// deconstruct the denom to get prefix used for the symbol and subunit
	prefix, _, err := assetfttypes.DeconstructDenom(registeredInactiveToken.CoreumDenom)
	require.NoError(t, err)

	require.Equal(t, assetfttypes.Token{
		Denom:          registeredInactiveToken.CoreumDenom,
		Issuer:         contractAddress.String(),
		Symbol:         strings.ToUpper(prefix),
		Subunit:        prefix,
		Precision:      15,
		Description:    "",
		GloballyFrozen: false,
		Features: []assetfttypes.Feature{
			assetfttypes.Feature_minting,
			assetfttypes.Feature_burning,
			assetfttypes.Feature_ibc,
		},
		BurnRate:           sdk.ZeroDec(),
		SendCommissionRate: sdk.ZeroDec(),
		Version:            assetfttypes.CurrentTokenVersion,
	}, tokenRes.Token)

	// go through the trust set evidence use cases

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	require.NotNil(t, operation.OperationType.TrustSet)

	rejectedTxEvidenceTrustSet := coreum.XRPLTransactionResultTrustSetEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultRejected,
		},
	}

	// try to register not existing operation
	invalidEvidenceTrustSetWithInvalidTicket := rejectedTxEvidenceTrustSet
	invalidEvidenceTrustSetWithInvalidTicket.TicketSequence = lo.ToPtr(uint32(99))
	_, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(
		ctx,
		relayers[0].CoreumAddress,
		invalidEvidenceTrustSetWithInvalidTicket,
	)
	require.True(t, coreum.IsPendingOperationNotFoundError(err), err)

	// send valid rejected evidence from first relayer
	txResTrustSet, err := contractClient.SendXRPLTrustSetTransactionResultEvidence(
		ctx,
		relayers[0].CoreumAddress,
		rejectedTxEvidenceTrustSet,
	)
	require.NoError(t, err)
	thresholdReachedTrustSet, err := event.FindStringEventAttribute(
		txResTrustSet.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(false), thresholdReachedTrustSet)
	// send valid rejected evidence from second relayer
	txResTrustSet, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(
		ctx, relayers[1].CoreumAddress, rejectedTxEvidenceTrustSet,
	)
	require.NoError(t, err)
	thresholdReachedTrustSet, err = event.FindStringEventAttribute(
		txResTrustSet.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(true), thresholdReachedTrustSet)

	registeredInactiveToken, err = contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, inactiveCurrency)
	require.NoError(t, err)

	require.Equal(t, coreum.XRPLToken{
		Issuer:           issuer,
		Currency:         inactiveCurrency,
		CoreumDenom:      registeredInactiveToken.CoreumDenom,
		SendingPrecision: sendingPrecision,
		MaxHoldingAmount: maxHoldingAmount,
		State:            coreum.TokenStateInactive,
		BridgingFee:      bridgingFee,
	}, registeredInactiveToken)

	// try to send evidence one more time
	_, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(
		ctx,
		relayers[1].CoreumAddress,
		rejectedTxEvidenceTrustSet,
	)
	require.True(t, coreum.IsOperationAlreadyExecutedError(err), err)

	// try to register the sending from the XRPL to coreum evidence with inactive token
	xrplToCoreumInactiveTokenTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    issuerAcc.String(),
		Currency:  inactiveCurrency,
		Amount:    sdkmath.NewInt(10),
		Recipient: coreumRecipient,
	}
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[1].CoreumAddress,
		xrplToCoreumInactiveTokenTransferEvidence,
	)
	require.True(t, coreum.IsTokenNotEnabledError(err), err)

	// register one more token and activate it
	_, err = contractClient.RegisterXRPLToken(
		ctx, owner, issuer, activeCurrency, sendingPrecision, maxHoldingAmount, bridgingFee,
	)
	require.NoError(t, err)

	registeredActiveToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, activeCurrency)
	require.NoError(t, err)

	require.Equal(t, coreum.XRPLToken{
		Issuer:           issuer,
		Currency:         activeCurrency,
		CoreumDenom:      registeredActiveToken.CoreumDenom,
		SendingPrecision: sendingPrecision,
		MaxHoldingAmount: maxHoldingAmount,
		State:            coreum.TokenStateProcessing,
		BridgingFee:      bridgingFee,
	}, registeredActiveToken)

	activateXRPLToken(ctx, t, contractClient, relayers, issuer, activeCurrency)

	amountToSend := sdkmath.NewInt(99)
	sendFromXRPLToCoreum(ctx, t, contractClient, relayers, issuer, activeCurrency, amountToSend, coreumRecipient)

	balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredActiveToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(t, amountToSend.String(), balanceRes.Balance.Amount.String())

	// update token

	newSendingPrecision := int32(12)
	newMaxHoldingAmount := sdkmath.NewInt(10101)
	newBridgingFee := sdkmath.NewInt(77)
	_, err = contractClient.UpdateXRPLToken(
		ctx,
		owner,
		issuer,
		activeCurrency,
		lo.ToPtr(coreum.TokenStateDisabled),
		&newSendingPrecision,
		&newMaxHoldingAmount,
		&newBridgingFee,
	)
	require.NoError(t, err)

	registeredActiveToken, err = contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, activeCurrency)
	require.NoError(t, err)
	require.Equal(t, coreum.XRPLToken{
		Issuer:           issuer,
		Currency:         activeCurrency,
		CoreumDenom:      registeredActiveToken.CoreumDenom,
		SendingPrecision: newSendingPrecision,
		MaxHoldingAmount: newMaxHoldingAmount,
		State:            coreum.TokenStateDisabled,
		BridgingFee:      newBridgingFee,
	}, registeredActiveToken)
}

func TestRecoverXRPLTokeRegistration(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 2)

	notOwner := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, notOwner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.AddRaw(1_000_000),
	})

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)

	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Mul(sdkmath.NewIntFromUint64(1)),
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))
	sendingPrecision := int32(15)
	maxHoldingAmount := sdkmath.NewInt(10000)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	// register from the owner
	_, err := contractClient.RegisterXRPLToken(
		ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)

	registeredXRPLToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)

	require.Equal(t, coreum.XRPLToken{
		Issuer:           issuer,
		Currency:         currency,
		CoreumDenom:      registeredXRPLToken.CoreumDenom,
		SendingPrecision: sendingPrecision,
		MaxHoldingAmount: maxHoldingAmount,
		State:            coreum.TokenStateProcessing,
		BridgingFee:      sdkmath.ZeroInt(),
	}, registeredXRPLToken)

	// try to recover the token with the unexpected current state
	_, err = contractClient.RecoverXRPLTokenRegistration(ctx, owner, issuer, currency)
	require.True(t, coreum.IsXRPLTokenNotInactiveError(err), err)

	// reject token trust set to be able to recover
	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	require.NotNil(t, operation.OperationType.TrustSet)

	require.Equal(t, coreum.OperationTypeTrustSet{
		Issuer:              issuer,
		Currency:            currency,
		TrustSetLimitAmount: defaultTrustSetLimitAmount,
	}, *operation.OperationType.TrustSet)

	rejectedTxEvidenceTrustSet := coreum.XRPLTransactionResultTrustSetEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultRejected,
		},
	}

	// send from first relayer
	_, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(
		ctx,
		relayers[0].CoreumAddress,
		rejectedTxEvidenceTrustSet,
	)
	require.NoError(t, err)
	// send from second relayer
	_, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(
		ctx,
		relayers[1].CoreumAddress,
		rejectedTxEvidenceTrustSet,
	)
	require.NoError(t, err)

	// check that we don't have pending operations anymore
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// fetch token to validate status
	registeredXRPLToken, err = contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	require.Equal(t, coreum.TokenStateInactive, registeredXRPLToken.State)

	// try to recover from now owner
	_, err = contractClient.RecoverXRPLTokenRegistration(ctx, notOwner, issuer, currency)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// recover from owner
	_, err = contractClient.RecoverXRPLTokenRegistration(ctx, owner, issuer, currency)
	require.NoError(t, err)

	// fetch token to validate status
	registeredXRPLToken, err = contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	require.Equal(t, coreum.TokenStateProcessing, registeredXRPLToken.State)

	// check that new operation is present here
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation = pendingOperations[0]
	require.NotNil(t, operation.OperationType.TrustSet)

	require.Equal(t, coreum.OperationTypeTrustSet{
		Issuer:              issuer,
		Currency:            currency,
		TrustSetLimitAmount: defaultTrustSetLimitAmount,
	}, *operation.OperationType.TrustSet)

	acceptedTxEvidenceTrustSet := coreum.XRPLTransactionResultTrustSetEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}

	// send from first relayer
	_, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(
		ctx,
		relayers[0].CoreumAddress,
		acceptedTxEvidenceTrustSet,
	)
	require.NoError(t, err)
	// send from second relayer
	_, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(
		ctx,
		relayers[1].CoreumAddress,
		acceptedTxEvidenceTrustSet,
	)
	require.NoError(t, err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// fetch token to validate status
	registeredXRPLToken, err = contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	require.Equal(t, coreum.TokenStateEnabled, registeredXRPLToken.State)
}

func TestEnableAndDisableXRPLOriginatedToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 2)
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	randomCoreumAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, randomCoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	coreumRecipient := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumRecipient, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))
	sendingPrecision := int32(15)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 7)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	// register from the owner
	_, err := contractClient.RegisterXRPLToken(
		ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)

	registeredToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	require.Equal(t, issuer, registeredToken.Issuer)
	require.Equal(t, currency, registeredToken.Currency)
	require.Equal(t, coreum.TokenStateProcessing, registeredToken.State)
	require.Equal(t, sendingPrecision, registeredToken.SendingPrecision)
	require.NotEmpty(t, registeredToken.CoreumDenom)

	// try to change states of inactive token
	for _, state := range allTokenStates {
		_, err = contractClient.UpdateXRPLToken(ctx, owner, issuer, currency, lo.ToPtr(state), nil, nil, nil)
		require.True(t, coreum.IsTokenStateIsImmutableError(err), err)
	}

	// activate token
	activateXRPLToken(ctx, t, contractClient, relayers, issuer, currency)

	// try to change states of enabled token to the unchangeable state
	for _, state := range unchangeableTokenStates {
		_, err = contractClient.UpdateXRPLToken(ctx, owner, issuer, currency, lo.ToPtr(state), nil, nil, nil)
		require.True(t, coreum.IsInvalidTargetTokenStateError(err), err)
	}

	// change states of enabled token to the changeable state
	for _, state := range changeableTokenStates {
		_, err = contractClient.UpdateXRPLToken(ctx, owner, issuer, currency, lo.ToPtr(state), nil, nil, nil)
		require.NoError(t, err)
		registeredToken, err = contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
		require.NoError(t, err)
		require.Equal(t, state, registeredToken.State)
	}

	// try to call from random address
	_, err = contractClient.UpdateXRPLToken(
		ctx, randomCoreumAddress, issuer, currency, lo.ToPtr(coreum.TokenStateDisabled), nil, nil, nil,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try to call from relayer address
	_, err = contractClient.UpdateXRPLToken(
		ctx, relayers[0].CoreumAddress, issuer, currency, lo.ToPtr(coreum.TokenStateDisabled), nil, nil, nil,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// disable token
	_, err = contractClient.UpdateXRPLToken(
		ctx, owner, issuer, currency, lo.ToPtr(coreum.TokenStateDisabled), nil, nil, nil,
	)
	require.NoError(t, err)

	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    issuerAcc.String(),
		Currency:  currency,
		Amount:    sdkmath.NewInt(100),
		Recipient: coreumRecipient,
	}
	// try to use disabled token
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.True(t, coreum.IsTokenNotEnabledError(err), err)

	// enable the token now
	_, err = contractClient.UpdateXRPLToken(
		ctx, owner, issuer, currency, lo.ToPtr(coreum.TokenStateEnabled), nil, nil, nil,
	)
	require.NoError(t, err)

	// call from first relayer one more time
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)

	// disable the token
	_, err = contractClient.UpdateXRPLToken(
		ctx, owner, issuer, currency, lo.ToPtr(coreum.TokenStateDisabled), nil, nil, nil,
	)
	require.NoError(t, err)

	// try to use disabled token form second relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[1].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.True(t, coreum.IsTokenNotEnabledError(err), err)

	// enable the token
	_, err = contractClient.UpdateXRPLToken(
		ctx, owner, issuer, currency, lo.ToPtr(coreum.TokenStateEnabled), nil, nil, nil,
	)
	require.NoError(t, err)

	// complete the transfer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[1].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)

	// expect new token on the recipient balance
	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(t, xrplToCoreumTransferEvidence.Amount.String(), recipientBalanceRes.Balance.Amount.String())

	// disable the token
	_, err = contractClient.UpdateXRPLToken(
		ctx, owner, issuer, currency, lo.ToPtr(coreum.TokenStateDisabled), nil, nil, nil,
	)
	require.NoError(t, err)

	// try to send the token back
	coinToSendBack := *recipientBalanceRes.Balance
	_, err = contractClient.SendToXRPL(ctx, coreumRecipient, xrplRecipientAddress.String(), coinToSendBack, nil)
	require.True(t, coreum.IsTokenNotEnabledError(err), err)

	// enable the token
	_, err = contractClient.UpdateXRPLToken(
		ctx, owner, issuer, currency, lo.ToPtr(coreum.TokenStateEnabled), nil, nil, nil,
	)
	require.NoError(t, err)

	// send the token back
	_, err = contractClient.SendToXRPL(ctx, coreumRecipient, xrplRecipientAddress.String(), coinToSendBack, nil)
	require.NoError(t, err)

	// disable the token to check that relayers can complete the operation even for the disabled token
	_, err = contractClient.UpdateXRPLToken(
		ctx, owner, issuer, currency, lo.ToPtr(coreum.TokenStateDisabled), nil, nil, nil,
	)
	require.NoError(t, err)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]

	// save signature from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SaveSignature(
			ctx, relayer.CoreumAddress, operation.TicketSequence, operation.Version, xrplTxSignature,
		)
		require.NoError(t, err)
	}

	require.NoError(t, err)

	rejectTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultRejected,
		},
	}
	// send evidence from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
			ctx,
			relayer.CoreumAddress,
			rejectTxEvidence,
		)
		require.NoError(t, err)
	}

	// manually claim refunds
	pendingRefunds, err := contractClient.GetPendingRefunds(ctx, coreumRecipient)
	require.NoError(t, err)
	require.Len(t, pendingRefunds, 1)
	require.EqualValues(t, pendingRefunds[0].Coin.String(), coinToSendBack.String())
	_, err = contractClient.ClaimRefund(ctx, coreumRecipient, pendingRefunds[0].ID)
	require.NoError(t, err)

	// check the successful refunding
	recipientBalanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   coinToSendBack.Denom,
	})
	require.NoError(t, err)
	require.Equal(t, coinToSendBack.Amount.String(), recipientBalanceRes.Balance.Amount.String())

	// enable the token
	_, err = contractClient.UpdateXRPLToken(
		ctx, owner, issuer, currency, lo.ToPtr(coreum.TokenStateEnabled), nil, nil, nil,
	)
	require.NoError(t, err)

	// send the token back
	_, err = contractClient.SendToXRPL(ctx, coreumRecipient, xrplRecipientAddress.String(), coinToSendBack, nil)
	require.NoError(t, err)

	// disable the token to check that relayers can complete the operation even for the disabled token
	_, err = contractClient.UpdateXRPLToken(
		ctx, owner, issuer, currency, lo.ToPtr(coreum.TokenStateDisabled), nil, nil, nil,
	)
	require.NoError(t, err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation = pendingOperations[0]

	acceptTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}
	// send evidence from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
			ctx,
			relayer.CoreumAddress,
			acceptTxEvidence,
		)
		require.NoError(t, err)
	}

	// check that token is sent now
	recipientBalanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   coinToSendBack.Denom,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.ZeroInt().String(), recipientBalanceRes.Balance.Amount.String())
}

func TestEnableAndDisableCoreumOriginatedToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)
	coreumRecipientAddress := chains.Coreum.GenAccount()

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.MulRaw(2).Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	randomCoreumAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, randomCoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	relayers := genRelayers(ctx, t, chains, 2)
	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 10)

	// issue asset ft and register it
	sendingPrecision := int32(15)
	tokenDecimals := uint32(15)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 11)
	initialAmount := sdkmath.NewIntWithDecimal(1, 8)
	registeredCoreumOriginatedToken := issueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		contractClient,
		chains.Coreum,
		coreumSenderAddress,
		owner,
		tokenDecimals,
		initialAmount,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)

	// try to change states of enabled token to the unchangeable state
	for _, state := range unchangeableTokenStates {
		_, err := contractClient.UpdateCoreumToken(
			ctx, owner, registeredCoreumOriginatedToken.Denom, lo.ToPtr(state), nil, nil, nil,
		)
		require.True(t, coreum.IsInvalidTargetTokenStateError(err), err)
	}

	// change states of enabled token to the changeable state
	for _, state := range changeableTokenStates {
		_, err := contractClient.UpdateCoreumToken(
			ctx, owner, registeredCoreumOriginatedToken.Denom, lo.ToPtr(state), nil, nil, nil,
		)
		require.NoError(t, err)
		registeredToken, err := contractClient.GetCoreumTokenByDenom(ctx, registeredCoreumOriginatedToken.Denom)
		require.NoError(t, err)
		require.Equal(t, state, registeredToken.State)
	}

	// try to call from random address
	_, err := contractClient.UpdateCoreumToken(
		ctx, randomCoreumAddress, registeredCoreumOriginatedToken.Denom, lo.ToPtr(coreum.TokenStateDisabled), nil, nil, nil,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try to call from relayer address
	_, err = contractClient.UpdateCoreumToken(
		ctx,
		relayers[0].CoreumAddress,
		registeredCoreumOriginatedToken.Denom,
		lo.ToPtr(coreum.TokenStateDisabled),
		nil,
		nil,
		nil,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	_, err = contractClient.UpdateCoreumToken(
		ctx, owner, registeredCoreumOriginatedToken.Denom, lo.ToPtr(coreum.TokenStateDisabled), nil, nil, nil,
	)
	require.NoError(t, err)

	// try to send the disabled token
	coinToSendFromCoreumToXRPL := sdk.NewCoin(registeredCoreumOriginatedToken.Denom, initialAmount)
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		coinToSendFromCoreumToXRPL,
		nil,
	)
	require.True(t, coreum.IsTokenNotEnabledError(err), err)

	// enable token
	_, err = contractClient.UpdateCoreumToken(
		ctx, owner, registeredCoreumOriginatedToken.Denom, lo.ToPtr(coreum.TokenStateEnabled), nil, nil, nil,
	)
	require.NoError(t, err)

	// send token
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		coinToSendFromCoreumToXRPL,
		nil,
	)
	require.NoError(t, err)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]

	// disable the token to check that relayers can complete the operation even for the disabled token
	_, err = contractClient.UpdateCoreumToken(
		ctx, owner, registeredCoreumOriginatedToken.Denom, lo.ToPtr(coreum.TokenStateDisabled), nil, nil, nil,
	)
	require.NoError(t, err)

	// save signature from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SaveSignature(
			ctx, relayer.CoreumAddress, operation.TicketSequence, operation.Version, xrplTxSignature,
		)
		require.NoError(t, err)
	}

	rejectTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultRejected,
		},
	}
	// send evidence from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
			ctx,
			relayer.CoreumAddress,
			rejectTxEvidence,
		)
		require.NoError(t, err)
	}

	// claim refunds
	pendingRefunds, err := contractClient.GetPendingRefunds(ctx, coreumSenderAddress)
	require.NoError(t, err)
	require.Len(t, pendingRefunds, 1)
	require.EqualValues(t, pendingRefunds[0].Coin.String(), coinToSendFromCoreumToXRPL.String())
	_, err = contractClient.ClaimRefund(ctx, coreumSenderAddress, pendingRefunds[0].ID)
	require.NoError(t, err)

	// check the successful refunding
	senderBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredCoreumOriginatedToken.Denom,
	})
	require.NoError(t, err)
	require.Equal(t, coinToSendFromCoreumToXRPL.Amount.String(), senderBalanceRes.Balance.Amount.String())

	// enable the token
	_, err = contractClient.UpdateCoreumToken(
		ctx, owner, registeredCoreumOriginatedToken.Denom, lo.ToPtr(coreum.TokenStateEnabled), nil, nil, nil,
	)
	require.NoError(t, err)

	// send the token one more time
	_, err = contractClient.SendToXRPL(
		ctx, coreumSenderAddress, xrplRecipientAddress.String(), coinToSendFromCoreumToXRPL, nil,
	)
	require.NoError(t, err)

	// disable the token to check that relayers can complete the operation even for the disabled token
	_, err = contractClient.UpdateCoreumToken(
		ctx, owner, registeredCoreumOriginatedToken.Denom, lo.ToPtr(coreum.TokenStateDisabled), nil, nil, nil,
	)
	require.NoError(t, err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation = pendingOperations[0]

	acceptTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}
	// send evidence from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
			ctx,
			relayer.CoreumAddress,
			acceptTxEvidence,
		)
		require.NoError(t, err)
	}

	// check that token is sent now
	senderBalanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredCoreumOriginatedToken.Denom,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.ZeroInt().String(), senderBalanceRes.Balance.Amount.String())

	registeredToken, err := contractClient.GetCoreumTokenByDenom(ctx, registeredCoreumOriginatedToken.Denom)
	require.NoError(t, err)
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    bridgeXRPLAddress,
		Currency:  registeredToken.XRPLCurrency,
		Amount:    sdkmath.NewInt(100),
		Recipient: coreumRecipientAddress,
	}

	// try to use disabled token
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.True(t, coreum.IsTokenNotEnabledError(err), err)

	// enable token and confirm the sending
	_, err = contractClient.UpdateCoreumToken(
		ctx, owner, registeredCoreumOriginatedToken.Denom, lo.ToPtr(coreum.TokenStateEnabled), nil, nil, nil,
	)
	require.NoError(t, err)

	for _, relayer := range relayers {
		_, err = contractClient.SendXRPLToCoreumTransferEvidence(
			ctx,
			relayer.CoreumAddress,
			xrplToCoreumTransferEvidence,
		)
		require.NoError(t, err)
	}

	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipientAddress.String(),
		Denom:   registeredCoreumOriginatedToken.Denom,
	})
	require.NoError(t, err)
	require.Equal(t, xrplToCoreumTransferEvidence.Amount.String(), recipientBalanceRes.Balance.Amount.String())
}

func TestUpdateXRPLOriginatedTokenSendingPrecision(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 2)
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	randomCoreumAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, randomCoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	coreumRecipient := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumRecipient, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))
	sendingPrecision := int32(15)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 7)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	// register from the owner
	_, err := contractClient.RegisterXRPLToken(
		ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount, sdk.ZeroInt(),
	)
	require.NoError(t, err)
	registeredToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	require.Equal(t, sendingPrecision, registeredToken.SendingPrecision)

	// activate token
	activateXRPLToken(ctx, t, contractClient, relayers, issuer, currency)

	newSendingPrecision := int32(14)

	// try to call from random address
	_, err = contractClient.UpdateXRPLToken(
		ctx, randomCoreumAddress, issuer, currency, nil, &newSendingPrecision, nil, nil,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try to call from relayer address
	_, err = contractClient.UpdateXRPLToken(
		ctx, relayers[0].CoreumAddress, issuer, currency, nil, &newSendingPrecision, nil, nil,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// send evidence with the prev precision
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    issuerAcc.String(),
		Currency:  currency,
		Amount:    sdkmath.NewInt(111),
		Recipient: coreumRecipient,
	}
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)

	// update sending precision
	_, err = contractClient.UpdateXRPLToken(ctx, owner, issuer, currency, nil, &newSendingPrecision, nil, nil)
	require.NoError(t, err)

	registeredToken, err = contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	require.Equal(t, newSendingPrecision, registeredToken.SendingPrecision)

	// call from second relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[1].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)

	// expect new token on the recipient balance
	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredToken.CoreumDenom,
	})
	require.NoError(t, err)
	// the amount is truncated with the new precision
	require.Equal(t, sdk.NewInt(110).String(), recipientBalanceRes.Balance.Amount.String())

	// send the token back
	coinToSendBack := sdk.NewCoin(registeredToken.CoreumDenom, sdk.NewInt(101))
	_, err = contractClient.SendToXRPL(ctx, coreumRecipient, xrplRecipientAddress.String(), coinToSendBack, nil)
	require.NoError(t, err)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	// check that operation contains the amount truncated by new precision
	require.Equal(t, sdkmath.NewInt(100).String(), operationType.Amount.String())
}

func TestUpdateCoreumOriginatedTokenSendingPrecision(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)
	coreumRecipientAddress := chains.Coreum.GenAccount()

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	randomCoreumAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, randomCoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	relayers := genRelayers(ctx, t, chains, 2)
	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 10)

	// issue asset ft and register it
	sendingPrecision := int32(15)
	tokenDecimals := uint32(15)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 11)
	initialAmount := sdkmath.NewIntWithDecimal(1, 8)
	registeredCoreumOriginatedToken := issueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		contractClient,
		chains.Coreum,
		coreumSenderAddress,
		owner,
		tokenDecimals,
		initialAmount,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)
	require.Equal(t, sendingPrecision, registeredCoreumOriginatedToken.SendingPrecision)

	newSendingPrecision := lo.ToPtr(int32(14))
	// try to call from random address
	_, err := contractClient.UpdateCoreumToken(
		ctx, randomCoreumAddress, registeredCoreumOriginatedToken.Denom, nil, newSendingPrecision, nil, nil,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try to call from relayer address
	_, err = contractClient.UpdateCoreumToken(
		ctx, relayers[0].CoreumAddress, registeredCoreumOriginatedToken.Denom, nil, newSendingPrecision, nil, nil,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	_, err = contractClient.UpdateCoreumToken(
		ctx, owner, registeredCoreumOriginatedToken.Denom, nil, newSendingPrecision, nil, nil,
	)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err = contractClient.GetCoreumTokenByDenom(ctx, registeredCoreumOriginatedToken.Denom)
	require.NoError(t, err)
	require.Equal(t, *newSendingPrecision, registeredCoreumOriginatedToken.SendingPrecision)

	coinToSendFromCoreumToXRPL := sdk.NewCoin(registeredCoreumOriginatedToken.Denom, sdk.NewInt(111))
	// send token
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		coinToSendFromCoreumToXRPL,
		nil,
	)
	require.NoError(t, err)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	// check that operation contains the amount truncated by new precision
	require.Equal(t, sdkmath.NewInt(110).String(), operationType.Amount.String())

	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    bridgeXRPLAddress,
		Currency:  registeredCoreumOriginatedToken.XRPLCurrency,
		Amount:    sdkmath.NewInt(111),
		Recipient: coreumRecipientAddress,
	}

	// call from first relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)

	// update sending precision one more time
	newSendingPrecision = lo.ToPtr(int32(13))
	_, err = contractClient.UpdateCoreumToken(
		ctx, owner, registeredCoreumOriginatedToken.Denom, nil, newSendingPrecision, nil, nil,
	)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err = contractClient.GetCoreumTokenByDenom(ctx, registeredCoreumOriginatedToken.Denom)
	require.NoError(t, err)
	require.Equal(t, *newSendingPrecision, registeredCoreumOriginatedToken.SendingPrecision)

	// call from second relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[1].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)

	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipientAddress.String(),
		Denom:   registeredCoreumOriginatedToken.Denom,
	})
	require.NoError(t, err)
	// truncated amount
	require.Equal(t, sdkmath.NewInt(100).String(), recipientBalanceRes.Balance.Amount.String())
}

func TestUpdateXRPLOriginatedTokenBridgingFee(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 2)
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	randomCoreumAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, randomCoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	coreumRecipient := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumRecipient, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))
	sendingPrecision := int32(15)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 7)
	bridgingFee := sdkmath.NewInt(100)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	// register from the owner
	_, err := contractClient.RegisterXRPLToken(
		ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount, bridgingFee,
	)
	require.NoError(t, err)
	registeredToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	require.Equal(t, bridgingFee, registeredToken.BridgingFee)

	// activate token
	activateXRPLToken(ctx, t, contractClient, relayers, issuer, currency)

	newBridgingFee := sdkmath.NewInt(200)

	// try to call from random address
	_, err = contractClient.UpdateXRPLToken(ctx, randomCoreumAddress, issuer, currency, nil, nil, nil, &newBridgingFee)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try to call from relayer address
	_, err = contractClient.UpdateXRPLToken(
		ctx, relayers[0].CoreumAddress, issuer, currency, nil, nil, nil, &newBridgingFee,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// send evidence with the prev bridging fee
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    issuerAcc.String(),
		Currency:  currency,
		Amount:    sdkmath.NewInt(1001),
		Recipient: coreumRecipient,
	}
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)

	// update bridging fee
	_, err = contractClient.UpdateXRPLToken(ctx, owner, issuer, currency, nil, nil, nil, &newBridgingFee)
	require.NoError(t, err)

	registeredToken, err = contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	require.Equal(t, newBridgingFee.String(), registeredToken.BridgingFee.String())

	// call from second relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[1].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)

	// expect new token on the recipient balance
	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredToken.CoreumDenom,
	})
	require.NoError(t, err)
	// the amount is truncated with the new bridging fee
	require.Equal(t, sdk.NewInt(801).String(), recipientBalanceRes.Balance.Amount.String())

	// send the token back
	coinToSendBack := sdk.NewCoin(registeredToken.CoreumDenom, sdk.NewInt(302))
	_, err = contractClient.SendToXRPL(ctx, coreumRecipient, xrplRecipientAddress.String(), coinToSendBack, nil)
	require.NoError(t, err)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	// check that operation contains the amount truncated by new bridging fee
	require.Equal(t, sdkmath.NewInt(102).String(), operationType.Amount.String())
}

func TestUpdateCoreumOriginatedTokenBridgingFee(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)
	coreumRecipientAddress := chains.Coreum.GenAccount()

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	randomCoreumAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, randomCoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	relayers := genRelayers(ctx, t, chains, 2)
	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 10)

	// issue asset ft and register it
	sendingPrecision := int32(15)
	tokenDecimals := uint32(15)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 11)
	bridgingFee := sdkmath.NewInt(100)
	initialAmount := sdkmath.NewIntWithDecimal(1, 8)
	registeredCoreumOriginatedToken := issueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		contractClient,
		chains.Coreum,
		coreumSenderAddress,
		owner,
		tokenDecimals,
		initialAmount,
		sendingPrecision,
		maxHoldingAmount,
		bridgingFee,
	)
	require.Equal(t, bridgingFee.String(), registeredCoreumOriginatedToken.BridgingFee.String())

	newBridgingFee := sdkmath.NewInt(200)
	// try to call from random address
	_, err := contractClient.UpdateCoreumToken(
		ctx, randomCoreumAddress, registeredCoreumOriginatedToken.Denom, nil, nil, nil, &newBridgingFee,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try to call from relayer address
	_, err = contractClient.UpdateCoreumToken(
		ctx, relayers[0].CoreumAddress, registeredCoreumOriginatedToken.Denom, nil, nil, nil, &newBridgingFee,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	_, err = contractClient.UpdateCoreumToken(
		ctx, owner, registeredCoreumOriginatedToken.Denom, nil, nil, nil, &newBridgingFee,
	)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err = contractClient.GetCoreumTokenByDenom(ctx, registeredCoreumOriginatedToken.Denom)
	require.NoError(t, err)
	require.Equal(t, newBridgingFee.String(), registeredCoreumOriginatedToken.BridgingFee.String())

	coinToSendFromCoreumToXRPL := sdk.NewCoin(registeredCoreumOriginatedToken.Denom, sdk.NewInt(301))
	// send token
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		coinToSendFromCoreumToXRPL,
		nil,
	)
	require.NoError(t, err)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	// check that operation contains the amount truncated by new bridging fee
	require.Equal(t, sdkmath.NewInt(101).String(), operationType.Amount.String())

	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    bridgeXRPLAddress,
		Currency:  registeredCoreumOriginatedToken.XRPLCurrency,
		Amount:    sdkmath.NewInt(505),
		Recipient: coreumRecipientAddress,
	}

	// call from first relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)

	// update bridging fee one more time
	newBridgingFee = sdkmath.NewInt(400)
	_, err = contractClient.UpdateCoreumToken(
		ctx, owner, registeredCoreumOriginatedToken.Denom, nil, nil, nil, &newBridgingFee,
	)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err = contractClient.GetCoreumTokenByDenom(ctx, registeredCoreumOriginatedToken.Denom)
	require.NoError(t, err)
	require.Equal(t, newBridgingFee.String(), registeredCoreumOriginatedToken.BridgingFee.String())

	// call from second relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[1].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)

	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipientAddress.String(),
		Denom:   registeredCoreumOriginatedToken.Denom,
	})
	require.NoError(t, err)
	// truncated amount
	require.Equal(t, sdkmath.NewInt(105).String(), recipientBalanceRes.Balance.Amount.String())
}

func TestUpdateXRPLOriginatedTokenMaxHoldingAmount(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 2)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	randomCoreumAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, randomCoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	coreumRecipient := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumRecipient, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))
	sendingPrecision := int32(15)
	maxHoldingAmount := sdkmath.NewInt(1000)
	bridgingFee := sdkmath.ZeroInt()

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	// register from the owner
	_, err := contractClient.RegisterXRPLToken(
		ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount, bridgingFee,
	)
	require.NoError(t, err)
	registeredToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	require.Equal(t, maxHoldingAmount.String(), registeredToken.MaxHoldingAmount.String())

	// activate token
	activateXRPLToken(ctx, t, contractClient, relayers, issuer, currency)

	// send evidence
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    issuerAcc.String(),
		Currency:  currency,
		Amount:    sdkmath.NewInt(1000),
		Recipient: coreumRecipient,
	}
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)

	newMaxHoldingAmount := sdkmath.NewInt(900)

	// update max holding amount
	_, err = contractClient.UpdateXRPLToken(ctx, owner, issuer, currency, nil, nil, &newMaxHoldingAmount, nil)
	require.NoError(t, err)

	registeredToken, err = contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	require.Equal(t, newMaxHoldingAmount.String(), registeredToken.MaxHoldingAmount.String())

	// call from second relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[1].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.True(t, coreum.IsMaximumBridgedAmountReachedError(err), err)

	newMaxHoldingAmount = sdkmath.NewInt(1100)
	// update max holding amount to all the tx to pass
	_, err = contractClient.UpdateXRPLToken(ctx, owner, issuer, currency, nil, nil, &newMaxHoldingAmount, nil)
	require.NoError(t, err)
	registeredToken, err = contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	require.Equal(t, newMaxHoldingAmount.String(), registeredToken.MaxHoldingAmount.String())

	// call from second relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[1].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)

	// expect new token on the recipient balance
	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredToken.CoreumDenom,
	})
	require.NoError(t, err)

	require.Equal(t, xrplToCoreumTransferEvidence.Amount.String(), recipientBalanceRes.Balance.Amount.String())

	newMaxHoldingAmount = sdkmath.NewInt(900)
	// try update max holding amount with the values less than balance
	_, err = contractClient.UpdateXRPLToken(ctx, owner, issuer, currency, nil, nil, &newMaxHoldingAmount, nil)
	require.True(t, coreum.IsInvalidTargetMaxHoldingAmountError(err), err)
}

func TestUpdateCoreumOriginatedTokenMaxHoldingAmount(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	randomCoreumAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, randomCoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	relayers := genRelayers(ctx, t, chains, 2)
	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 10)

	// issue asset ft and register it
	sendingPrecision := int32(15)
	tokenDecimals := uint32(15)
	maxHoldingAmount := sdkmath.NewInt(1000)
	initialAmount := sdkmath.NewIntWithDecimal(1, 8)
	registeredCoreumOriginatedToken := issueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		contractClient,
		chains.Coreum,
		coreumSenderAddress,
		owner,
		tokenDecimals,
		initialAmount,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)
	require.Equal(t, maxHoldingAmount.String(), registeredCoreumOriginatedToken.MaxHoldingAmount.String())

	newMaxHoldingAmount := sdkmath.NewInt(900)
	_, err := contractClient.UpdateCoreumToken(
		ctx, owner, registeredCoreumOriginatedToken.Denom, nil, nil, &newMaxHoldingAmount, nil,
	)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err = contractClient.GetCoreumTokenByDenom(ctx, registeredCoreumOriginatedToken.Denom)
	require.NoError(t, err)
	require.Equal(t, newMaxHoldingAmount.String(), registeredCoreumOriginatedToken.MaxHoldingAmount.String())

	coinToSendFromCoreumToXRPL := sdk.NewCoin(registeredCoreumOriginatedToken.Denom, sdk.NewInt(901))
	// try to send token with to0 high amount
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		coinToSendFromCoreumToXRPL,
		nil,
	)
	require.True(t, coreum.IsMaximumBridgedAmountReachedError(err), err)

	// send token
	coinToSendFromCoreumToXRPL = sdk.NewCoin(registeredCoreumOriginatedToken.Denom, sdk.NewInt(800))
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		coinToSendFromCoreumToXRPL,
		nil,
	)
	require.NoError(t, err)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	require.Equal(t, coinToSendFromCoreumToXRPL.Amount.String(), operationType.Amount.String())

	newMaxHoldingAmount = sdkmath.NewInt(100)
	// try update max holding amount with the values less than balance
	_, err = contractClient.UpdateCoreumToken(
		ctx, owner, registeredCoreumOriginatedToken.Denom, nil, nil, &newMaxHoldingAmount, nil,
	)
	require.True(t, coreum.IsInvalidTargetMaxHoldingAmountError(err), err)
}
