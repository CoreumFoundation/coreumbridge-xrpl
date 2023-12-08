//go:build integrationtests
// +build integrationtests

package coreum_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmoserrors "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum/v3/pkg/client"
	"github.com/CoreumFoundation/coreum/v3/testutil/event"
	coreumintegration "github.com/CoreumFoundation/coreum/v3/testutil/integration"
	assetfttypes "github.com/CoreumFoundation/coreum/v3/x/asset/ft/types"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

const (
	xrp           = "XRP"
	drop          = "drop"
	xrplPrecision = 15
	xrpIssuer     = "rrrrrrrrrrrrrrrrrrrrrhoLvTp"
	xrpCurrency   = "XRP"

	xrpSendingPrecision            = 6
	eventAttributeThresholdReached = "threshold_reached"
)

var (
	defaultTrustSetLimitAmount = sdkmath.NewInt(10000000000000000)
	xrpMaxHoldingAmount        = sdkmath.NewInt(10000000000000000)
)

func TestDeployAndInstantiateContract(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	assetftClient := assetfttypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 1)

	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()

	usedTicketSequenceThreshold := 10
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		len(relayers),
		usedTicketSequenceThreshold,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
	)

	contractCfg, err := contractClient.GetContractConfig(ctx)
	require.NoError(t, err)

	require.Equal(t, coreum.ContractConfig{
		Relayers:                    relayers,
		EvidenceThreshold:           len(relayers),
		UsedTicketSequenceThreshold: usedTicketSequenceThreshold,
		TrustSetLimitAmount:         defaultTrustSetLimitAmount,
		BridgeXRPLAddress:           bridgeXRPLAddress,
	}, contractCfg)

	contractOwnership, err := contractClient.GetContractOwnership(ctx)
	require.NoError(t, err)

	require.Equal(t, coreum.ContractOwnership{
		Owner:        owner,
		PendingOwner: sdk.AccAddress{},
	}, contractOwnership)

	contractAddress := contractClient.GetContractAddress()
	tokensRes, err := assetftClient.Tokens(ctx, &assetfttypes.QueryTokensRequest{
		Issuer: contractAddress.String(),
	})
	require.NoError(t, err)
	require.Len(t, tokensRes.Tokens, 1)

	coreumDenom := fmt.Sprintf("%s-%s", drop, contractAddress.String())
	require.Equal(t, assetfttypes.Token{
		Denom:              coreumDenom,
		Issuer:             contractAddress.String(),
		Symbol:             xrp,
		Subunit:            drop,
		Precision:          6,
		Description:        "",
		GloballyFrozen:     false,
		Features:           []assetfttypes.Feature{assetfttypes.Feature_minting, assetfttypes.Feature_burning, assetfttypes.Feature_ibc},
		BurnRate:           sdk.ZeroDec(),
		SendCommissionRate: sdk.ZeroDec(),
		Version:            assetfttypes.CurrentTokenVersion,
	}, tokensRes.Tokens[0])

	// query all tokens
	xrplTokens, err := contractClient.GetXRPLTokens(ctx)
	require.NoError(t, err)

	require.Len(t, xrplTokens, 1)
	require.Equal(t, coreum.XRPLToken{
		Issuer:           xrpIssuer,
		Currency:         xrpCurrency,
		CoreumDenom:      coreumDenom,
		SendingPrecision: xrpSendingPrecision,
		MaxHoldingAmount: xrpMaxHoldingAmount,
		State:            coreum.TokenStateEnabled,
	}, xrplTokens[0])
}

func TestChangeContractOwnership(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 1)

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		len(relayers),
		10,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
	)

	contractOwnership, err := contractClient.GetContractOwnership(ctx)
	require.NoError(t, err)
	require.Equal(t, owner.String(), contractOwnership.Owner.String())

	newOwner := chains.Coreum.GenAccount()
	// fund to cover fees
	chains.Coreum.FundAccountWithOptions(ctx, t, newOwner, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})

	// transfer ownership
	_, err = contractClient.TransferOwnership(ctx, owner, newOwner)
	require.NoError(t, err)
	contractOwnership, err = contractClient.GetContractOwnership(ctx)
	require.NoError(t, err)
	// the owner is still old until new owner accepts the ownership
	require.Equal(t, owner.String(), contractOwnership.Owner.String())
	require.Equal(t, newOwner.String(), contractOwnership.PendingOwner.String())

	// accept the ownership
	_, err = contractClient.AcceptOwnership(ctx, newOwner)
	require.NoError(t, err)
	contractOwnership, err = contractClient.GetContractOwnership(ctx)
	require.NoError(t, err)
	// the contract has a new owner
	require.Equal(t, newOwner.String(), contractOwnership.Owner.String())

	// try to update the ownership one more time (from old owner)
	_, err = contractClient.TransferOwnership(ctx, owner, newOwner)
	require.True(t, coreum.IsNotOwnerError(err), err)
}

func TestRegisterCoreumToken(t *testing.T) {
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
		Amount: sdkmath.NewInt(1_000_000),
	})

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		len(relayers),
		10,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
	)

	denom1 := "denom1"
	denom1Decimals := uint32(17)
	sendingPrecision := int32(15)
	maxHoldingAmount := sdk.NewIntFromUint64(10000)

	// try to register from not owner
	_, err := contractClient.RegisterCoreumToken(ctx, notOwner, denom1, denom1Decimals, sendingPrecision, maxHoldingAmount)
	require.True(t, coreum.IsNotOwnerError(err), err)

	// register from the owner
	_, err = contractClient.RegisterCoreumToken(ctx, owner, denom1, denom1Decimals, sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)

	// try to register the same denom one more time
	_, err = contractClient.RegisterCoreumToken(ctx, owner, denom1, denom1Decimals, sendingPrecision, maxHoldingAmount)
	require.True(t, coreum.IsCoreumTokenAlreadyRegisteredError(err), err)

	coreumTokens, err := contractClient.GetCoreumTokens(ctx)
	require.NoError(t, err)
	require.Len(t, coreumTokens, 1)

	registeredToken := coreumTokens[0]
	require.Equal(t, coreum.CoreumToken{
		Denom:            denom1,
		Decimals:         denom1Decimals,
		XRPLCurrency:     registeredToken.XRPLCurrency,
		SendingPrecision: sendingPrecision,
		MaxHoldingAmount: maxHoldingAmount,
		State:            coreum.TokenStateEnabled,
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
	receiveAmount, ok := balancesAfterSend[fmt.Sprintf("%s/%s", currency.String(), issuerAcc.String())]
	require.True(t, ok)
	require.Equal(t, amountToSend.String(), receiveAmount.Value.String())
}

func TestRegisterXRPLToken(t *testing.T) {
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
		len(relayers),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
	)

	// fund owner to cover issuance fees twice
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Mul(sdkmath.NewIntFromUint64(2)),
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	inactiveCurrency := "INA"
	activeCurrency := "ACT"
	sendingPrecision := int32(15)
	maxHoldingAmount := sdk.NewIntFromUint64(10000)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	// try to register from not owner
	_, err := contractClient.RegisterXRPLToken(ctx, notOwner, issuer, inactiveCurrency, sendingPrecision, maxHoldingAmount)
	require.True(t, coreum.IsNotOwnerError(err), err)

	// register from the owner
	_, err = contractClient.RegisterXRPLToken(ctx, owner, issuer, inactiveCurrency, sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)

	// try to register the same denom one more time
	_, err = contractClient.RegisterXRPLToken(ctx, owner, issuer, inactiveCurrency, sendingPrecision, maxHoldingAmount)
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
		Denom:              registeredInactiveToken.CoreumDenom,
		Issuer:             contractAddress.String(),
		Symbol:             strings.ToUpper(prefix),
		Subunit:            prefix,
		Precision:          xrplPrecision,
		Description:        "",
		GloballyFrozen:     false,
		Features:           []assetfttypes.Feature{assetfttypes.Feature_minting, assetfttypes.Feature_burning, assetfttypes.Feature_ibc},
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
		Issuer:   issuer,
		Currency: inactiveCurrency,
	}

	// try to register not existing operation
	invalidEvidenceTrustSetWithInvalidTicket := rejectedTxEvidenceTrustSet
	invalidEvidenceTrustSetWithInvalidTicket.TicketSequence = lo.ToPtr(uint32(99))
	_, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(ctx, relayers[0].CoreumAddress, invalidEvidenceTrustSetWithInvalidTicket)
	require.True(t, coreum.IsPendingOperationNotFoundError(err), err)

	// try to register with not existing currency
	invalidEvidenceNotExistingIssuer := rejectedTxEvidenceTrustSet
	invalidEvidenceNotExistingIssuer.Issuer = xrpl.GenPrivKeyTxSigner().Account().String()
	_, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(ctx, relayers[0].CoreumAddress, invalidEvidenceNotExistingIssuer)
	require.True(t, coreum.IsTokenNotRegisteredError(err), err)

	// send valid rejected evidence from first relayer
	txResTrustSet, err := contractClient.SendXRPLTrustSetTransactionResultEvidence(ctx, relayers[0].CoreumAddress, rejectedTxEvidenceTrustSet)
	require.NoError(t, err)
	thresholdReachedTrustSet, err := event.FindStringEventAttribute(txResTrustSet.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(false), thresholdReachedTrustSet)
	// send valid rejected evidence from second relayer
	txResTrustSet, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(ctx, relayers[1].CoreumAddress, rejectedTxEvidenceTrustSet)
	require.NoError(t, err)
	thresholdReachedTrustSet, err = event.FindStringEventAttribute(txResTrustSet.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
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
	}, registeredInactiveToken)

	// try to send evidence one more time
	_, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(ctx, relayers[1].CoreumAddress, rejectedTxEvidenceTrustSet)
	require.True(t, coreum.IsOperationAlreadyExecutedError(err), err)

	// try to register the sending from the XRPL to coreum evidence with inactive token
	xrplToCoreumInactiveTokenTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    issuerAcc.String(),
		Currency:  inactiveCurrency,
		Amount:    sdkmath.NewInt(10),
		Recipient: coreumRecipient,
	}
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[1].CoreumAddress, xrplToCoreumInactiveTokenTransferEvidence)
	require.True(t, coreum.IsXRPLTokenNotEnabledError(err), err)

	// register one more token and activate it
	_, err = contractClient.RegisterXRPLToken(ctx, owner, issuer, activeCurrency, sendingPrecision, maxHoldingAmount)
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
}

func TestSendFromXRPLToCoreumXRPLOriginatedToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumRecipient := chains.Coreum.GenAccount()
	randomAddress := chains.Coreum.GenAccount()
	relayers := genRelayers(ctx, t, chains, 2)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	chains.Coreum.FundAccountWithOptions(ctx, t, randomAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		len(relayers),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
	)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	// fund owner to cover issuance fees
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := "RCR"
	sendingPrecision := int32(15)
	maxHoldingAmount := sdk.NewIntFromUint64(10000)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	// register from the owner
	_, err := contractClient.RegisterXRPLToken(ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)

	xrplTokens, err := contractClient.GetXRPLTokens(ctx)
	require.NoError(t, err)
	// find registered token
	var registeredToken coreum.XRPLToken
	for _, token := range xrplTokens {
		if token.Issuer == issuer && token.Currency == currency {
			registeredToken = token
			break
		}
	}
	require.Equal(t, issuer, registeredToken.Issuer)
	require.Equal(t, currency, registeredToken.Currency)
	require.NotEmpty(t, registeredToken.CoreumDenom)

	// activate token
	activateXRPLToken(ctx, t, contractClient, relayers, issuer, currency)

	// create an evidence of transfer tokens from XRPL to Coreum
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    issuerAcc.String(),
		Currency:  currency,
		Amount:    sdkmath.NewInt(10),
		Recipient: coreumRecipient,
	}

	// try to call from not relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, randomAddress, xrplToCoreumTransferEvidence)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try use not registered token
	wrongXRPLToCoreumTransferEvidence := xrplToCoreumTransferEvidence
	wrongXRPLToCoreumTransferEvidence.Currency = "NEZ"
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, wrongXRPLToCoreumTransferEvidence)
	require.True(t, coreum.IsTokenNotRegisteredError(err), err)

	// call from first relayer
	txRes, err := contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence)
	require.NoError(t, err)
	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.True(t, recipientBalanceRes.Balance.IsZero())
	thresholdReached, err := event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(false), thresholdReached)

	// call from first relayer one more time
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence)
	require.True(t, coreum.IsEvidenceAlreadyProvidedError(err), err)

	// call from second relayer
	txRes, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[1].CoreumAddress, xrplToCoreumTransferEvidence)
	require.NoError(t, err)
	recipientBalanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredToken.CoreumDenom,
	})
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(true), thresholdReached)

	require.NoError(t, err)
	// expect new token on the recipient balance
	require.Equal(t, xrplToCoreumTransferEvidence.Amount.String(), recipientBalanceRes.Balance.Amount.String())

	// try to push the same evidence
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence)
	require.True(t, coreum.IsOperationAlreadyExecutedError(err), err)
}

//nolint:tparallel // the test is parallel, but test cases are not
func TestSendFromXRPLToCoreumXRPLOriginatedTokenWithDifferentSendingPrecision(t *testing.T) {
	t.Parallel()

	var (
		tokenDecimals        = int64(15)
		highMaxHoldingAmount = integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)
	)

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 2)
	coreumRecipient := chains.Coreum.GenAccount()

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		len(relayers),
		10,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	tests := []struct {
		name                                       string
		sendingPrecision                           int32
		sendingAmount                              sdkmath.Int
		maxHoldingAmount                           sdkmath.Int
		wantReceivedAmount                         sdkmath.Int
		wantIsAmountSentIsZeroAfterTruncationError bool
		wantIsMaximumBridgedAmountReachedError     bool
	}{
		{
			name:               "positive_precision_no_truncation",
			sendingPrecision:   2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", tokenDecimals),
		},
		{
			name:               "positive_precision_with_truncation",
			sendingPrecision:   2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15567", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15", tokenDecimals),
		},
		{
			name:             "positive_precision_low_amount",
			sendingPrecision: 2,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.009999", tokenDecimals),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "zero_precision_no_truncation",
			sendingPrecision:   0,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", tokenDecimals),
		},
		{
			name:               "zero_precision_with_truncation",
			sendingPrecision:   0,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1.15567", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", tokenDecimals),
		},
		{
			name:             "zero_precision_low_amount",
			sendingPrecision: 0,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.9999", tokenDecimals),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "negative_precision_no_truncation",
			sendingPrecision:   -2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", tokenDecimals),
		},
		{
			name:               "negative_precision_with_truncation",
			sendingPrecision:   -2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999.15567", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9900", tokenDecimals),
		},
		{
			name:             "negative_precision_low_amount",
			sendingPrecision: -2,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "99.9999", tokenDecimals),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:                                   "reached_max_holding_amount",
			sendingPrecision:                       2,
			maxHoldingAmount:                       integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999", tokenDecimals),
			sendingAmount:                          integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999.01", tokenDecimals),
			wantIsMaximumBridgedAmountReachedError: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// fund owner to cover registration fee
			chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
				Amount: issueFee.Amount,
			})

			issuerAcc := xrpl.GenPrivKeyTxSigner().Account()
			issuer := issuerAcc.String()
			currency := "CRC"

			// register from the owner
			_, err := contractClient.RegisterXRPLToken(ctx, owner, issuer, currency, tt.sendingPrecision, tt.maxHoldingAmount)
			require.NoError(t, err)
			registeredXRPLToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
			require.NoError(t, err)

			// activate token
			activateXRPLToken(ctx, t, contractClient, relayers, issuerAcc.String(), currency)

			// create an evidence
			xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
				TxHash:    genXRPLTxHash(t),
				Issuer:    issuerAcc.String(),
				Currency:  currency,
				Amount:    tt.sendingAmount,
				Recipient: coreumRecipient,
			}

			// call from all relayers
			for _, relayer := range relayers {
				_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence)
				if tt.wantIsAmountSentIsZeroAfterTruncationError {
					require.True(t, coreum.IsAmountSentIsZeroAfterTruncationError(err), err)
					return
				}
				if tt.wantIsMaximumBridgedAmountReachedError {
					require.True(t, coreum.IsMaximumBridgedAmountReachedError(err), err)
					return
				}
				require.NoError(t, err)
			}

			balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: coreumRecipient.String(),
				Denom:   registeredXRPLToken.CoreumDenom,
			})
			require.NoError(t, err)
			require.Equal(t, tt.wantReceivedAmount.String(), balanceRes.Balance.Amount.String())
		})
	}
}

func TestSendFromXRPLToCoreumXRPToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumRecipient := chains.Coreum.GenAccount()
	randomAddress := chains.Coreum.GenAccount()
	relayers := genRelayers(ctx, t, chains, 2)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	chains.Coreum.FundAccountWithOptions(ctx, t, randomAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})

	_, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		len(relayers),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
	)
	registeredXRPToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, xrpl.XRPTokenIssuer.String(), xrpl.XRPTokenCurrency.String())
	require.NoError(t, err)

	require.Equal(t, coreum.XRPLToken{
		Issuer:           xrpl.XRPTokenIssuer.String(),
		Currency:         xrpl.XRPTokenCurrency.String(),
		CoreumDenom:      assetfttypes.BuildDenom("drop", contractClient.GetContractAddress()),
		SendingPrecision: 6,
		MaxHoldingAmount: sdkmath.NewInt(10000000000000000),
		State:            coreum.TokenStateEnabled,
	}, registeredXRPToken)

	// create an evidence of transfer tokens from XRPL to Coreum
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    rippledata.Account{}.String(),
		Currency:  rippledata.Currency{}.String(),
		Amount:    sdkmath.NewInt(10),
		Recipient: coreumRecipient,
	}

	// try to call from not relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, randomAddress, xrplToCoreumTransferEvidence)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// call from first relayer
	txRes, err := contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence)
	require.NoError(t, err)
	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredXRPToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.True(t, recipientBalanceRes.Balance.IsZero())
	thresholdReached, err := event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(false), thresholdReached)

	// call from first relayer one more time
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence)
	require.True(t, coreum.IsEvidenceAlreadyProvidedError(err), err)

	// call from second relayer
	txRes, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[1].CoreumAddress, xrplToCoreumTransferEvidence)
	require.NoError(t, err)
	recipientBalanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredXRPToken.CoreumDenom,
	})
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(true), thresholdReached)

	require.NoError(t, err)
	// expect new token on the recipient balance
	require.Equal(t, xrplToCoreumTransferEvidence.Amount.String(), recipientBalanceRes.Balance.Amount.String())

	// try to push the same evidence
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence)
	require.True(t, coreum.IsOperationAlreadyExecutedError(err), err)
}

func TestSendFromXRPLToCoreumCoreumOriginatedToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumSender := chains.Coreum.GenAccount()
	coreumRecipient := chains.Coreum.GenAccount()
	xrplRecipient := chains.XRPL.GenAccount(ctx, t, 1)

	relayers := genRelayers(ctx, t, chains, 2)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.MulRaw(2).Add(sdkmath.NewInt(10_000_000)),
	})

	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		len(relayers),
		3,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 10)

	// issue asset ft and register it
	sendingPrecision := int32(5)
	tokenDecimals := uint32(5)
	maxHoldingAmount := sdk.NewIntFromUint64(100_000_000_000)
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSender.String(),
		Symbol:        "denom",
		Subunit:       "denom",
		Precision:     tokenDecimals, // token decimals in terms of the contract
		InitialAmount: maxHoldingAmount,
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSender),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)
	denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSender)
	_, err = contractClient.RegisterCoreumToken(ctx, owner, denom, tokenDecimals, sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)
	registeredCoreumToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
	require.NoError(t, err)

	coinToSend := sdk.NewCoin(denom, sdkmath.NewInt(10))
	sendFromCoreumToXRPL(ctx, t, contractClient, relayers, coreumSender, coinToSend, xrplRecipient)

	contractBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: contractClient.GetContractAddress().String(),
		Denom:   denom,
	})
	require.NoError(t, err)
	require.Equal(t, coinToSend.String(), contractBalanceRes.Balance.String())

	// create an evidence of transfer tokens from XRPL to Coreum
	// account has 100_000_000_000 in XRPL after conversion
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:   genXRPLTxHash(t),
		Issuer:   bridgeXRPLAddress,
		Currency: registeredCoreumToken.XRPLCurrency,
		// Equivalent of sending 4 tokens back
		Amount:    sdkmath.NewInt(40_000_000_000),
		Recipient: coreumRecipient,
	}

	xrplToCoreumTransferEvidenceForNotRegisteredToken := xrplToCoreumTransferEvidence
	xrplToCoreumTransferEvidenceForNotRegisteredToken.Currency = "XZA"
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidenceForNotRegisteredToken)
	require.True(t, coreum.IsTokenNotRegisteredError(err), err)

	// call from first relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence)
	require.NoError(t, err)

	// call from second relayer
	txRes, err := contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[1].CoreumAddress, xrplToCoreumTransferEvidence)
	require.NoError(t, err)
	thresholdReached, err := event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(true), thresholdReached)

	// check recipient balance
	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredCoreumToken.Denom,
	})
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(4).String(), recipientBalanceRes.Balance.Amount.String())

	// check contract balance
	contractBalanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: contractClient.GetContractAddress().String(),
		Denom:   denom,
	})
	require.NoError(t, err)
	require.Equal(t, coinToSend.Amount.Sub(sdkmath.NewInt(4)).String(), contractBalanceRes.Balance.Amount.String())
}

//nolint:tparallel // the test is parallel, but test cases are not
func TestSendFromXRPLToCoreumCoreumOriginatedTokenWithFreezingAndWhitelisting(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumRecipient := chains.Coreum.GenAccount()
	xrplRecipient := chains.XRPL.GenAccount(ctx, t, 1)

	relayers := genRelayers(ctx, t, chains, 2)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		len(relayers),
		3,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	amountToSend := sdkmath.NewInt(10_000)

	tests := []struct {
		name              string
		features          []assetfttypes.Feature
		beforeSendToXRPL  func(t *testing.T, issuer sdk.AccAddress, denom string)
		afterSendToXRPL   func(t *testing.T, issuer sdk.AccAddress, denom string)
		checkAssetFTError func(t *testing.T, err error)
	}{
		{
			name: "freezing_of_the_contract_account",
			features: []assetfttypes.Feature{
				assetfttypes.Feature_freezing,
			},
			afterSendToXRPL: func(t *testing.T, issuer sdk.AccAddress, denom string) {
				msg := &assetfttypes.MsgFreeze{
					Sender:  issuer.String(),
					Account: contractClient.GetContractAddress().String(),
					Coin:    sdk.NewCoin(denom, amountToSend),
				}
				_, err := client.BroadcastTx(
					ctx,
					chains.Coreum.ClientContext.WithFromAddress(issuer),
					chains.Coreum.TxFactory().WithSimulateAndExecute(true),
					msg,
				)
				require.NoError(t, err)
			},
			checkAssetFTError: func(t *testing.T, err error) {
				require.True(t, coreum.IsAssetFTFreezingError(err), err)
			},
		},
		{
			name: "global_freezing",
			features: []assetfttypes.Feature{
				assetfttypes.Feature_freezing,
			},
			afterSendToXRPL: func(t *testing.T, issuer sdk.AccAddress, denom string) {
				msg := &assetfttypes.MsgGloballyFreeze{
					Sender: issuer.String(),
					Denom:  denom,
				}
				_, err := client.BroadcastTx(
					ctx,
					chains.Coreum.ClientContext.WithFromAddress(issuer),
					chains.Coreum.TxFactory().WithSimulateAndExecute(true),
					msg,
				)
				require.NoError(t, err)
			},
			checkAssetFTError: func(t *testing.T, err error) {
				require.True(t, coreum.IsAssetFTGlobalFreezingError(err), err)
			},
		},
		{
			name: "whitelisting",
			features: []assetfttypes.Feature{
				assetfttypes.Feature_whitelisting,
			},
			beforeSendToXRPL: func(t *testing.T, issuer sdk.AccAddress, denom string) {
				msg := &assetfttypes.MsgSetWhitelistedLimit{
					Sender:  issuer.String(),
					Account: contractClient.GetContractAddress().String(),
					Coin:    sdk.NewCoin(denom, amountToSend),
				}
				_, err := client.BroadcastTx(
					ctx,
					chains.Coreum.ClientContext.WithFromAddress(issuer),
					chains.Coreum.TxFactory().WithSimulateAndExecute(true),
					msg,
				)
				require.NoError(t, err)
			},
			checkAssetFTError: func(t *testing.T, err error) {
				require.True(t, coreum.IsAssetFTWhitelistedLimitExceededError(err), err)
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			coreumSender := chains.Coreum.GenAccount()
			issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
			chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
				Amount: issueFee.Amount.Add(sdkmath.NewInt(1_000_000)),
			})

			sendingPrecision := int32(5)
			tokenDecimals := uint32(5)
			maxHoldingAmount := sdk.NewIntFromUint64(100_000_000_000)
			issueMsg := &assetfttypes.MsgIssue{
				Issuer:        coreumSender.String(),
				Symbol:        "denom",
				Subunit:       "denom",
				Precision:     tokenDecimals,
				InitialAmount: maxHoldingAmount,
				Features:      tt.features,
			}
			_, err := client.BroadcastTx(
				ctx,
				chains.Coreum.ClientContext.WithFromAddress(coreumSender),
				chains.Coreum.TxFactory().WithSimulateAndExecute(true),
				issueMsg,
			)
			require.NoError(t, err)
			denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSender)
			_, err = contractClient.RegisterCoreumToken(ctx, owner, denom, tokenDecimals, sendingPrecision, maxHoldingAmount)
			require.NoError(t, err)
			registeredCoreumToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
			require.NoError(t, err)

			if tt.beforeSendToXRPL != nil {
				tt.beforeSendToXRPL(t, coreumSender, denom)
			}

			coinToSend := sdk.NewCoin(denom, amountToSend)
			sendFromCoreumToXRPL(ctx, t, contractClient, relayers, coreumSender, coinToSend, xrplRecipient)

			if tt.afterSendToXRPL != nil {
				tt.afterSendToXRPL(t, coreumSender, denom)
			}

			contractBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: contractClient.GetContractAddress().String(),
				Denom:   denom,
			})
			require.NoError(t, err)
			require.Equal(t, coinToSend.String(), contractBalanceRes.Balance.String())

			// The amount is the converted amount that was sent in XRPL and is the equivalent of 10_000 in Coreum
			amountToSendBack := sdkmath.NewInt(1_000_000_000_000_000)

			// create an evidence of transfer tokens from XRPL to Coreum
			xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
				TxHash:    genXRPLTxHash(t),
				Issuer:    bridgeXRPLAddress,
				Currency:  registeredCoreumToken.XRPLCurrency,
				Amount:    amountToSendBack,
				Recipient: coreumRecipient,
			}

			// call from first relayer
			_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence)
			require.NoError(t, err)

			// call from second relayer
			txRes, err := contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[1].CoreumAddress, xrplToCoreumTransferEvidence)
			if tt.checkAssetFTError != nil {
				require.True(t, coreum.IsAssetFTStateError(err), err)
				tt.checkAssetFTError(t, err)
				return
			}

			require.NoError(t, err)
			thresholdReached, err := event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
			require.NoError(t, err)
			require.Equal(t, strconv.FormatBool(true), thresholdReached)

			// check recipient balance
			recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: coreumRecipient.String(),
				Denom:   registeredCoreumToken.Denom,
			})
			require.NoError(t, err)
			require.Equal(t, xrplToCoreumTransferEvidence.Amount.String(), recipientBalanceRes.Balance.Amount.String())

			// check contract balance
			contractBalanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: contractClient.GetContractAddress().String(),
				Denom:   denom,
			})
			require.NoError(t, err)
			require.Equal(t, coinToSend.Amount.Sub(xrplToCoreumTransferEvidence.Amount).String(), contractBalanceRes.Balance.Amount.String())
		})
	}
}

//nolint:tparallel // the test is parallel, but test cases are not
func TestSendFromXRPLToCoreumCoreumOriginatedTokenWithDifferentSendingPrecision(t *testing.T) {
	t.Parallel()

	highMaxHoldingAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 2)
	coreumRecipient := chains.Coreum.GenAccount()

	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		len(relayers),
		10,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	tests := []struct {
		name                                       string
		sendingPrecision                           int32
		decimals                                   uint32
		sendingAmount                              sdkmath.Int
		maxHoldingAmount                           sdkmath.Int
		wantReceivedAmount                         sdkmath.Int
		wantIsAmountSentIsZeroAfterTruncationError bool
		xrplSendingAmount                          sdkmath.Int
	}{
		{
			name:               "positive_precision_no_truncation",
			sendingPrecision:   2,
			decimals:           6,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", 6),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", 6),
			xrplSendingAmount:  sdkmath.NewIntWithDecimal(999999999915, 13),
		},
		{
			name:               "positive_precision_with_truncation",
			sendingPrecision:   2,
			decimals:           20,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15567", 20),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15", 20),
			xrplSendingAmount:  sdkmath.NewIntWithDecimal(15, 13),
		},
		{
			name:              "positive_precision_low_amount",
			sendingPrecision:  2,
			decimals:          13,
			maxHoldingAmount:  highMaxHoldingAmount,
			sendingAmount:     integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.009999", 13),
			xrplSendingAmount: sdkmath.NewIntWithDecimal(9999, 8),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "zero_precision_no_truncation",
			sendingPrecision:   0,
			decimals:           11,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", 11),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", 11),
			xrplSendingAmount:  sdkmath.NewIntWithDecimal(9999999999, 15),
		},
		{
			name:               "zero_precision_with_truncation",
			sendingPrecision:   0,
			decimals:           1,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1.15567", 1),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 1),
			xrplSendingAmount:  sdkmath.NewIntWithDecimal(1, 15),
		},
		{
			name:              "zero_precision_low_amount",
			sendingPrecision:  0,
			decimals:          2,
			maxHoldingAmount:  highMaxHoldingAmount,
			sendingAmount:     integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.9999", 2),
			xrplSendingAmount: sdkmath.NewIntWithDecimal(9999, 11),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "negative_precision_no_truncation",
			sendingPrecision:   -2,
			decimals:           3,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", 3),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", 3),
			xrplSendingAmount:  sdkmath.NewIntWithDecimal(9999999900, 15),
		},
		{
			name:               "negative_precision_with_truncation",
			sendingPrecision:   -2,
			decimals:           20,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999.15567", 20),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9900", 20),
			xrplSendingAmount:  sdkmath.NewIntWithDecimal(9900, 15),
		},
		{
			name:              "negative_precision_low_amount",
			sendingPrecision:  -2,
			decimals:          6,
			maxHoldingAmount:  highMaxHoldingAmount,
			sendingAmount:     integrationtests.ConvertStringWithDecimalsToSDKInt(t, "99.9999", 6),
			xrplSendingAmount: sdkmath.NewIntWithDecimal(999999, 11),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// fund sender to cover registration fee and some coins on top for the contract calls
			coreumSenderAddress := chains.Coreum.GenAccount()
			chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
				Amount: issueFee.Amount.Add(sdkmath.NewInt(1_000_000)),
			})

			// issue asset ft and register it
			issueMsg := &assetfttypes.MsgIssue{
				Issuer:        coreumSenderAddress.String(),
				Symbol:        "denom",
				Subunit:       "denom",
				Precision:     tt.decimals, // token decimals in terms of the contract
				InitialAmount: tt.maxHoldingAmount,
			}
			_, err := client.BroadcastTx(
				ctx,
				chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
				chains.Coreum.TxFactory().WithSimulateAndExecute(true),
				issueMsg,
			)
			require.NoError(t, err)
			denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)

			_, err = contractClient.RegisterCoreumToken(ctx, owner, denom, tt.decimals, tt.sendingPrecision, tt.maxHoldingAmount)
			require.NoError(t, err)
			registeredCoreumToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
			require.NoError(t, err)

			// if we expect an error the amount is invalid so it won't be accepted from the coreum to XRPL
			if !tt.wantIsAmountSentIsZeroAfterTruncationError {
				coinToSend := sdk.NewCoin(denom, tt.sendingAmount)
				sendFromCoreumToXRPL(ctx, t, contractClient, relayers, coreumSenderAddress, coinToSend, xrpl.GenPrivKeyTxSigner().Account())
			}

			// create an evidence
			xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
				TxHash:    genXRPLTxHash(t),
				Issuer:    bridgeXRPLAddress,
				Currency:  registeredCoreumToken.XRPLCurrency,
				Amount:    tt.xrplSendingAmount,
				Recipient: coreumRecipient,
			}

			// call from all relayers
			for _, relayer := range relayers {
				_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence)
				if tt.wantIsAmountSentIsZeroAfterTruncationError {
					require.True(t, coreum.IsAmountSentIsZeroAfterTruncationError(err), err)
					return
				}
				require.NoError(t, err)
			}

			balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: coreumRecipient.String(),
				Denom:   denom,
			})
			require.NoError(t, err)
			require.Equal(t, tt.wantReceivedAmount.String(), balanceRes.Balance.Amount.String())
		})
	}
}

func TestRecoverTickets(t *testing.T) {
	t.Parallel()

	// TODO(dzmitryhil) extend the test to check multiple operations once we have them allowed to be created
	usedTicketSequenceThreshold := 5
	numberOfTicketsToInit := uint32(6)

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 3)

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		2,
		usedTicketSequenceThreshold,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
	)

	// ********** Ticket allocation / Recovery **********
	bridgeXRPLAccountFirstSeqNumber := uint32(1)

	// try to call from not owner
	_, err := contractClient.RecoverTickets(ctx, relayers[0].CoreumAddress, bridgeXRPLAccountFirstSeqNumber, &numberOfTicketsToInit)
	require.True(t, coreum.IsNotOwnerError(err), err)

	// try to use more than max allowed tickets
	_, err = contractClient.RecoverTickets(ctx, owner, bridgeXRPLAccountFirstSeqNumber, lo.ToPtr(uint32(251)))
	require.True(t, coreum.IsInvalidTicketSequenceToAllocateError(err), err)

	// try to use zero tickets
	_, err = contractClient.RecoverTickets(ctx, owner, bridgeXRPLAccountFirstSeqNumber, lo.ToPtr(uint32(0)))
	require.True(t, coreum.IsInvalidTicketSequenceToAllocateError(err), err)

	_, err = contractClient.RecoverTickets(ctx, owner, bridgeXRPLAccountFirstSeqNumber, &numberOfTicketsToInit)
	require.NoError(t, err)

	availableTickets, err := contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	// check that we have just one operation with correct data
	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	ticketsAllocationOperation := pendingOperations[0]
	require.Equal(t, coreum.Operation{
		TicketSequence:  0,
		AccountSequence: bridgeXRPLAccountFirstSeqNumber,
		Signatures:      make([]coreum.Signature, 0),
		OperationType: coreum.OperationType{
			AllocateTickets: &coreum.OperationTypeAllocateTickets{
				Number: numberOfTicketsToInit,
			},
		},
	}, ticketsAllocationOperation)

	// try to recover tickets when the tickets allocation is in-process
	_, err = contractClient.RecoverTickets(ctx, owner, bridgeXRPLAccountFirstSeqNumber, &numberOfTicketsToInit)
	require.True(t, coreum.IsPendingTicketUpdateError(err), err)

	// ********** Signatures **********

	createTicketsTx := rippledata.TicketCreate{
		TicketCount: lo.ToPtr(numberOfTicketsToInit),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TICKET_CREATE,
		},
	}
	relayer1XRPLAcc, err := rippledata.NewAccountFromAddress(relayers[0].XRPLAddress)
	require.NoError(t, err)
	signerItem1 := chains.XRPL.Multisign(t, &createTicketsTx, *relayer1XRPLAcc).Signer
	// try to send from not relayer
	_, err = contractClient.SaveSignature(ctx, owner, bridgeXRPLAccountFirstSeqNumber, signerItem1.TxnSignature.String())
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try to send with incorrect operation ID
	_, err = contractClient.SaveSignature(ctx, relayers[0].CoreumAddress, uint32(999), signerItem1.TxnSignature.String())
	require.True(t, coreum.IsPendingOperationNotFoundError(err), err)

	// send from first relayer
	_, err = contractClient.SaveSignature(ctx, relayers[0].CoreumAddress, bridgeXRPLAccountFirstSeqNumber, signerItem1.TxnSignature.String())
	require.NoError(t, err)

	// try to send from the same relayer one more time
	_, err = contractClient.SaveSignature(ctx, relayers[0].CoreumAddress, bridgeXRPLAccountFirstSeqNumber, signerItem1.TxnSignature.String())
	require.True(t, coreum.IsSignatureAlreadyProvidedError(err), err)

	// send from second relayer
	createTicketsTx = rippledata.TicketCreate{
		TicketCount: lo.ToPtr(numberOfTicketsToInit),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TICKET_CREATE,
		},
	}
	relayer2XRPLAcc, err := rippledata.NewAccountFromAddress(relayers[0].XRPLAddress)
	require.NoError(t, err)
	signerItem2 := chains.XRPL.Multisign(t, &createTicketsTx, *relayer2XRPLAcc).Signer
	_, err = contractClient.SaveSignature(ctx, relayers[1].CoreumAddress, bridgeXRPLAccountFirstSeqNumber, signerItem2.TxnSignature.String())
	require.NoError(t, err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	ticketsAllocationOperation = pendingOperations[0]
	require.Equal(t, coreum.Operation{
		TicketSequence:  0,
		AccountSequence: bridgeXRPLAccountFirstSeqNumber,
		Signatures: []coreum.Signature{
			{
				RelayerCoreumAddress: relayers[0].CoreumAddress,
				Signature:            signerItem1.TxnSignature.String(),
			},
			{
				RelayerCoreumAddress: relayers[1].CoreumAddress,
				Signature:            signerItem2.TxnSignature.String(),
			},
		},
		OperationType: coreum.OperationType{
			AllocateTickets: &coreum.OperationTypeAllocateTickets{
				Number: numberOfTicketsToInit,
			},
		},
	}, ticketsAllocationOperation)

	// ********** TransactionResultEvidence / Transaction rejected **********

	rejectedTxHash := genXRPLTxHash(t)
	rejectedTxEvidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            rejectedTxHash,
			AccountSequence:   &bridgeXRPLAccountFirstSeqNumber,
			TransactionResult: coreum.TransactionResultRejected,
		},
		Tickets: nil,
	}

	// try to send with not existing sequence
	invalidRejectedTxEvidence := rejectedTxEvidence
	invalidRejectedTxEvidence.AccountSequence = lo.ToPtr(uint32(999))
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[0].CoreumAddress, invalidRejectedTxEvidence)
	require.True(t, coreum.IsPendingOperationNotFoundError(err), err)

	// try to send with not existing ticket
	invalidRejectedTxEvidence = rejectedTxEvidence
	invalidRejectedTxEvidence.AccountSequence = nil
	invalidRejectedTxEvidence.TicketSequence = lo.ToPtr(uint32(999))
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[0].CoreumAddress, invalidRejectedTxEvidence)
	require.True(t, coreum.IsPendingOperationNotFoundError(err), err)

	// try to send from not relayer
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, owner, rejectedTxEvidence)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// send evidence from first relayer
	txRes, err := contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[0].CoreumAddress, rejectedTxEvidence)
	require.NoError(t, err)
	thresholdReached, err := event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(false), thresholdReached)

	// try to send evidence from second relayer one more time
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[0].CoreumAddress, rejectedTxEvidence)
	require.True(t, coreum.IsEvidenceAlreadyProvidedError(err), err)

	// send evidence from second relayer
	txRes, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[1].CoreumAddress, rejectedTxEvidence)
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(true), thresholdReached)

	// try to send the evidence one more time
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[0].CoreumAddress, rejectedTxEvidence)
	require.True(t, coreum.IsOperationAlreadyExecutedError(err), err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	availableTickets, err = contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	// ********** TransactionResultEvidence / Transaction invalid **********

	bridgeXRPLAccountInvalidSeqNumber := uint32(1000)
	_, err = contractClient.RecoverTickets(ctx, owner, bridgeXRPLAccountInvalidSeqNumber, &numberOfTicketsToInit)
	require.NoError(t, err)

	invalidTxEvidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            "",
			AccountSequence:   &bridgeXRPLAccountInvalidSeqNumber,
			TransactionResult: coreum.TransactionResultInvalid,
		},
		Tickets: nil,
	}
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[0].CoreumAddress, invalidTxEvidence)
	require.NoError(t, err)
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[1].CoreumAddress, invalidTxEvidence)
	require.NoError(t, err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	availableTickets, err = contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	// try to use the same sequence number (it should be possible)
	_, err = contractClient.RecoverTickets(ctx, owner, bridgeXRPLAccountInvalidSeqNumber, &numberOfTicketsToInit)
	require.NoError(t, err)

	// reject one more time
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[0].CoreumAddress, invalidTxEvidence)
	require.NoError(t, err)
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[1].CoreumAddress, invalidTxEvidence)
	require.NoError(t, err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// ********** Ticket allocation after previous failure / Recovery **********

	bridgeXRPLAccountSecondSeqNumber := uint32(2)
	// start the process one more time
	_, err = contractClient.RecoverTickets(ctx, owner, bridgeXRPLAccountSecondSeqNumber, &numberOfTicketsToInit)
	require.NoError(t, err)

	// ********** TransactionResultEvidence / Transaction accepted **********

	// we can omit the signing here since it is required only for the tx submission.
	acceptedTxEvidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			AccountSequence:   &bridgeXRPLAccountSecondSeqNumber,
			TransactionResult: coreum.TransactionResultAccepted,
		},
		Tickets: []uint32{3, 5, 6, 7},
	}

	// try to send with already used txHash
	invalidAcceptedTxEvidence := acceptedTxEvidence
	invalidAcceptedTxEvidence.TxHash = rejectedTxHash
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[0].CoreumAddress, invalidAcceptedTxEvidence)
	require.True(t, coreum.IsOperationAlreadyExecutedError(err), err)

	// send evidence from first relayer
	txRes, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[0].CoreumAddress, acceptedTxEvidence)
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(false), thresholdReached)

	// send evidence from second relayer
	txRes, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[1].CoreumAddress, acceptedTxEvidence)
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(true), thresholdReached)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	availableTickets, err = contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Equal(t, acceptedTxEvidence.Tickets, availableTickets)

	// try to call recovery when there are available tickets
	_, err = contractClient.RecoverTickets(ctx, owner, bridgeXRPLAccountSecondSeqNumber, &numberOfTicketsToInit)
	require.True(t, coreum.IsStillHaveAvailableTicketsError(err), err)
}

func TestSendFromCoreumToXRPLXRPLOriginatedToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	relayers := genRelayers(ctx, t, chains, 2)
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		len(relayers),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
	)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := "CRN"
	sendingPrecision := int32(15)
	maxHoldingAmount := sdk.NewIntFromUint64(1_000_000_000)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 5)

	// register new token
	_, err := contractClient.RegisterXRPLToken(ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)
	// activate token
	registeredXRPLOriginatedToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	require.NotEmpty(t, registeredXRPLOriginatedToken)
	activateXRPLToken(ctx, t, contractClient, relayers, issuer, currency)

	amountToSendFromXRPLToCoreum := sdkmath.NewInt(1_000_100)
	sendFromXRPLToCoreum(ctx, t, contractClient, relayers, issuer, currency, amountToSendFromXRPLToCoreum, coreumSenderAddress)
	// validate that the amount is received
	balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(t, amountToSendFromXRPLToCoreum.String(), balanceRes.Balance.Amount.String())

	amountToSend := sdkmath.NewInt(1_000_000)

	// try to send more than account has
	_, err = contractClient.SendToXRPL(ctx, coreumSenderAddress, xrplRecipientAddress.String(), sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSendFromXRPLToCoreum.AddRaw(1)))
	require.ErrorContains(t, err, cosmoserrors.ErrInsufficientFunds.Error())

	// try to send with invalid recipient
	_, err = contractClient.SendToXRPL(ctx, coreumSenderAddress, "invalid", sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSend))
	require.True(t, coreum.IsInvalidXRPLAddressError(err), err)

	// try to send with not registered token
	_, err = contractClient.SendToXRPL(ctx, coreumSenderAddress, xrplRecipientAddress.String(), sdk.NewCoin(chains.Coreum.ChainSettings.Denom, sdk.NewIntFromUint64(1)))
	require.True(t, coreum.IsTokenNotRegisteredError(err), err)

	// send valid amount and validate the state
	coreumSenderBalanceBeforeRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)
	_, err = contractClient.SendToXRPL(ctx, coreumSenderAddress, xrplRecipientAddress.String(), sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSend))
	require.NoError(t, err)
	// check the remaining balance
	coreumSenderBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(t, coreumSenderBalanceBeforeRes.Balance.Amount.Sub(amountToSend).String(), coreumSenderBalanceAfterRes.Balance.Amount.String())

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	require.Equal(t, operationType.Issuer, registeredXRPLOriginatedToken.Issuer)
	require.Equal(t, operationType.Currency, registeredXRPLOriginatedToken.Currency)
	require.Equal(t, operationType.Amount, amountToSend)
	require.Equal(t, operationType.Recipient, xrplRecipientAddress.String())

	acceptedTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}

	// send from first relayer
	_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(ctx, relayers[0].CoreumAddress, acceptedTxEvidence)
	require.NoError(t, err)

	// send from second relayer
	_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(ctx, relayers[1].CoreumAddress, acceptedTxEvidence)
	require.NoError(t, err)

	// check pending operations
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// use all available tickets
	tickets, err := contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	for i := 0; i < len(tickets)-1; i++ {
		_, err = contractClient.SendToXRPL(ctx, coreumSenderAddress, xrplRecipientAddress.String(), sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, sdk.NewIntFromUint64(1)))
		require.NoError(t, err)
	}

	// try to use last (protected) ticket
	_, err = contractClient.SendToXRPL(ctx, coreumSenderAddress, xrplRecipientAddress.String(), sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, sdk.NewIntFromUint64(1)))
	require.True(t, coreum.IsLastTicketReservedError(err))
}

//nolint:tparallel // the test is parallel, but test cases are not
func TestSendFromCoreumToXRPLXRPLOriginatedTokenWithDifferentSendingPrecision(t *testing.T) {
	t.Parallel()

	var (
		tokenDecimals        = int64(15)
		highMaxHoldingAmount = integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)
	)

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 2)
	xrplRecipient := xrpl.GenPrivKeyTxSigner().Account()

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		len(relayers),
		50,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	tests := []struct {
		name                                       string
		sendingPrecision                           int32
		sendingAmount                              sdkmath.Int
		maxHoldingAmount                           sdkmath.Int
		wantReceivedAmount                         sdkmath.Int
		wantIsAmountSentIsZeroAfterTruncationError bool
	}{
		{
			name:               "positive_precision_no_truncation",
			sendingPrecision:   2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", tokenDecimals),
		},
		{
			name:               "positive_precision_with_truncation",
			sendingPrecision:   2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15567", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15", tokenDecimals),
		},
		{
			name:             "positive_precision_low_amount",
			sendingPrecision: 2,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.009999", tokenDecimals),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "zero_precision_no_truncation",
			sendingPrecision:   0,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", tokenDecimals),
		},
		{
			name:               "zero_precision_with_truncation",
			sendingPrecision:   0,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1.15567", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", tokenDecimals),
		},
		{
			name:             "zero_precision_low_amount",
			sendingPrecision: 0,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.9999", tokenDecimals),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "negative_precision_no_truncation",
			sendingPrecision:   -2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", tokenDecimals),
		},
		{
			name:               "negative_precision_with_truncation",
			sendingPrecision:   -2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999.15567", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9900", tokenDecimals),
		},
		{
			name:             "negative_precision_low_amount",
			sendingPrecision: -2,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "99.9999", tokenDecimals),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// fund owner to cover registration fee
			chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
				Amount: issueFee.Amount,
			})

			issuerAcc := xrpl.GenPrivKeyTxSigner().Account()
			issuer := issuerAcc.String()
			currency := "CRC"

			// register from the owner
			_, err := contractClient.RegisterXRPLToken(ctx, owner, issuer, currency, tt.sendingPrecision, tt.maxHoldingAmount)
			require.NoError(t, err)
			registeredXRPLToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
			require.NoError(t, err)

			// activate token
			activateXRPLToken(ctx, t, contractClient, relayers, issuerAcc.String(), currency)

			coreumSenderAddress := chains.Coreum.GenAccount()
			// fund coreum sender address to cover fee
			chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
				Amount: sdkmath.NewInt(1_000_000),
			})
			sendFromXRPLToCoreum(ctx, t, contractClient, relayers, issuer, currency, tt.maxHoldingAmount, coreumSenderAddress)
			coreumSenderBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: coreumSenderAddress.String(),
				Denom:   registeredXRPLToken.CoreumDenom,
			})
			require.NoError(t, err)
			require.Equal(t, tt.maxHoldingAmount.String(), coreumSenderBalanceRes.Balance.Amount.String())

			_, err = contractClient.SendToXRPL(ctx, coreumSenderAddress, xrplRecipient.String(), sdk.NewCoin(registeredXRPLToken.CoreumDenom, tt.sendingAmount))
			if tt.wantIsAmountSentIsZeroAfterTruncationError {
				require.True(t, coreum.IsAmountSentIsZeroAfterTruncationError(err), err)
				return
			}
			require.NoError(t, err)

			pendingOperations, err := contractClient.GetPendingOperations(ctx)
			require.NoError(t, err)
			found := false
			for _, operation := range pendingOperations {
				operationType := operation.OperationType.CoreumToXRPLTransfer
				if operationType != nil && operationType.Issuer == issuer && operationType.Currency == currency {
					found = true
					require.Equal(t, tt.wantReceivedAmount.String(), operationType.Amount.String())
				}
			}
			require.True(t, found)
		})
	}
}

func TestSendFromCoreumToXRPLXRPToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 2)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		len(relayers),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
	)
	registeredXRPToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, xrpl.XRPTokenIssuer.String(), xrpl.XRPTokenCurrency.String())
	require.NoError(t, err)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 5)

	amountToSendFromXRPLToCoreum := sdkmath.NewInt(1_000_100)
	sendFromXRPLToCoreum(ctx, t, contractClient, relayers, registeredXRPToken.Issuer, registeredXRPToken.Currency, amountToSendFromXRPLToCoreum, coreumSenderAddress)
	// validate that the amount is received
	balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(t, amountToSendFromXRPLToCoreum.String(), balanceRes.Balance.Amount.String())

	amountToSend := sdkmath.NewInt(1_000_000)

	// send valid amount and validate the state
	coreumSenderBalanceBeforeRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPToken.CoreumDenom,
	})
	require.NoError(t, err)
	_, err = contractClient.SendToXRPL(ctx, coreumSenderAddress, xrplRecipientAddress.String(), sdk.NewCoin(registeredXRPToken.CoreumDenom, amountToSend))
	require.NoError(t, err)
	// check the remaining balance
	coreumSenderBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(t, coreumSenderBalanceBeforeRes.Balance.Amount.Sub(amountToSend).String(), coreumSenderBalanceAfterRes.Balance.Amount.String())

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	require.Equal(t, operationType.Issuer, registeredXRPToken.Issuer)
	require.Equal(t, operationType.Currency, registeredXRPToken.Currency)
	require.Equal(t, operationType.Amount, amountToSend)
	require.Equal(t, operationType.Recipient, xrplRecipientAddress.String())

	acceptedTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}

	// send from first relayer
	_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(ctx, relayers[0].CoreumAddress, acceptedTxEvidence)
	require.NoError(t, err)

	// send from second relayer
	_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(ctx, relayers[1].CoreumAddress, acceptedTxEvidence)
	require.NoError(t, err)

	// check pending operations
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)
}

func TestSendFromCoreumToXRPLCoreumOriginatedToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.MulRaw(2).Add(sdkmath.NewInt(10_000_000)),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	relayers := genRelayers(ctx, t, chains, 2)
	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		len(relayers),
		3,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 10)

	// issue asset ft and register it
	sendingPrecision1 := int32(5)
	tokenDecimals1 := uint32(5)
	maxHoldingAmount1 := sdk.NewIntFromUint64(100_000_000_000)
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSenderAddress.String(),
		Symbol:        "denom1",
		Subunit:       "denom1",
		Precision:     tokenDecimals1, // token decimals in terms of the contract
		InitialAmount: maxHoldingAmount1,
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)
	denom1 := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)
	_, err = contractClient.RegisterCoreumToken(ctx, owner, denom1, tokenDecimals1, sendingPrecision1, maxHoldingAmount1)
	require.NoError(t, err)
	registeredCoreumOriginatedToken1, err := contractClient.GetCoreumTokenByDenom(ctx, denom1)
	require.NoError(t, err)

	// register coreum (udevcore) denom
	denom2 := chains.Coreum.ChainSettings.Denom
	sendingPrecision2 := int32(6)
	tokenDecimals2 := uint32(6)
	maxHoldingAmount2 := sdk.NewIntFromUint64(1_000_000_000)
	_, err = contractClient.RegisterCoreumToken(ctx, owner, denom2, tokenDecimals2, sendingPrecision2, maxHoldingAmount2)
	require.NoError(t, err)
	registeredCoreumOriginatedToken2, err := contractClient.GetCoreumTokenByDenom(ctx, denom2)
	require.NoError(t, err)

	// issue asset ft but not register it
	issueMsg = &assetfttypes.MsgIssue{
		Issuer:        coreumSenderAddress.String(),
		Symbol:        "notreg",
		Subunit:       "notreg",
		Precision:     uint32(16), // token decimals in terms of the contract
		InitialAmount: sdk.NewIntFromUint64(100_000_000_000),
	}
	_, err = client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)
	notRegisteredTokenDenom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)

	// try to send with not registered token
	_, err = contractClient.SendToXRPL(ctx, coreumSenderAddress, xrplRecipientAddress.String(), sdk.NewCoin(notRegisteredTokenDenom, sdk.NewIntFromUint64(1)))
	require.True(t, coreum.IsTokenNotRegisteredError(err), err)

	// ********** test token1 (assetft) **********

	amountToSendOfToken1 := sdkmath.NewInt(1_001_001)

	// send valid amount and validate the state
	coreumSenderBalanceBeforeRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredCoreumOriginatedToken1.Denom,
	})
	require.NoError(t, err)
	_, err = contractClient.SendToXRPL(ctx, coreumSenderAddress, xrplRecipientAddress.String(), sdk.NewCoin(registeredCoreumOriginatedToken1.Denom, amountToSendOfToken1))
	require.NoError(t, err)
	// check the remaining balance
	coreumSenderBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredCoreumOriginatedToken1.Denom,
	})
	require.NoError(t, err)
	require.Equal(t, coreumSenderBalanceBeforeRes.Balance.Amount.Sub(amountToSendOfToken1).String(), coreumSenderBalanceAfterRes.Balance.Amount.String())

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	require.Equal(t, operationType.Issuer, bridgeXRPLAddress)
	require.Equal(t, operationType.Currency, registeredCoreumOriginatedToken1.XRPLCurrency)
	// XRPL DECIMALS (15) - TOKEN DECIMALS (5) = 10
	require.Equal(t, operationType.Amount, amountToSendOfToken1.Mul(sdk.NewInt(10_000_000_000)))
	require.Equal(t, operationType.Recipient, xrplRecipientAddress.String())

	acceptedTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}
	// send from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(ctx, relayer.CoreumAddress, acceptedTxEvidence)
		require.NoError(t, err)
	}

	// check pending operations
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// ********** test token2 (udevcore) **********

	amountToSendOfToken2 := sdkmath.NewInt(1_002_001)

	// send valid amount and validate the state
	coreumSenderBalanceBeforeRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredCoreumOriginatedToken2.Denom,
	})
	require.NoError(t, err)
	_, err = contractClient.SendToXRPL(ctx, coreumSenderAddress, xrplRecipientAddress.String(), sdk.NewCoin(registeredCoreumOriginatedToken2.Denom, amountToSendOfToken2))
	require.NoError(t, err)
	// check the remaining balance
	coreumSenderBalanceAfterRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredCoreumOriginatedToken2.Denom,
	})
	require.NoError(t, err)
	require.True(t, coreumSenderBalanceBeforeRes.Balance.Amount.Sub(amountToSendOfToken2).GT(coreumSenderBalanceAfterRes.Balance.Amount))

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation = pendingOperations[0]
	operationType = operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	require.Equal(t, operationType.Issuer, bridgeXRPLAddress)
	require.Equal(t, operationType.Currency, registeredCoreumOriginatedToken2.XRPLCurrency)
	// XRPL DECIMALS (15) - TOKEN DECIMALS (6) = 9
	require.Equal(t, operationType.Amount, amountToSendOfToken2.Mul(sdkmath.NewIntWithDecimal(1, 9)))
	require.Equal(t, operationType.Recipient, xrplRecipientAddress.String())

	acceptedTxEvidence = coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}

	// send from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(ctx, relayer.CoreumAddress, acceptedTxEvidence)
		require.NoError(t, err)
	}

	// check pending operations
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// ********** use all available tickets **********

	tickets, err := contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	for i := 0; i < len(tickets)-1; i++ {
		_, err = contractClient.SendToXRPL(ctx, coreumSenderAddress, xrplRecipientAddress.String(), sdk.NewCoin(registeredCoreumOriginatedToken1.Denom, sdk.NewIntFromUint64(1)))
		require.NoError(t, err)
	}

	// try to use last (protected) ticket
	_, err = contractClient.SendToXRPL(ctx, coreumSenderAddress, xrplRecipientAddress.String(), sdk.NewCoin(registeredCoreumOriginatedToken1.Denom, sdk.NewIntFromUint64(1)))
	require.True(t, coreum.IsLastTicketReservedError(err))
}

//nolint:tparallel // the test is parallel, but test cases are not
func TestSendFromCoreumToXRPLCoreumOriginatedTokenWithDifferentSendingPrecisionAndDecimals(t *testing.T) {
	t.Parallel()

	highMaxHoldingAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)
	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 2)
	xrplRecipient := xrpl.GenPrivKeyTxSigner().Account()

	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		len(relayers),
		50,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	tests := []struct {
		name                                       string
		sendingPrecision                           int32
		decimals                                   uint32
		sendingAmount                              sdkmath.Int
		maxHoldingAmount                           sdkmath.Int
		wantReceivedAmount                         sdkmath.Int
		wantIsAmountSentIsZeroAfterTruncationError bool
		wantIsMaximumBridgedAmountReachedError     bool
	}{
		{
			name:               "positive_precision_no_truncation",
			sendingPrecision:   2,
			decimals:           6,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", 6),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", 15),
		},
		{
			name:               "positive_precision_with_truncation",
			sendingPrecision:   2,
			decimals:           20,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15567", 20),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15", 15),
		},
		{
			name:             "positive_precision_low_amount",
			sendingPrecision: 2,
			decimals:         13,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.009999", 13),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "zero_precision_no_truncation",
			sendingPrecision:   0,
			decimals:           11,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", 11),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", 15),
		},
		{
			name:               "zero_precision_with_truncation",
			sendingPrecision:   0,
			decimals:           1,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1.15567", 1),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 15),
		},
		{
			name:             "zero_precision_low_amount",
			sendingPrecision: 0,
			decimals:         2,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.9999", 2),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "negative_precision_no_truncation",
			sendingPrecision:   -2,
			decimals:           3,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", 3),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", 15),
		},
		{
			name:               "negative_precision_with_truncation",
			sendingPrecision:   -2,
			decimals:           20,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999.15567", 20),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9900", 15),
		},
		{
			name:             "negative_precision_low_amount",
			sendingPrecision: -2,
			decimals:         6,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "99.9999", 6),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:                                   "reached_max_holding_amount",
			sendingPrecision:                       2,
			decimals:                               8,
			maxHoldingAmount:                       integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999", 8),
			sendingAmount:                          integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999.01", 8),
			wantIsMaximumBridgedAmountReachedError: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// fund sender to cover registration fee and some coins on top for the contract calls
			coreumSenderAddress := chains.Coreum.GenAccount()
			chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
				Amount: issueFee.Amount.Add(sdkmath.NewInt(1_000_000)),
			})

			// issue asset ft and register it
			issueMsg := &assetfttypes.MsgIssue{
				Issuer:        coreumSenderAddress.String(),
				Symbol:        "denom",
				Subunit:       "denom",
				Precision:     tt.decimals,                   // token decimals in terms of the contract
				InitialAmount: tt.maxHoldingAmount.MulRaw(2), // twice more to be able to send more than max
			}
			_, err := client.BroadcastTx(
				ctx,
				chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
				chains.Coreum.TxFactory().WithSimulateAndExecute(true),
				issueMsg,
			)
			require.NoError(t, err)
			denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)

			_, err = contractClient.RegisterCoreumToken(ctx, owner, denom, tt.decimals, tt.sendingPrecision, tt.maxHoldingAmount)
			require.NoError(t, err)
			registeredCoreumToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
			require.NoError(t, err)

			_, err = contractClient.SendToXRPL(ctx, coreumSenderAddress, xrplRecipient.String(), sdk.NewCoin(registeredCoreumToken.Denom, tt.sendingAmount))
			if tt.wantIsAmountSentIsZeroAfterTruncationError {
				require.True(t, coreum.IsAmountSentIsZeroAfterTruncationError(err), err)
				return
			}
			if tt.wantIsMaximumBridgedAmountReachedError {
				require.True(t, coreum.IsMaximumBridgedAmountReachedError(err), err)
				return
			}
			require.NoError(t, err)

			pendingOperations, err := contractClient.GetPendingOperations(ctx)
			require.NoError(t, err)
			found := false
			for _, operation := range pendingOperations {
				operationType := operation.OperationType.CoreumToXRPLTransfer
				if operationType != nil && operationType.Issuer == bridgeXRPLAddress && operationType.Currency == registeredCoreumToken.XRPLCurrency {
					found = true
					require.Equal(t, tt.wantReceivedAmount.String(), operationType.Amount.String())
				}
			}
			require.True(t, found)
		})
	}
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
		len(relayers),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
	)

	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Mul(sdkmath.NewIntFromUint64(1)),
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := "CRN"
	sendingPrecision := int32(15)
	maxHoldingAmount := sdk.NewIntFromUint64(10000)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	// register from the owner
	_, err := contractClient.RegisterXRPLToken(ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount)
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
		Issuer:   issuer,
		Currency: currency,
	}

	// send from first relayer
	_, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(ctx, relayers[0].CoreumAddress, rejectedTxEvidenceTrustSet)
	require.NoError(t, err)
	// send from second relayer
	_, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(ctx, relayers[1].CoreumAddress, rejectedTxEvidenceTrustSet)
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
	require.True(t, coreum.IsNotOwnerError(err), err)

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
		Issuer:   issuer,
		Currency: currency,
	}

	// send from first relayer
	_, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(ctx, relayers[0].CoreumAddress, acceptedTxEvidenceTrustSet)
	require.NoError(t, err)
	// send from second relayer
	_, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(ctx, relayers[1].CoreumAddress, acceptedTxEvidenceTrustSet)
	require.NoError(t, err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// fetch token to validate status
	registeredXRPLToken, err = contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	require.Equal(t, coreum.TokenStateEnabled, registeredXRPLToken.State)
}

func recoverTickets(
	ctx context.Context,
	t *testing.T,
	contractClient *coreum.ContractClient,
	owner sdk.AccAddress,
	relayers []coreum.Relayer,
	numberOfTickets uint32,
) {
	bridgeXRPLAccountFirstSeqNumber := uint32(1)
	_, err := contractClient.RecoverTickets(ctx, owner, bridgeXRPLAccountFirstSeqNumber, &numberOfTickets)
	require.NoError(t, err)

	acceptedTxEvidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			AccountSequence:   &bridgeXRPLAccountFirstSeqNumber,
			TransactionResult: coreum.TransactionResultAccepted,
		},
		Tickets: lo.RepeatBy(int(numberOfTickets), func(index int) uint32 {
			return uint32(index + 1)
		}),
	}

	for _, relayer := range relayers {
		txRes, err := contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayer.CoreumAddress, acceptedTxEvidence)
		require.NoError(t, err)
		thresholdReached, err := event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
		require.NoError(t, err)
		if thresholdReached == strconv.FormatBool(true) {
			break
		}
	}
}

func activateXRPLToken(
	ctx context.Context,
	t *testing.T,
	contractClient *coreum.ContractClient,
	relayers []coreum.Relayer,
	issuer, currency string,
) {
	t.Helper()

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)

	var (
		turstSetOperation coreum.Operation
		found             bool
	)
	for _, operation := range pendingOperations {
		operationType := operation.OperationType.TrustSet
		if operationType != nil && operationType.Issuer == issuer && operationType.Currency == currency {
			found = true
			turstSetOperation = operation
			break
		}
	}
	require.True(t, found)
	require.NotNil(t, turstSetOperation.OperationType.TrustSet)

	acceptedTxEvidenceTrustSet := coreum.XRPLTransactionResultTrustSetEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &turstSetOperation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
		Issuer:   issuer,
		Currency: currency,
	}

	// send evidences from relayers
	for _, relayer := range relayers {
		txRes, err := contractClient.SendXRPLTrustSetTransactionResultEvidence(ctx, relayer.CoreumAddress, acceptedTxEvidenceTrustSet)
		require.NoError(t, err)
		thresholdReached, err := event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
		require.NoError(t, err)
		if thresholdReached == strconv.FormatBool(true) {
			break
		}
	}

	// asset token state
	registeredToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	require.Equal(t, coreum.TokenStateEnabled, registeredToken.State)
}

func sendFromXRPLToCoreum(
	ctx context.Context,
	t *testing.T,
	contractClient *coreum.ContractClient,
	relayers []coreum.Relayer,
	issuer, currency string,
	amount sdkmath.Int,
	coreumRecipient sdk.AccAddress,
) {
	t.Helper()

	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    issuer,
		Currency:  currency,
		Amount:    amount,
		Recipient: coreumRecipient,
	}

	// send evidences from relayers
	for _, relayer := range relayers {
		txRes, err := contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence)
		require.NoError(t, err)
		thresholdReached, err := event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
		require.NoError(t, err)
		if thresholdReached == strconv.FormatBool(true) {
			break
		}
	}
}

func sendFromCoreumToXRPL(
	ctx context.Context,
	t *testing.T,
	contractClient *coreum.ContractClient,
	relayers []coreum.Relayer,
	senderCoreumAddress sdk.AccAddress,
	coin sdk.Coin,
	xrplRecipientAddress rippledata.Account,
) {
	_, err := contractClient.SendToXRPL(ctx, senderCoreumAddress, xrplRecipientAddress.String(), coin)
	require.NoError(t, err)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)

	acceptedTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}

	// send evidences from relayers
	for _, relayer := range relayers {
		txRes, err := contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(ctx, relayer.CoreumAddress, acceptedTxEvidence)
		require.NoError(t, err)
		thresholdReached, err := event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
		require.NoError(t, err)
		if thresholdReached == strconv.FormatBool(true) {
			break
		}
	}
}

func genRelayers(ctx context.Context, t *testing.T, chains integrationtests.Chains, relayersCount int) []coreum.Relayer {
	relayers := make([]coreum.Relayer, 0)

	for i := 0; i < relayersCount; i++ {
		relayerXRPLSigner := chains.XRPL.GenAccount(ctx, t, 0)
		relayerCoreumAddress := chains.Coreum.GenAccount()
		chains.Coreum.FundAccountWithOptions(ctx, t, relayerCoreumAddress, coreumintegration.BalancesOptions{
			Amount: sdkmath.NewInt(1_000_000),
		})
		relayers = append(relayers, coreum.Relayer{
			CoreumAddress: relayerCoreumAddress,
			XRPLAddress:   relayerXRPLSigner.String(),
			XRPLPubKey:    chains.XRPL.GetSignerPubKey(t, relayerXRPLSigner).String(),
		})
	}
	return relayers
}

func genXRPLTxHash(t *testing.T) string {
	t.Helper()

	hash := rippledata.Hash256{}
	_, err := rand.Read(hash[:])
	require.NoError(t, err)

	return hash.String()
}
