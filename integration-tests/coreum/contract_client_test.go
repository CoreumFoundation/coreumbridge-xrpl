//go:build integrationtests
// +build integrationtests

package coreum_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmoserrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	"github.com/CoreumFoundation/coreum/v4/pkg/client"
	"github.com/CoreumFoundation/coreum/v4/testutil/event"
	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	assetfttypes "github.com/CoreumFoundation/coreum/v4/x/asset/ft/types"
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

	//nolint:lll // the signature sample doesn't require to be splited
	xrplTxSignature = "304502210097099E9AB2C41DA3F672004924B3557D58D101A5745C57C6336C5CF36B59E8F5022003984E50483C921E3FDF45BC7DE4E6ED9D340F0E0CAA6BB1828C647C6665A1CC"
)

var (
	defaultTrustSetLimitAmount = sdkmath.NewInt(10000000000000000)
	xrpMaxHoldingAmount        = sdkmath.NewInt(10000000000000000)

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

func TestDeployAndInstantiateContract(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	assetftClient := assetfttypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 1)

	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()

	xrplBaseFee := uint32(10)
	usedTicketSequenceThreshold := uint32(10)
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		usedTicketSequenceThreshold,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		xrplBaseFee,
	)

	contractCfg, err := contractClient.GetContractConfig(ctx)
	require.NoError(t, err)

	require.Equal(t, coreum.ContractConfig{
		Relayers:                    relayers,
		EvidenceThreshold:           uint32(len(relayers)),
		UsedTicketSequenceThreshold: usedTicketSequenceThreshold,
		TrustSetLimitAmount:         defaultTrustSetLimitAmount,
		BridgeXRPLAddress:           bridgeXRPLAddress,
		BridgeState:                 coreum.BridgeStateActive,
		XRPLBaseFee:                 xrplBaseFee,
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
		Denom:          coreumDenom,
		Issuer:         contractAddress.String(),
		Symbol:         xrp,
		Subunit:        drop,
		Precision:      6,
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
		BridgingFee:      sdkmath.ZeroInt(),
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
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
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
		Amount: sdkmath.NewInt(1_000_000),
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
	inactiveCurrency := "INA"
	activeCurrency := "ACT"
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
		Precision:      xrplPrecision,
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
	currency := "RCR"
	sendingPrecision := int32(15)
	maxHoldingAmount := sdkmath.NewInt(10000)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	// register from the owner
	bridgingFee := sdkmath.NewInt(3)
	_, err := contractClient.RegisterXRPLToken(
		ctx,
		owner,
		issuer,
		currency,
		sendingPrecision,
		maxHoldingAmount,
		bridgingFee,
	)
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
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		wrongXRPLToCoreumTransferEvidence,
	)
	require.True(t, coreum.IsTokenNotRegisteredError(err), err)

	// try to provide the evidence with prohibited recipient
	xrplToCoreumTransferEvidenceWithProhibitedRecipient := xrplToCoreumTransferEvidence
	xrplToCoreumTransferEvidenceWithProhibitedRecipient.Recipient = contractClient.GetContractAddress()
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidenceWithProhibitedRecipient,
	)
	require.True(t, coreum.IsProhibitedRecipientError(err), err)

	// call from first relayer
	txRes, err := contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)
	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.True(t, recipientBalanceRes.Balance.IsZero())
	thresholdReached, err := event.FindStringEventAttribute(
		txRes.Events,
		wasmtypes.ModuleName,
		eventAttributeThresholdReached,
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(false), thresholdReached)

	// call from first relayer one more time
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.True(t, coreum.IsEvidenceAlreadyProvidedError(err), err)

	// call from second relayer
	txRes, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[1].CoreumAddress, xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)
	recipientBalanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredToken.CoreumDenom,
	})
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(
		txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(true), thresholdReached)

	require.NoError(t, err)
	// expect new token on the recipient balance
	// the amount is sent_amount - bridge_fee
	require.Equal(t,
		xrplToCoreumTransferEvidence.Amount.Sub(bridgingFee).String(),
		recipientBalanceRes.Balance.Amount.String())

	// try to push the same evidence
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence,
	)
	require.True(t, coreum.IsOperationAlreadyExecutedError(err), err)

	// assert fees are calculated correctly
	claimFeesAndMakeAssertions(
		ctx,
		t,
		contractClient,
		bankClient,
		relayers,
		bridgingFee,
		sdk.ZeroInt(),
		registeredToken.CoreumDenom,
	)
}

func TestSendFromXRPLToCoreumModuleAccount(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	moduleAccountRecipient := authtypes.NewModuleAddress(govtypes.ModuleName)
	relayers := genRelayers(ctx, t, chains, 2)

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
	_, err := contractClient.RegisterXRPLToken(
		ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount, sdk.ZeroInt(),
	)
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
		Recipient: moduleAccountRecipient,
	}

	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)

	// sending to module account is blocked.
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[1].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.True(t, coreum.IsRecipientBlockedError(err), err)
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
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
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
			name:             "reached_max_holding_amount",
			sendingPrecision: 2,
			maxHoldingAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999", tokenDecimals),
			sendingAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(
				t, "9999.01", tokenDecimals,
			),
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
			currency := "CRC" //nolint:goconst // defining these variables as const for testing is not beneficial.

			// register from the owner
			_, err := contractClient.RegisterXRPLToken(
				ctx, owner, issuer, currency, tt.sendingPrecision, tt.maxHoldingAmount, sdkmath.ZeroInt(),
			)
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
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	registeredXRPToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx,
		xrpl.XRPTokenIssuer.String(),
		xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
	)
	require.NoError(t, err)

	require.Equal(t, coreum.XRPLToken{
		Issuer:           xrpl.XRPTokenIssuer.String(),
		Currency:         xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
		CoreumDenom:      assetfttypes.BuildDenom("drop", contractClient.GetContractAddress()),
		SendingPrecision: 6,
		MaxHoldingAmount: sdkmath.NewInt(10000000000000000),
		State:            coreum.TokenStateEnabled,
		BridgingFee:      sdkmath.ZeroInt(),
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
	txRes, err := contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)
	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredXRPToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.True(t, recipientBalanceRes.Balance.IsZero())
	thresholdReached, err := event.FindStringEventAttribute(
		txRes.Events,
		wasmtypes.ModuleName,
		eventAttributeThresholdReached,
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(false), thresholdReached)

	// call from first relayer one more time
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence,
	)
	require.True(t, coreum.IsEvidenceAlreadyProvidedError(err), err)

	// call from second relayer
	txRes, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[1].CoreumAddress, xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)
	recipientBalanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredXRPToken.CoreumDenom,
	})
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(
		txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
	)
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
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 10)

	// issue asset ft and register it
	sendingPrecision := int32(5)
	tokenDecimals := uint32(5)
	maxHoldingAmount := sdkmath.NewInt(100_000_000_000)
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSender.String(),
		Symbol:        "symbol",
		Subunit:       "subunit",
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
	_, err = contractClient.RegisterCoreumToken(
		ctx, owner, denom, tokenDecimals, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
	)
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
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidenceForNotRegisteredToken,
	)
	require.True(t, coreum.IsTokenNotRegisteredError(err), err)

	// call from first relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence)
	require.NoError(t, err)

	// call from second relayer
	txRes, err := contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[1].CoreumAddress, xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)
	thresholdReached, err := event.FindStringEventAttribute(
		txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
	)
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
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
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
			maxHoldingAmount := sdkmath.NewInt(100_000_000_000)
			issueMsg := &assetfttypes.MsgIssue{
				Issuer:        coreumSender.String(),
				Symbol:        "symbol",
				Subunit:       "subunit",
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
			_, err = contractClient.RegisterCoreumToken(
				ctx, owner, denom, tokenDecimals, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
			)
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
			_, err = contractClient.SendXRPLToCoreumTransferEvidence(
				ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence,
			)
			require.NoError(t, err)

			// call from second relayer
			txRes, err := contractClient.SendXRPLToCoreumTransferEvidence(
				ctx, relayers[1].CoreumAddress, xrplToCoreumTransferEvidence,
			)
			if tt.checkAssetFTError != nil {
				require.True(t, coreum.IsAssetFTStateError(err), err)
				tt.checkAssetFTError(t, err)
				return
			}

			require.NoError(t, err)
			thresholdReached, err := event.FindStringEventAttribute(
				txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
			)
			require.NoError(t, err)
			require.Equal(t, strconv.FormatBool(true), thresholdReached)

			// check recipient balance
			recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: coreumRecipient.String(),
				Denom:   registeredCoreumToken.Denom,
			})
			require.NoError(t, err)
			require.Equal(
				t,
				xrplToCoreumTransferEvidence.Amount.String(),
				recipientBalanceRes.Balance.Amount.String(),
			)

			// check contract balance
			contractBalanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: contractClient.GetContractAddress().String(),
				Denom:   denom,
			})
			require.NoError(t, err)
			require.Equal(
				t,
				coinToSend.Amount.Sub(xrplToCoreumTransferEvidence.Amount).String(),
				contractBalanceRes.Balance.Amount.String(),
			)
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
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
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
				Symbol:        "symbol",
				Subunit:       "subunit",
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

			_, err = contractClient.RegisterCoreumToken(
				ctx, owner, denom, tt.decimals, tt.sendingPrecision, tt.maxHoldingAmount, sdkmath.ZeroInt(),
			)
			require.NoError(t, err)
			registeredCoreumToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
			require.NoError(t, err)

			// if we expect an error the amount is invalid so it won't be accepted from the coreum to XRPL
			if !tt.wantIsAmountSentIsZeroAfterTruncationError {
				coinToSend := sdk.NewCoin(denom, tt.sendingAmount)
				sendFromCoreumToXRPL(
					ctx, t, contractClient, relayers, coreumSenderAddress, coinToSend, xrpl.GenPrivKeyTxSigner().Account(),
				)
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
	usedTicketSequenceThreshold := uint32(5)
	numberOfTicketsToInit := uint32(6)

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 3)
	xrplBaseFee := uint32(10)
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		2,
		usedTicketSequenceThreshold,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		xrplBaseFee,
	)

	// ********** Ticket allocation / Recovery **********
	bridgeXRPLAccountFirstSeqNumber := uint32(1)

	// try to call from not owner
	_, err := contractClient.RecoverTickets(
		ctx, relayers[0].CoreumAddress, bridgeXRPLAccountFirstSeqNumber, &numberOfTicketsToInit,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

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
		Version:         1,
		TicketSequence:  0,
		AccountSequence: bridgeXRPLAccountFirstSeqNumber,
		Signatures:      make([]coreum.Signature, 0),
		OperationType: coreum.OperationType{
			AllocateTickets: &coreum.OperationTypeAllocateTickets{
				Number: numberOfTicketsToInit,
			},
		},
		XRPLBaseFee: xrplBaseFee,
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
	_, err = contractClient.SaveSignature(
		ctx,
		owner,
		bridgeXRPLAccountFirstSeqNumber,
		ticketsAllocationOperation.Version,
		signerItem1.TxnSignature.String(),
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try to send with incorrect operation ID
	_, err = contractClient.SaveSignature(
		ctx, relayers[0].CoreumAddress, uint32(999), ticketsAllocationOperation.Version, signerItem1.TxnSignature.String(),
	)
	require.True(t, coreum.IsPendingOperationNotFoundError(err), err)

	// send from first relayer
	_, err = contractClient.SaveSignature(
		ctx,
		relayers[0].CoreumAddress,
		bridgeXRPLAccountFirstSeqNumber,
		ticketsAllocationOperation.Version,
		signerItem1.TxnSignature.String(),
	)
	require.NoError(t, err)

	// try to send from the same relayer one more time
	_, err = contractClient.SaveSignature(
		ctx,
		relayers[0].CoreumAddress,
		bridgeXRPLAccountFirstSeqNumber,
		ticketsAllocationOperation.Version,
		signerItem1.TxnSignature.String(),
	)
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
	_, err = contractClient.SaveSignature(
		ctx,
		relayers[1].CoreumAddress,
		bridgeXRPLAccountFirstSeqNumber,
		ticketsAllocationOperation.Version,
		signerItem2.TxnSignature.String(),
	)
	require.NoError(t, err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	ticketsAllocationOperation = pendingOperations[0]
	require.Equal(t, coreum.Operation{
		Version:         1,
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
		XRPLBaseFee: xrplBaseFee,
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
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
		ctx, relayers[0].CoreumAddress, invalidRejectedTxEvidence,
	)
	require.True(t, coreum.IsPendingOperationNotFoundError(err), err)

	// try to send with not existing ticket
	invalidRejectedTxEvidence = rejectedTxEvidence
	invalidRejectedTxEvidence.AccountSequence = nil
	invalidRejectedTxEvidence.TicketSequence = lo.ToPtr(uint32(999))
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
		ctx, relayers[0].CoreumAddress, invalidRejectedTxEvidence,
	)
	require.True(t, coreum.IsPendingOperationNotFoundError(err), err)

	// try to send from not relayer
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, owner, rejectedTxEvidence)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// send evidence from first relayer
	txRes, err := contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
		ctx, relayers[0].CoreumAddress, rejectedTxEvidence,
	)
	require.NoError(t, err)
	thresholdReached, err := event.FindStringEventAttribute(
		txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(false), thresholdReached)

	// try to send evidence from second relayer one more time
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
		ctx, relayers[0].CoreumAddress, rejectedTxEvidence,
	)
	require.True(t, coreum.IsEvidenceAlreadyProvidedError(err), err)

	// send evidence from second relayer
	txRes, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
		ctx, relayers[1].CoreumAddress, rejectedTxEvidence,
	)
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(
		txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(true), thresholdReached)

	// try to send the evidence one more time
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
		ctx, relayers[0].CoreumAddress, rejectedTxEvidence,
	)
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
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
		ctx, relayers[0].CoreumAddress, invalidTxEvidence,
	)
	require.NoError(t, err)
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
		ctx, relayers[1].CoreumAddress, invalidTxEvidence,
	)
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
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
		ctx, relayers[0].CoreumAddress, invalidTxEvidence,
	)
	require.NoError(t, err)
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
		ctx, relayers[1].CoreumAddress, invalidTxEvidence,
	)
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
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
		ctx, relayers[0].CoreumAddress, invalidAcceptedTxEvidence,
	)
	require.True(t, coreum.IsOperationAlreadyExecutedError(err), err)

	// send evidence from first relayer
	txRes, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
		ctx, relayers[0].CoreumAddress, acceptedTxEvidence,
	)
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(
		txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(false), thresholdReached)

	// send evidence from second relayer
	txRes, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
		ctx, relayers[1].CoreumAddress, acceptedTxEvidence,
	)
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(
		txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
	)
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
	currency := "CRN"
	sendingPrecision := int32(15)
	maxHoldingAmount := sdkmath.NewInt(1_000_000_000)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 5)

	// register new token
	_, err := contractClient.RegisterXRPLToken(
		ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	// activate token
	registeredXRPLOriginatedToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	activateXRPLToken(ctx, t, contractClient, relayers, issuer, currency)

	amountToSendFromXRPLToCoreum := sdkmath.NewInt(1_000_100)
	sendFromXRPLToCoreum(
		ctx, t, contractClient, relayers, issuer, currency, amountToSendFromXRPLToCoreum, coreumSenderAddress,
	)
	// validate that the amount is received
	balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(t, amountToSendFromXRPLToCoreum.String(), balanceRes.Balance.Amount.String())

	amountToSend := sdkmath.NewInt(1_000_000)

	// try to send more than account has
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSendFromXRPLToCoreum.AddRaw(1)),
		nil,
	)
	require.ErrorContains(t, err, cosmoserrors.ErrInsufficientFunds.Error())

	// try to send with invalid recipient
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		"invalid",
		sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSend),
		nil,
	)
	require.True(t, coreum.IsInvalidXRPLAddressError(err), err)

	contractCfg, err := contractClient.GetContractConfig(ctx)
	require.NoError(t, err)
	// try to send to XRPL bridge account address
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		contractCfg.BridgeXRPLAddress,
		sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSend),
		nil,
	)
	require.True(t, coreum.IsProhibitedRecipientError(err), err)

	// try to send with not registered token
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(chains.Coreum.ChainSettings.Denom, sdkmath.NewInt(1)),
		nil,
	)
	require.True(t, coreum.IsTokenNotRegisteredError(err), err)

	// send valid amount and validate the state
	coreumSenderBalanceBeforeRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSend),
		nil,
	)
	require.NoError(t, err)
	// check the remaining balance
	coreumSenderBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(
		t,
		coreumSenderBalanceBeforeRes.Balance.Amount.Sub(amountToSend).String(),
		coreumSenderBalanceAfterRes.Balance.Amount.String(),
	)

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
	_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
		ctx,
		relayers[0].CoreumAddress,
		acceptedTxEvidence,
	)
	require.NoError(t, err)

	// send from second relayer
	_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
		ctx,
		relayers[1].CoreumAddress,
		acceptedTxEvidence,
	)
	require.NoError(t, err)

	// check pending operations
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// use all available tickets
	tickets, err := contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	for i := 0; i < len(tickets)-1; i++ {
		_, err = contractClient.SendToXRPL(
			ctx,
			coreumSenderAddress,
			xrplRecipientAddress.String(),
			sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, sdkmath.NewInt(1)),
			nil,
		)
		require.NoError(t, err)
	}

	// try to use last (protected) ticket
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, sdkmath.NewInt(1)),
		nil,
	)
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
		uint32(len(relayers)),
		50,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
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
			_, err := contractClient.RegisterXRPLToken(
				ctx, owner, issuer, currency, tt.sendingPrecision, tt.maxHoldingAmount, sdkmath.ZeroInt(),
			)
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

			_, err = contractClient.SendToXRPL(
				ctx,
				coreumSenderAddress,
				xrplRecipient.String(),
				sdk.NewCoin(registeredXRPLToken.CoreumDenom, tt.sendingAmount),
				nil,
			)
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
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	registeredXRPToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrpl.XRPTokenIssuer.String(), xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
	)
	require.NoError(t, err)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 5)

	amountToSendFromXRPLToCoreum := sdkmath.NewInt(1_000_100)
	sendFromXRPLToCoreum(
		ctx,
		t,
		contractClient,
		relayers,
		registeredXRPToken.Issuer,
		registeredXRPToken.Currency,
		amountToSendFromXRPLToCoreum,
		coreumSenderAddress,
	)
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
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPToken.CoreumDenom, amountToSend),
		nil,
	)
	require.NoError(t, err)
	// check the remaining balance
	coreumSenderBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(
		t,
		coreumSenderBalanceBeforeRes.Balance.Amount.Sub(amountToSend).String(),
		coreumSenderBalanceAfterRes.Balance.Amount.String(),
	)

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
	_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
		ctx, relayers[0].CoreumAddress, acceptedTxEvidence,
	)
	require.NoError(t, err)

	// send from second relayer
	_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
		ctx, relayers[1].CoreumAddress, acceptedTxEvidence,
	)
	require.NoError(t, err)

	// check pending operations
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)
}

func TestSendFromCoreumXRPLOriginatedTokenWithDeliverAmount(t *testing.T) {
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
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	sendingPrecision := int32(3)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 30)
	issuer := chains.XRPL.GenAccount(ctx, t, 0).String()
	currency := "SLO"
	bridgingFee := sdkmath.NewInt(10)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 15)

	// register new XRPL token and test sending

	_, err := contractClient.RegisterXRPLToken(
		ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount, bridgingFee,
	)
	require.NoError(t, err)

	registeredXRPLOriginatedToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	activateXRPLToken(ctx, t, contractClient, relayers, issuer, currency)

	xrplTokenAmountToSendFromXRPLToCoreum := sdkmath.NewIntWithDecimal(1, 15).Add(bridgingFee)
	sendFromXRPLToCoreum(
		ctx, t, contractClient, relayers, issuer, currency, xrplTokenAmountToSendFromXRPLToCoreum, coreumSenderAddress,
	)
	// validate that the XRPL token amount is received
	coreumSenderBalance, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(
		t, xrplTokenAmountToSendFromXRPLToCoreum.Sub(bridgingFee).String(), coreumSenderBalance.Balance.Amount.String(),
	)

	amountToSendBack := sdkmath.NewIntWithDecimal(1, 14)

	// try to send amount greater than max amount
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSendBack),
		lo.ToPtr(amountToSendBack.AddRaw(1000)),
	)
	require.True(t, coreum.IsInvalidDeliverAmountError(err), err)

	coreumSenderBalanceBeforeRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)

	// send amount equal to max amount
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSendBack.Add(bridgingFee)),
		lo.ToPtr(amountToSendBack),
	)
	require.NoError(t, err)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	require.Equal(t, coreum.OperationTypeCoreumToXRPLTransfer{
		Issuer:   issuer,
		Currency: currency,
		// the amounts are truncated
		Amount:    amountToSendBack,
		MaxAmount: &amountToSendBack,
		Recipient: xrplRecipientAddress.String(),
	}, *operationType)

	// reject the operation
	rejectedTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultRejected,
		},
	}
	for _, relayer := range relayers {
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
			ctx, relayer.CoreumAddress, rejectedTxEvidence,
		)
		require.NoError(t, err)
	}

	// claim refunds
	pendingRefunds, err := contractClient.GetPendingRefunds(ctx, coreumSenderAddress)
	require.NoError(t, err)
	require.Len(t, pendingRefunds, 1)
	require.Equal(t, pendingRefunds[0].XRPLTxHash, rejectedTxEvidence.TxHash)
	_, err = contractClient.ClaimRefund(ctx, coreumSenderAddress, pendingRefunds[0].ID)
	require.NoError(t, err)

	coreumSenderBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)

	// check that only the bridging fee is charged
	require.Equal(
		t,
		bridgingFee.String(),
		coreumSenderBalanceBeforeRes.Balance.Amount.Sub(coreumSenderBalanceAfterRes.Balance.Amount).String(),
	)

	coreumSenderBalanceBeforeRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)

	// send amount with low deliver amount
	deliverAmount := amountToSendBack.QuoRaw(2)
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSendBack.Add(bridgingFee)),
		&deliverAmount,
	)
	require.NoError(t, err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation = pendingOperations[0]
	operationType = operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	require.Equal(t, coreum.OperationTypeCoreumToXRPLTransfer{
		Issuer:   issuer,
		Currency: currency,
		// the amounts are truncated
		Amount:    deliverAmount,
		MaxAmount: &amountToSendBack,
		Recipient: xrplRecipientAddress.String(),
	}, *operationType)

	// accep the operation
	acceptedTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}
	for _, relayer := range relayers {
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
			ctx, relayer.CoreumAddress, acceptedTxEvidence,
		)
		require.NoError(t, err)
	}

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	coreumSenderBalanceAfterRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)

	// check that changed full amount plus bridging fee
	require.Equal(
		t,
		coreumSenderBalanceBeforeRes.Balance.Amount.Sub(amountToSendBack).Sub(bridgingFee).String(),
		coreumSenderBalanceAfterRes.Balance.Amount.String(),
	)
}

func TestSendFromCoreumCoreumOriginatedTokenWithDeliverAmount(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewInt(1_000_000)),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	relayers := genRelayers(ctx, t, chains, 2)
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

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 15)

	decimals := 6
	sendingPrecision := int32(3)
	maxHoldingAmount := sdkmath.NewInt(1_000_000_000)
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSenderAddress.String(),
		Symbol:        "symbol",
		Subunit:       "subunit",
		Precision:     uint32(decimals),
		InitialAmount: maxHoldingAmount,
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)
	coreumTokenDenom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)
	_, err = contractClient.RegisterCoreumToken(
		ctx, owner, coreumTokenDenom, uint32(decimals), sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err := contractClient.GetCoreumTokenByDenom(ctx, coreumTokenDenom)
	require.NoError(t, err)

	amountToSend := sdkmath.NewInt(1000)
	// try to send with deliverAmount
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSend),
		lo.ToPtr(amountToSend),
	)
	require.True(t, coreum.IsDeliverAmountIsProhibitedError(err), err)
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
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 10)

	// issue asset ft and register it
	sendingPrecision1 := int32(5)
	tokenDecimals1 := uint32(5)
	maxHoldingAmount1 := sdkmath.NewInt(100_000_000_000)
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
	_, err = contractClient.RegisterCoreumToken(
		ctx, owner, denom1, tokenDecimals1, sendingPrecision1, maxHoldingAmount1, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	registeredCoreumOriginatedToken1, err := contractClient.GetCoreumTokenByDenom(ctx, denom1)
	require.NoError(t, err)

	// register coreum (udevcore) denom
	denom2 := chains.Coreum.ChainSettings.Denom
	sendingPrecision2 := int32(6)
	tokenDecimals2 := uint32(6)
	maxHoldingAmount2 := sdkmath.NewInt(1_000_000_000)
	_, err = contractClient.RegisterCoreumToken(
		ctx, owner, denom2, tokenDecimals2, sendingPrecision2, maxHoldingAmount2, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	registeredCoreumOriginatedToken2, err := contractClient.GetCoreumTokenByDenom(ctx, denom2)
	require.NoError(t, err)

	// issue asset ft but not register it
	issueMsg = &assetfttypes.MsgIssue{
		Issuer:        coreumSenderAddress.String(),
		Symbol:        "notreg",
		Subunit:       "notreg",
		Precision:     uint32(16), // token decimals in terms of the contract
		InitialAmount: sdkmath.NewInt(100_000_000_000),
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
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(notRegisteredTokenDenom, sdkmath.NewInt(1)),
		nil,
	)
	require.True(t, coreum.IsTokenNotRegisteredError(err), err)

	// ********** test token1 (assetft) **********

	amountToSendOfToken1 := sdkmath.NewInt(1_001_001)

	// send valid amount and validate the state
	coreumSenderBalanceBeforeRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredCoreumOriginatedToken1.Denom,
	})
	require.NoError(t, err)
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredCoreumOriginatedToken1.Denom, amountToSendOfToken1),
		nil,
	)
	require.NoError(t, err)
	// check the remaining balance
	coreumSenderBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredCoreumOriginatedToken1.Denom,
	})
	require.NoError(t, err)
	require.Equal(
		t,
		coreumSenderBalanceBeforeRes.Balance.Amount.Sub(amountToSendOfToken1).String(),
		coreumSenderBalanceAfterRes.Balance.Amount.String(),
	)

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
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
			ctx, relayer.CoreumAddress, acceptedTxEvidence,
		)
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
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredCoreumOriginatedToken2.Denom, amountToSendOfToken2),
		nil,
	)
	require.NoError(t, err)
	// check the remaining balance
	coreumSenderBalanceAfterRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredCoreumOriginatedToken2.Denom,
	})
	require.NoError(t, err)
	require.True(t, coreumSenderBalanceBeforeRes.Balance.Amount.
		Sub(amountToSendOfToken2).
		GT(coreumSenderBalanceAfterRes.Balance.Amount),
	)

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
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
			ctx, relayer.CoreumAddress, acceptedTxEvidence,
		)
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
		_, err = contractClient.SendToXRPL(
			ctx,
			coreumSenderAddress,
			xrplRecipientAddress.String(),
			sdk.NewCoin(registeredCoreumOriginatedToken1.Denom, sdkmath.NewInt(1)),
			nil,
		)
		require.NoError(t, err)
	}

	// try to use last (protected) ticket
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredCoreumOriginatedToken1.Denom, sdkmath.NewInt(1)),
		nil,
	)
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
		uint32(len(relayers)),
		50,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
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
				Symbol:        "symbol",
				Subunit:       "subunit",
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

			_, err = contractClient.RegisterCoreumToken(
				ctx, owner, denom, tt.decimals, tt.sendingPrecision, tt.maxHoldingAmount, sdkmath.ZeroInt(),
			)
			require.NoError(t, err)
			registeredCoreumToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
			require.NoError(t, err)

			_, err = contractClient.SendToXRPL(
				ctx,
				coreumSenderAddress,
				xrplRecipient.String(),
				sdk.NewCoin(registeredCoreumToken.Denom, tt.sendingAmount),
				nil,
			)
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
				if operationType != nil &&
					operationType.Issuer == bridgeXRPLAddress &&
					operationType.Currency == registeredCoreumToken.XRPLCurrency {
					found = true
					require.Equal(t, tt.wantReceivedAmount.String(), operationType.Amount.String())
				}
			}
			require.True(t, found)
		})
	}
}

func TestSendCoreumOriginatedTokenWithBurningRateAndSendingCommissionFromCoreumToXRPLAndBack(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	coreumIssuerAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumIssuerAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewInt(10_000_000)),
	})

	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})

	coreumRecipient := chains.Coreum.GenAccount()

	xrplRecipientAddress := xrpl.GenPrivKeyTxSigner().Account()
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
	sendingPrecision := int32(5)
	tokenDecimals := uint32(5)
	maxHoldingAmount := sdkmath.NewInt(100_000_000_000)
	msgIssue := &assetfttypes.MsgIssue{
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
		msgIssue,
	)
	require.NoError(t, err)
	denom := assetfttypes.BuildDenom(msgIssue.Subunit, coreumIssuerAddress)
	_, err = contractClient.RegisterCoreumToken(
		ctx, owner, denom, tokenDecimals, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	registeredToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
	require.NoError(t, err)

	// send coins to sender to test the commission

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

	// ********** Coreum to XRPL **********

	amountToSend := sdkmath.NewInt(1_000_000)

	// send the amount and validate the state
	coreumSenderBalanceBeforeRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredToken.Denom,
	})
	require.NoError(t, err)
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredToken.Denom, amountToSend),
		nil,
	)
	require.NoError(t, err)
	// check the remaining balance
	coreumSenderBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredToken.Denom,
	})
	require.NoError(t, err)
	// amountToSend + burning rate + sending commission
	spentAmount := amountToSend.
		Add(amountToSend.ToLegacyDec().Mul(msgIssue.BurnRate.Add(msgIssue.SendCommissionRate)).TruncateInt())
	require.Equal(
		t,
		coreumSenderBalanceBeforeRes.Balance.Amount.Sub(spentAmount).String(),
		coreumSenderBalanceAfterRes.Balance.Amount.String(),
	)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	// XRPL DECIMALS (15) - TOKEN DECIMALS (5) = 10
	require.Equal(t, operationType.Amount, amountToSend.Mul(sdk.NewInt(10_000_000_000)))
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
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
			ctx, relayer.CoreumAddress, acceptedTxEvidence,
		)
		require.NoError(t, err)
	}

	// check pending operations
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// ********** XRPL to Coreum **********

	// XRPL DECIMALS (15) - TOKEN DECIMALS (5) = 10
	amountToSendBack := amountToSend.Mul(sdkmath.NewIntWithDecimal(1, 10))

	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    bridgeXRPLAddress,
		Currency:  registeredToken.XRPLCurrency,
		Amount:    amountToSendBack,
		Recipient: coreumRecipient,
	}

	bridgeContractBalanceBeforeRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: contractClient.GetContractAddress().String(),
		Denom:   registeredToken.Denom,
	})
	require.NoError(t, err)

	// send from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SendXRPLToCoreumTransferEvidence(
			ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence,
		)
		require.NoError(t, err)
	}

	// check the remaining balance
	bridgeContractBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: contractClient.GetContractAddress().String(),
		Denom:   registeredToken.Denom,
	})
	require.NoError(t, err)
	require.Equal(
		t,
		bridgeContractBalanceBeforeRes.Balance.Amount.Sub(amountToSend).String(),
		bridgeContractBalanceAfterRes.Balance.Amount.String(),
	)
	require.Equal(t, sdkmath.ZeroInt().String(), bridgeContractBalanceAfterRes.Balance.Amount.String())

	coreumRecipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredToken.Denom,
	})
	require.NoError(t, err)
	require.Equal(
		t,
		coreumRecipientBalanceRes.Balance.Amount.String(),
		bridgeContractBalanceBeforeRes.Balance.Amount.String(),
	)
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
	currency := "CRN"
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

// TestBridgingFeeForXRPLOrginatedTokens tests that corrects fees are calculated, deducted and
// are collected by relayers.
//
//nolint:tparallel // the test is parallel, but test cases are not
func TestBridgingFeeForXRPLOrginatedTokens(t *testing.T) {
	t.Parallel()

	var (
		tokenDecimals        = int64(15)
		highMaxHoldingAmount = integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)
	)

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 2)
	bridgeAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		bridgeAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	type testCase struct {
		name                    string
		bridgingFee             string
		sendingPrecision        int32
		sendingAmountFromXRPL   string
		receivedOnCoreum        string
		collectedFeeFromXRPL    string
		sendingAmountFromCoreum string
		receivedOnXRPL          string
		collectedFeeFromCoreum  string
		expectErrorXRPL         bool
		expectErrorSendToCoreum bool
	}

	tests := []testCase{
		{
			name:                    "zero_bridge_fee",
			sendingPrecision:        2,
			bridgingFee:             "0",
			sendingAmountFromXRPL:   "9999999999.15",
			receivedOnCoreum:        "9999999999.15",
			collectedFeeFromXRPL:    "0",
			sendingAmountFromCoreum: "9999999999.15",
			receivedOnXRPL:          "9999999999.15",
			collectedFeeFromCoreum:  "0",
		},
		{
			name:                    "4_bridge_fee",
			sendingPrecision:        2,
			bridgingFee:             "4",
			sendingAmountFromXRPL:   "1008",
			receivedOnCoreum:        "1004",
			collectedFeeFromXRPL:    "4",
			sendingAmountFromCoreum: "1004",
			receivedOnXRPL:          "1000",
			collectedFeeFromCoreum:  "4",
		},
		{
			name:                    "bridge_fee_with_precision",
			sendingPrecision:        2,
			bridgingFee:             "0.04",
			sendingAmountFromXRPL:   "1000",
			receivedOnCoreum:        "999.96",
			collectedFeeFromXRPL:    "0.04",
			sendingAmountFromCoreum: "999.96",
			receivedOnXRPL:          "999.92",
			collectedFeeFromCoreum:  "0.04",
		},
		{
			name:                    "bridge_fee_with_precision_and_truncation",
			sendingPrecision:        2,
			bridgingFee:             "0.04",
			sendingAmountFromXRPL:   "1000.222",
			receivedOnCoreum:        "1000.18",
			collectedFeeFromXRPL:    "0.042",
			sendingAmountFromCoreum: "1000.127",
			receivedOnXRPL:          "1000.08",
			collectedFeeFromCoreum:  "0.047",
		},
		{
			name:                    "bridge_fee_less_than_sending_precision",
			sendingPrecision:        1,
			bridgingFee:             "0.00001",
			sendingAmountFromXRPL:   "1000",
			receivedOnCoreum:        "999.9",
			collectedFeeFromXRPL:    "0.1",
			sendingAmountFromCoreum: "999.9",
			receivedOnXRPL:          "999.8",
			collectedFeeFromCoreum:  "0.1",
		},
		{
			name:                    "low_amount_send_from_xrpl",
			sendingPrecision:        2,
			bridgingFee:             "0.001",
			sendingAmountFromXRPL:   "0.01",
			expectErrorSendToCoreum: true,
		},
		{
			name:                    "low_amount_send_from_coreum",
			sendingPrecision:        2,
			bridgingFee:             "0.001",
			sendingAmountFromXRPL:   "1",
			receivedOnCoreum:        "0.99",
			collectedFeeFromXRPL:    "0.01",
			sendingAmountFromCoreum: "0.01",
			expectErrorXRPL:         true,
		},
	}

	stringToSDKInt := func(stringValue string) sdkmath.Int {
		return integrationtests.ConvertStringWithDecimalsToSDKInt(t, stringValue, tokenDecimals)
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
			xrplCurrency := "CRC"

			// register from the owner
			_, err := contractClient.RegisterXRPLToken(
				ctx,
				owner,
				issuer,
				xrplCurrency,
				tt.sendingPrecision,
				highMaxHoldingAmount,
				stringToSDKInt(tt.bridgingFee),
			)
			require.NoError(t, err)
			registeredXRPLToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, xrplCurrency)
			require.NoError(t, err)

			// activate token
			activateXRPLToken(ctx, t, contractClient, relayers, issuerAcc.String(), xrplCurrency)

			// create an evidence
			coreumRecipient := chains.Coreum.GenAccount()
			xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
				TxHash:    genXRPLTxHash(t),
				Issuer:    issuerAcc.String(),
				Currency:  xrplCurrency,
				Amount:    stringToSDKInt(tt.sendingAmountFromXRPL),
				Recipient: coreumRecipient,
			}

			// call from all relayers
			for _, relayer := range relayers {
				_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence)
				if tt.expectErrorSendToCoreum {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
			}

			balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: coreumRecipient.String(),
				Denom:   registeredXRPLToken.CoreumDenom,
			})
			require.NoError(t, err)
			require.Equal(t, stringToSDKInt(tt.receivedOnCoreum).String(), balanceRes.Balance.Amount.String())

			// assert fee collection
			claimFeesAndMakeAssertions(
				ctx,
				t,
				contractClient,
				bankClient,
				relayers,
				stringToSDKInt(tt.collectedFeeFromXRPL),
				sdk.ZeroInt(),
				registeredXRPLToken.CoreumDenom,
			)

			// send back to xrpl
			chains.Coreum.FundAccountWithOptions(ctx, t, coreumRecipient, coreumintegration.BalancesOptions{
				Amount: sdk.NewInt(1_000_000),
			})
			xrplRecipient := xrpl.GenPrivKeyTxSigner().Account()
			_, err = contractClient.SendToXRPL(
				ctx,
				coreumRecipient,
				xrplRecipient.String(),
				sdk.NewCoin(
					registeredXRPLToken.CoreumDenom,
					stringToSDKInt(tt.sendingAmountFromCoreum),
				),
				nil,
			)
			if tt.expectErrorXRPL {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			pendingOperations, err := contractClient.GetPendingOperations(ctx)
			require.NoError(t, err)
			found := false
			for _, operation := range pendingOperations {
				operationType := operation.OperationType.CoreumToXRPLTransfer
				if operationType != nil &&
					operationType.Issuer == issuerAcc.String() &&
					operationType.Currency == xrplCurrency {
					found = true
					require.Equal(t, stringToSDKInt(tt.receivedOnXRPL).String(), operationType.Amount.String())
				}
			}
			require.True(t, found)

			// assert fees again for bridging back
			claimFeesAndMakeAssertions(
				ctx,
				t,
				contractClient,
				bankClient,
				relayers,
				stringToSDKInt(tt.collectedFeeFromCoreum),
				sdk.ZeroInt(),
				registeredXRPLToken.CoreumDenom,
			)
		})
	}
}

// TestBridgingFeeForCoreumOriginatedTokens tests that corrects fees are calculated, deducted and
// are collected by relayers.
//
//nolint:tparallel // the test is parallel, but test cases are not
func TestBridgingFeeForCoreumOriginatedTokens(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	maxHoldingAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)
	tokenDecimals := uint32(15)
	relayers := genRelayers(ctx, t, chains, 2)
	bridgeAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		bridgeAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	type testCase struct {
		name             string
		bridgingFee      string
		sendingPrecision int32
		sendingAmount    string
		receivedAmount   string
		collectedFee     string
	}

	tests := []testCase{
		{
			name:             "zero_bridging_fee",
			sendingPrecision: 2,
			bridgingFee:      "0",
			sendingAmount:    "9999999999.15",
			receivedAmount:   "9999999999.15",
			collectedFee:     "0",
		},
		{
			name:             "4_bridging_fee",
			sendingPrecision: 2,
			bridgingFee:      "4",
			sendingAmount:    "1004",
			receivedAmount:   "1000",
			collectedFee:     "4",
		},
		{
			name:             "bridge_fee_with_precision",
			sendingPrecision: 2,
			bridgingFee:      "0.04",
			sendingAmount:    "999.96",
			receivedAmount:   "999.92",
			collectedFee:     "0.04",
		},
		{
			name:             "bridge_fee_with_precision_and_truncation",
			sendingPrecision: 2,
			bridgingFee:      "0.04",
			sendingAmount:    "1000.127",
			receivedAmount:   "1000.08",
			collectedFee:     "0.047",
		},
		{
			name:             "bridge_fee_less_than_sending_precision",
			sendingPrecision: 1,
			bridgingFee:      "0.00001",
			sendingAmount:    "999.9",
			receivedAmount:   "999.8",
			collectedFee:     "0.1",
		},
	}

	stringToSDKInt := func(stringValue string) sdkmath.Int {
		return integrationtests.ConvertStringWithDecimalsToSDKInt(t, stringValue, int64(tokenDecimals))
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
				Symbol:        "symbol",
				Subunit:       "subunit",
				Precision:     tokenDecimals,              // token decimals in terms of the contract
				InitialAmount: maxHoldingAmount.MulRaw(2), // twice more to be able to send more than max
			}
			_, err := client.BroadcastTx(
				ctx,
				chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
				chains.Coreum.TxFactory().WithSimulateAndExecute(true),
				issueMsg,
			)
			require.NoError(t, err)
			denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)

			xrplRecipient := xrpl.GenPrivKeyTxSigner().Account()
			_, err = contractClient.RegisterCoreumToken(
				ctx,
				owner,
				denom,
				tokenDecimals,
				tt.sendingPrecision,
				maxHoldingAmount,
				stringToSDKInt(tt.bridgingFee),
			)
			require.NoError(t, err)
			registeredCoreumToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
			require.NoError(t, err)

			_, err = contractClient.SendToXRPL(
				ctx,
				coreumSenderAddress,
				xrplRecipient.String(),
				sdk.NewCoin(registeredCoreumToken.Denom, stringToSDKInt(tt.sendingAmount)),
				nil,
			)

			require.NoError(t, err)

			pendingOperations, err := contractClient.GetPendingOperations(ctx)
			require.NoError(t, err)
			found := false
			for _, operation := range pendingOperations {
				operationType := operation.OperationType.CoreumToXRPLTransfer
				if operationType != nil &&
					operationType.Issuer == bridgeAddress &&
					operationType.Currency == registeredCoreumToken.XRPLCurrency {
					found = true
					require.Equal(t, stringToSDKInt(tt.receivedAmount).String(), operationType.Amount.String())
				}
			}
			require.True(t, found)

			// assert fee collection
			claimFeesAndMakeAssertions(
				ctx,
				t,
				contractClient,
				bankClient,
				relayers,
				stringToSDKInt(tt.collectedFee),
				sdk.ZeroInt(),
				registeredCoreumToken.Denom,
			)
		})
	}
}

// TestFeeCalculations_MultipleAssetsAndPartialClaim tests that corrects fees are calculated, deducted and
// are collected by relayers.
func TestFeeCalculations_MultipleAssetsAndPartialClaim(t *testing.T) {
	t.Parallel()

	var (
		sendingAmount    = sdkmath.NewInt(1000_000)
		maxHoldingAmount = sdkmath.NewInt(1000_000_000)
	)

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 3)
	bridgeAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		bridgeAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	assets := []struct {
		name            string
		bridgingFee     sdkmath.Int
		registeredToken coreum.XRPLToken
	}{
		{
			name:        "asset 1",
			bridgingFee: sdkmath.NewInt(4000),
		},
		{
			name:        "asset 2",
			bridgingFee: sdkmath.NewInt(4500),
		},
		{
			name:        "asset 3",
			bridgingFee: sdkmath.NewInt(2221),
		},
	}

	for index, asset := range assets {
		// fund owner to cover registration fee
		chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
			Amount: issueFee.Amount,
		})

		issuerAcc := xrpl.GenPrivKeyTxSigner().Account()
		issuer := issuerAcc.String()
		xrplCurrency := "CRC"

		// register from the owner
		_, err := contractClient.RegisterXRPLToken(
			ctx,
			owner,
			issuer,
			xrplCurrency,
			15,
			maxHoldingAmount,
			asset.bridgingFee,
		)
		require.NoError(t, err)
		registeredXRPLToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, xrplCurrency)
		require.NoError(t, err)
		assets[index].registeredToken = registeredXRPLToken

		// activate token
		activateXRPLToken(ctx, t, contractClient, relayers, issuerAcc.String(), xrplCurrency)

		// create an evidence
		coreumRecipient := chains.Coreum.GenAccount()
		xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
			TxHash:    genXRPLTxHash(t),
			Issuer:    issuerAcc.String(),
			Currency:  xrplCurrency,
			Amount:    sendingAmount,
			Recipient: coreumRecipient,
		}

		// call from all relayers
		for _, relayer := range relayers {
			_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence)
			require.NoError(t, err)
		}

		balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
			Address: coreumRecipient.String(),
			Denom:   registeredXRPLToken.CoreumDenom,
		})
		require.NoError(t, err)
		require.Equal(t, sendingAmount.Sub(asset.bridgingFee).String(), balanceRes.Balance.Amount.String())
	}

	// assert fees are calculated correctly
	for _, relayer := range relayers {
		fees, err := contractClient.GetFeesCollected(ctx, relayer.CoreumAddress)
		require.NoError(t, err)
		require.Len(t, fees, len(assets))
		for _, fee := range fees {
			found := false
			for _, asset := range assets {
				if asset.registeredToken.CoreumDenom == fee.Denom {
					found = true
					assert.EqualValues(t, fee.Amount.String(), asset.bridgingFee.Quo(sdk.NewInt(int64(len(relayers)))).String())
					break
				}
			}

			require.True(t, found)
		}
	}

	// partial fee claiming
	for _, relayer := range relayers {
		initialFees, err := contractClient.GetFeesCollected(ctx, relayer.CoreumAddress)
		require.NoError(t, err)

		// claim one third of the fees
		oneThirdOfFees := initialFees.QuoInt(sdk.NewInt(3))
		_, err = contractClient.ClaimRelayerFees(ctx, relayer.CoreumAddress, oneThirdOfFees)
		require.NoError(t, err)
		allBalances, err := bankClient.AllBalances(ctx, &banktypes.QueryAllBalancesRequest{
			Address: relayer.CoreumAddress.String(),
		})
		require.NoError(t, err)
		for _, coin := range oneThirdOfFees {
			require.Equal(t, coin.Amount.String(), allBalances.Balances.AmountOf(coin.Denom).String())
		}

		// assert remainder is correct
		remainderFees := initialFees.Sub(oneThirdOfFees...)
		fees, err := contractClient.GetFeesCollected(ctx, relayer.CoreumAddress)
		require.NoError(t, err)
		require.EqualValues(t, remainderFees.String(), fees.String())

		// claim remainder of fees
		_, err = contractClient.ClaimRelayerFees(ctx, relayer.CoreumAddress, remainderFees)
		require.NoError(t, err)

		allBalances, err = bankClient.AllBalances(ctx, &banktypes.QueryAllBalancesRequest{
			Address: relayer.CoreumAddress.String(),
		})
		require.NoError(t, err)
		for _, coin := range initialFees {
			require.Equal(t, coin.Amount.String(), allBalances.Balances.AmountOf(coin.Denom).String())
		}

		// assert no fees are remaining
		fees, err = contractClient.GetFeesCollected(ctx, relayer.CoreumAddress)
		require.NoError(t, err)
		require.Empty(t, fees)
	}
}

// TestFeeCalculations_MultipleAssetsAndPartialClaim tests that corrects remainder fees are calculated, deducted and
// are collected by relayers.
func TestFeeCalculations_FeeRemainder(t *testing.T) {
	t.Parallel()

	maxHoldingAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 2)
	bridgeAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		bridgeAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	// fund owner to cover registration fee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	issuerAcc := xrpl.GenPrivKeyTxSigner().Account()
	issuer := issuerAcc.String()
	xrplCurrency := "CRC"

	tokenDecimals := int64(15)
	// register from the owner
	bridgingFee := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.3", tokenDecimals)
	_, err := contractClient.RegisterXRPLToken(
		ctx,
		owner,
		issuer,
		xrplCurrency,
		1,
		maxHoldingAmount,
		bridgingFee,
	)
	require.NoError(t, err)
	registeredXRPLToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, xrplCurrency)
	require.NoError(t, err)

	// activate token
	activateXRPLToken(ctx, t, contractClient, relayers, issuerAcc.String(), xrplCurrency)

	// create an evidence
	sendingAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1.36", tokenDecimals)
	remainder := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.06", tokenDecimals)
	coreumRecipient := chains.Coreum.GenAccount()
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    issuerAcc.String(),
		Currency:  xrplCurrency,
		Amount:    sendingAmount,
		Recipient: coreumRecipient,
	}

	// call from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence)
		require.NoError(t, err)
	}

	balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredXRPLToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(t, sendingAmount.Sub(bridgingFee).Sub(remainder).String(), balanceRes.Balance.Amount.String())

	// assert fees are calculated correctly
	claimFeesAndMakeAssertions(
		ctx,
		t,
		contractClient,
		bankClient,
		relayers,
		bridgingFee,
		remainder,
		registeredXRPLToken.CoreumDenom,
	)

	// send the amount again
	xrplToCoreumTransferEvidence.TxHash = genXRPLTxHash(t)
	xrplToCoreumTransferEvidence.Recipient = coreum.GenAccount()

	// call from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence)
		require.NoError(t, err)
	}

	balanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredXRPLToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(t, sendingAmount.Sub(bridgingFee).Sub(remainder).String(), balanceRes.Balance.Amount.String())

	// assert fees are calculated correctly
	bridgingFeeWithRemainder := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.4", tokenDecimals)
	newRemainder := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.02", tokenDecimals)
	claimFeesAndMakeAssertions(
		ctx,
		t,
		contractClient,
		bankClient,
		relayers,
		bridgingFeeWithRemainder,
		newRemainder,
		registeredXRPLToken.CoreumDenom,
	)
}

func TestEnableAndDisableXRPLOriginatedToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 2)
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	randomCoreumAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, randomCoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})

	coreumRecipient := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumRecipient, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
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
	currency := "abc"
	sendingPrecision := int32(15)
	maxHoldingAmount := sdkmath.NewInt(10000000)

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
		Amount: issueFee.Amount.MulRaw(2).Add(sdkmath.NewInt(10_000_000)),
	})

	randomCoreumAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, randomCoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
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
	maxHoldingAmount := sdkmath.NewInt(100_000_000_000)
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSenderAddress.String(),
		Symbol:        "smb",
		Subunit:       "denom1",
		Precision:     tokenDecimals, // token decimals in terms of the contract
		InitialAmount: sdkmath.NewInt(100_000_000),
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)
	denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)
	_, err = contractClient.RegisterCoreumToken(
		ctx, owner, denom, tokenDecimals, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
	require.NoError(t, err)

	// try to change states of enabled token to the unchangeable state
	for _, state := range unchangeableTokenStates {
		_, err = contractClient.UpdateCoreumToken(ctx, owner, denom, lo.ToPtr(state), nil, nil, nil)
		require.True(t, coreum.IsInvalidTargetTokenStateError(err), err)
	}

	// change states of enabled token to the changeable state
	for _, state := range changeableTokenStates {
		_, err = contractClient.UpdateCoreumToken(ctx, owner, denom, lo.ToPtr(state), nil, nil, nil)
		require.NoError(t, err)
		registeredToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
		require.NoError(t, err)
		require.Equal(t, state, registeredToken.State)
	}

	// try to call from random address
	_, err = contractClient.UpdateCoreumToken(
		ctx, randomCoreumAddress, denom, lo.ToPtr(coreum.TokenStateDisabled), nil, nil, nil,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try to call from relayer address
	_, err = contractClient.UpdateCoreumToken(
		ctx, relayers[0].CoreumAddress, denom, lo.ToPtr(coreum.TokenStateDisabled), nil, nil, nil,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	_, err = contractClient.UpdateCoreumToken(ctx, owner, denom, lo.ToPtr(coreum.TokenStateDisabled), nil, nil, nil)
	require.NoError(t, err)

	// try to send the disabled token
	coinToSendFromCoreumToXRPL := sdk.NewCoin(registeredCoreumOriginatedToken.Denom, issueMsg.InitialAmount)
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		coinToSendFromCoreumToXRPL,
		nil,
	)
	require.True(t, coreum.IsTokenNotEnabledError(err), err)

	// enable token
	_, err = contractClient.UpdateCoreumToken(ctx, owner, denom, lo.ToPtr(coreum.TokenStateEnabled), nil, nil, nil)
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

	registeredToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
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
		Amount: sdkmath.NewInt(1_000_000),
	})

	coreumRecipient := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumRecipient, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
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
	currency := "abc"
	sendingPrecision := int32(15)
	maxHoldingAmount := sdkmath.NewInt(10000000)

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
		Amount: issueFee.Amount.Add(sdkmath.NewInt(10_000_000)),
	})

	randomCoreumAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, randomCoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
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
	maxHoldingAmount := sdkmath.NewInt(100_000_000_000)
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSenderAddress.String(),
		Symbol:        "symbol",
		Subunit:       "subunit",
		Precision:     tokenDecimals, // token decimals in terms of the contract
		InitialAmount: sdkmath.NewInt(100_000_000),
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)
	denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)
	_, err = contractClient.RegisterCoreumToken(
		ctx, owner, denom, tokenDecimals, sendingPrecision, maxHoldingAmount, sdk.ZeroInt(),
	)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
	require.NoError(t, err)
	require.Equal(t, sendingPrecision, registeredCoreumOriginatedToken.SendingPrecision)

	newSendingPrecision := lo.ToPtr(int32(14))
	// try to call from random address
	_, err = contractClient.UpdateCoreumToken(ctx, randomCoreumAddress, denom, nil, newSendingPrecision, nil, nil)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try to call from relayer address
	_, err = contractClient.UpdateCoreumToken(ctx, relayers[0].CoreumAddress, denom, nil, newSendingPrecision, nil, nil)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	_, err = contractClient.UpdateCoreumToken(ctx, owner, denom, nil, newSendingPrecision, nil, nil)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err = contractClient.GetCoreumTokenByDenom(ctx, denom)
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
	_, err = contractClient.UpdateCoreumToken(ctx, owner, denom, nil, newSendingPrecision, nil, nil)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err = contractClient.GetCoreumTokenByDenom(ctx, denom)
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
		Amount: sdkmath.NewInt(1_000_000),
	})

	coreumRecipient := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumRecipient, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
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
	currency := "crn"
	sendingPrecision := int32(15)
	maxHoldingAmount := sdkmath.NewInt(10000000)
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
		Amount: issueFee.Amount.Add(sdkmath.NewInt(10_000_000)),
	})

	randomCoreumAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, randomCoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
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
	maxHoldingAmount := sdkmath.NewInt(100_000_000_000)
	bridgingFee := sdkmath.NewInt(100)
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSenderAddress.String(),
		Symbol:        "symbol",
		Subunit:       "subunit",
		Precision:     tokenDecimals, // token decimals in terms of the contract
		InitialAmount: sdkmath.NewInt(100_000_000),
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)
	denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)
	_, err = contractClient.RegisterCoreumToken(
		ctx, owner, denom, tokenDecimals, sendingPrecision, maxHoldingAmount, bridgingFee,
	)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
	require.NoError(t, err)
	require.Equal(t, bridgingFee.String(), registeredCoreumOriginatedToken.BridgingFee.String())

	newBridgingFee := sdkmath.NewInt(200)
	// try to call from random address
	_, err = contractClient.UpdateCoreumToken(ctx, randomCoreumAddress, denom, nil, nil, nil, &newBridgingFee)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try to call from relayer address
	_, err = contractClient.UpdateCoreumToken(ctx, relayers[0].CoreumAddress, denom, nil, nil, nil, &newBridgingFee)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	_, err = contractClient.UpdateCoreumToken(ctx, owner, denom, nil, nil, nil, &newBridgingFee)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err = contractClient.GetCoreumTokenByDenom(ctx, denom)
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
	_, err = contractClient.UpdateCoreumToken(ctx, owner, denom, nil, nil, nil, &newBridgingFee)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err = contractClient.GetCoreumTokenByDenom(ctx, denom)
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
		Amount: sdkmath.NewInt(1_000_000),
	})

	coreumRecipient := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumRecipient, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
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
	currency := "crn"
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
		Amount: issueFee.Amount.Add(sdkmath.NewInt(10_000_000)),
	})

	randomCoreumAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, randomCoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
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
	bridgingFee := sdkmath.ZeroInt()
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSenderAddress.String(),
		Symbol:        "symbol",
		Subunit:       "subunit",
		Precision:     tokenDecimals, // token decimals in terms of the contract
		InitialAmount: sdkmath.NewInt(100_000_000),
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)
	denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)
	_, err = contractClient.RegisterCoreumToken(
		ctx, owner, denom, tokenDecimals, sendingPrecision, maxHoldingAmount, bridgingFee,
	)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
	require.NoError(t, err)
	require.Equal(t, maxHoldingAmount.String(), registeredCoreumOriginatedToken.MaxHoldingAmount.String())

	newMaxHoldingAmount := sdkmath.NewInt(900)
	_, err = contractClient.UpdateCoreumToken(ctx, owner, denom, nil, nil, &newMaxHoldingAmount, nil)
	require.NoError(t, err)
	registeredCoreumOriginatedToken, err = contractClient.GetCoreumTokenByDenom(ctx, denom)
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
	_, err = contractClient.UpdateCoreumToken(ctx, owner, denom, nil, nil, &newMaxHoldingAmount, nil)
	require.True(t, coreum.IsInvalidTargetMaxHoldingAmountError(err), err)
}

func TestBridgeHalting(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	randomAddress := chains.Coreum.GenAccount()
	relayers := genRelayers(ctx, t, chains, 2)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewInt(10_000_000)),
	})

	chains.Coreum.FundAccountWithOptions(ctx, t, randomAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})

	xrplBridgeAddress := xrpl.GenPrivKeyTxSigner().Account()
	xrplBaseFee := uint32(10)
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		5,
		defaultTrustSetLimitAmount,
		xrplBridgeAddress.String(),
		xrplBaseFee,
	)
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.MulRaw(2),
	})

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 10)

	maxHoldingAmount := sdk.NewIntFromUint64(1_000_000_000)
	sendingPrecision := int32(15)

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	coreumTokenDecimals := uint32(15)
	// register coreum token
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSenderAddress.String(),
		Symbol:        "symbol",
		Subunit:       "subunit",
		Precision:     coreumTokenDecimals, // token decimals in terms of the contract
		InitialAmount: sdkmath.NewInt(100_000_000),
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)
	coreumDenom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)
	_, err = contractClient.RegisterCoreumToken(
		ctx, owner, coreumDenom, coreumTokenDecimals, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	registeredCoreumToken, err := contractClient.GetCoreumTokenByDenom(ctx, coreumDenom)
	require.NoError(t, err)

	// try to halt from not owner and not relayer
	_, err = contractClient.HaltBridge(ctx, randomAddress)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// halt from owner
	_, err = contractClient.HaltBridge(ctx, owner)
	require.NoError(t, err)
	_, err = contractClient.ResumeBridge(ctx, owner)
	require.NoError(t, err)

	// halt from relayer
	_, err = contractClient.HaltBridge(ctx, relayers[0].CoreumAddress)
	require.NoError(t, err)

	// check prohibited operations with the halted bridge
	_, err = contractClient.RegisterXRPLToken(
		ctx,
		owner,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		"TKN",
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)
	require.True(t, coreum.IsBridgeHaltedError(err), err)

	_, err = contractClient.RegisterCoreumToken(
		ctx, owner, coreumDenom, coreumTokenDecimals, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
	)
	require.True(t, coreum.IsBridgeHaltedError(err), err)

	_, err = contractClient.HaltBridge(ctx, owner)
	require.True(t, coreum.IsBridgeHaltedError(err), err)

	_, err = contractClient.ClaimRelayerFees(ctx, relayers[0].CoreumAddress, sdk.NewCoins())
	require.True(t, coreum.IsBridgeHaltedError(err), err)

	// try to provide transfer evidence with the halted bridge
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    xrpl.GenPrivKeyTxSigner().Account().String(),
		Currency:  "SMB",
		Amount:    sdkmath.NewInt(1000),
		Recipient: randomAddress,
	}
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.True(t, coreum.IsBridgeHaltedError(err), err)

	// check that tickets reallocation works if the bridge is halted
	_, err = contractClient.ResumeBridge(ctx, owner)
	require.NoError(t, err)

	tickets, err := contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)

	// use all available tickets and fail the tickets reallocation to test the recovery when the bridge is halted
	sendToXRPLRequests := make([]coreum.SendToXRPLRequest, 0)
	for i := 0; i < len(tickets)-1; i++ {
		sendToXRPLRequests = append(sendToXRPLRequests, coreum.SendToXRPLRequest{
			Recipient:     xrplRecipientAddress.String(),
			Amount:        sdk.NewInt64Coin(registeredCoreumToken.Denom, 1),
			DeliverAmount: nil,
		})
	}
	_, err = contractClient.MultiSendToXRPL(ctx, coreumSenderAddress, sendToXRPLRequests...)
	require.NoError(t, err)

	_, err = contractClient.HaltBridge(ctx, owner)
	require.NoError(t, err)

	// confirm operations (we can't provide signatures, but can confirm the operation is it was submitted)
	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)

	for _, operation := range pendingOperations {
		operationType := operation.OperationType.CoreumToXRPLTransfer
		require.NotNil(t, operationType)
		hash := genXRPLTxHash(t)
		for _, relayer := range relayers {
			acceptTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
				XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
					TxHash:            hash,
					TicketSequence:    &operation.TicketSequence,
					TransactionResult: coreum.TransactionResultAccepted,
				},
			}
			_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
				ctx,
				relayer.CoreumAddress,
				acceptTxEvidence,
			)
			require.NoError(t, err)
		}
	}

	// only tickets allocation is left
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	ticketsAllocationOperation := pendingOperations[0]
	require.NotNil(t, ticketsAllocationOperation.OperationType.AllocateTickets)

	availableTickets, err := contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	// reject allocation first to check the recovery with the halted bridge
	xrplTxHash := genXRPLTxHash(t)
	for _, relayer := range relayers {
		rejectTxEvidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
			XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
				TxHash:            xrplTxHash,
				TicketSequence:    &ticketsAllocationOperation.TicketSequence,
				TransactionResult: coreum.TransactionResultRejected,
			},
		}
		_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
			ctx,
			relayer.CoreumAddress,
			rejectTxEvidence,
		)
		require.NoError(t, err)
	}
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// recover ti
	ticketsToRecover := 10
	recoverTickets(ctx, t, contractClient, owner, relayers, uint32(ticketsToRecover))

	// check that the bridge is still halted
	cfg, err := contractClient.GetContractConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, coreum.BridgeStateHalted, cfg.BridgeState)
}

func TestKeysRotationWithRecovery(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumRecipient := chains.Coreum.GenAccount()
	randomAddress := chains.Coreum.GenAccount()
	initialRelayers := genRelayers(ctx, t, chains, 2)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	chains.Coreum.FundAccountWithOptions(ctx, t, randomAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})

	xrplBridgeAddress := xrpl.GenPrivKeyTxSigner().Account()
	xrplBaseFee := uint32(10)
	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		initialRelayers,
		uint32(len(initialRelayers)),
		20,
		defaultTrustSetLimitAmount,
		xrplBridgeAddress.String(),
		xrplBaseFee,
	)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, initialRelayers, 100)

	maxHoldingAmount := sdk.NewIntFromUint64(1_000_000_000)
	sendingPrecision := int32(15)

	xrplIssuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	xrplIssuer := xrplIssuerAcc.String()

	currencyHexSymbol := hex.EncodeToString([]byte(strings.Repeat("X", 20)))
	hexXRPLCurrency, err := rippledata.NewCurrency(currencyHexSymbol)
	require.NoError(t, err)
	xrplCurrency := xrpl.ConvertCurrencyToString(hexXRPLCurrency)

	// register XRPL token
	_, err = contractClient.RegisterXRPLToken(
		ctx,
		owner,
		xrplIssuer,
		xrplCurrency,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	registerXRPLToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, xrplIssuer, xrplCurrency)
	require.NoError(t, err)

	// activate token
	activateXRPLToken(ctx, t, contractClient, initialRelayers, xrplIssuer, xrplCurrency)

	coreumDenom := "denom"
	coreumTokenDecimals := uint32(15)

	// register coreum token
	_, err = contractClient.RegisterCoreumToken(
		ctx,
		owner,
		coreumDenom,
		coreumTokenDecimals,
		sendingPrecision,
		maxHoldingAmount,
		sdk.ZeroInt(),
	)
	require.NoError(t, err)

	// send XRPL token transfer evidences from current relayer
	xrplToCoreumXRPLTokenTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    xrplIssuerAcc.String(),
		Currency:  xrplCurrency,
		Amount:    sdkmath.NewInt(10),
		Recipient: coreumRecipient,
	}
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, initialRelayers[0].CoreumAddress, xrplToCoreumXRPLTokenTransferEvidence,
	)
	require.NoError(t, err)

	// send Coreum token transfer evidences from current relayer
	registeredCoreumToken, err := contractClient.GetCoreumTokenByDenom(ctx, coreumDenom)
	require.NoError(t, err)
	xrplToCoreumCoreumTokenTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    genXRPLTxHash(t),
		Issuer:    xrplBridgeAddress.String(),
		Currency:  registeredCoreumToken.XRPLCurrency,
		Amount:    sdkmath.NewInt(20),
		Recipient: coreumRecipient,
	}
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, initialRelayers[1].CoreumAddress, xrplToCoreumCoreumTokenTransferEvidence,
	)
	require.NoError(t, err)

	contractCfgBeforeRotationStart, err := contractClient.GetContractConfig(ctx)
	require.NoError(t, err)

	require.Equal(t, coreum.BridgeStateActive, contractCfgBeforeRotationStart.BridgeState)
	require.Equal(t, uint32(2), contractCfgBeforeRotationStart.EvidenceThreshold)

	// keys rotation
	newRelayers := genRelayers(ctx, t, chains, 3)
	// we remove one relayers from first set and add 3 more as result we have 4 relayers
	updatedRelayers := []coreum.Relayer{
		initialRelayers[0],
		newRelayers[0],
		newRelayers[1],
		newRelayers[2],
	}

	// create rotate key operation
	_, err = contractClient.RotateKeys(ctx,
		owner,
		updatedRelayers,
		3,
	)
	require.NoError(t, err)

	contractCfgAfterRotationStart, err := contractClient.GetContractConfig(ctx)
	require.NoError(t, err)

	// check that the current config set is same as it was (apart from state)
	expectedBridgeCfg := contractCfgBeforeRotationStart
	expectedBridgeCfg.BridgeState = coreum.BridgeStateHalted

	require.Equal(t, expectedBridgeCfg, contractCfgAfterRotationStart)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	require.Equal(t, coreum.OperationType{
		RotateKeys: &coreum.OperationTypeRotateKeys{
			NewRelayers:          updatedRelayers,
			NewEvidenceThreshold: 3,
		},
	}, pendingOperations[0].OperationType)

	// update the tx hash to pass the evidence deduplication
	xrplToCoreumXRPLTokenTransferEvidence.TxHash = genXRPLTxHash(t)
	xrplToCoreumCoreumTokenTransferEvidence.TxHash = genXRPLTxHash(t)

	// try to provide the send evidence from the current relayers
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, initialRelayers[0].CoreumAddress, xrplToCoreumXRPLTokenTransferEvidence,
	)
	require.True(t, coreum.IsBridgeHaltedError(err), err)
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, initialRelayers[1].CoreumAddress, xrplToCoreumCoreumTokenTransferEvidence,
	)
	require.True(t, coreum.IsBridgeHaltedError(err), err)

	// try to provide the send evidence from new relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, updatedRelayers[3].CoreumAddress, xrplToCoreumCoreumTokenTransferEvidence,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try to un-halt the bridge with not complete rotation
	_, err = contractClient.ResumeBridge(ctx, owner)
	require.True(t, coreum.IsRotateKeysOngoingError(err), err)

	// reject the rotation
	rejectKeysRotationEvidence := coreum.XRPLTransactionResultKeysRotationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &pendingOperations[0].TicketSequence,
			TransactionResult: coreum.TransactionResultRejected,
		},
	}

	// send from first initial relayer
	_, err = contractClient.SendKeysRotationTransactionResultEvidence(
		ctx, initialRelayers[0].CoreumAddress, rejectKeysRotationEvidence,
	)
	require.NoError(t, err)

	// send from second initial relayer
	_, err = contractClient.SendKeysRotationTransactionResultEvidence(
		ctx, initialRelayers[1].CoreumAddress, rejectKeysRotationEvidence,
	)
	require.NoError(t, err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// check that keys remain the same
	contractCfgAfterRotationRejection, err := contractClient.GetContractConfig(ctx)
	require.NoError(t, err)
	// the bridge is still halted and keys are initial
	require.Equal(t, expectedBridgeCfg, contractCfgAfterRotationRejection)

	contractCfgBeforeRotationRejection := contractCfgAfterRotationRejection

	// create rotate key operation
	_, err = contractClient.RotateKeys(ctx,
		owner,
		updatedRelayers,
		3,
	)
	require.NoError(t, err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)

	// reject the rotation
	acceptKeysRotationEvidence := coreum.XRPLTransactionResultKeysRotationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            genXRPLTxHash(t),
			TicketSequence:    &pendingOperations[0].TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}

	// send from first initial relayer
	_, err = contractClient.SendKeysRotationTransactionResultEvidence(
		ctx, initialRelayers[0].CoreumAddress, acceptKeysRotationEvidence,
	)
	require.NoError(t, err)

	// send from second initial relayer
	_, err = contractClient.SendKeysRotationTransactionResultEvidence(
		ctx, initialRelayers[1].CoreumAddress, acceptKeysRotationEvidence,
	)
	require.NoError(t, err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// check that config is updated
	expectedBridgeCfgAfterRotationAcceptance := contractCfgBeforeRotationRejection
	expectedBridgeCfgAfterRotationAcceptance.EvidenceThreshold = 3
	expectedBridgeCfgAfterRotationAcceptance.Relayers = updatedRelayers

	contractCfgAfterRotationAcceptance, err := contractClient.GetContractConfig(ctx)
	require.NoError(t, err)

	require.Equal(t, expectedBridgeCfgAfterRotationAcceptance, contractCfgAfterRotationAcceptance)

	// resume the bridge
	_, err = contractClient.ResumeBridge(ctx, owner)
	require.NoError(t, err)

	// provide the evidence from the relay which was in prev relayer set
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, initialRelayers[0].CoreumAddress, xrplToCoreumXRPLTokenTransferEvidence,
	)
	require.NoError(t, err)

	// try to provide the evidence from the relay which was in prev relayer set and was removed
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, initialRelayers[1].CoreumAddress, xrplToCoreumXRPLTokenTransferEvidence,
	)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// provide the evidence from the new relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, updatedRelayers[1].CoreumAddress, xrplToCoreumXRPLTokenTransferEvidence,
	)
	require.NoError(t, err)
	// one more time to confirm the sending
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, updatedRelayers[2].CoreumAddress, xrplToCoreumXRPLTokenTransferEvidence,
	)
	require.NoError(t, err)

	// check that the coin is received
	coreumRecipientBalance, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: xrplToCoreumXRPLTokenTransferEvidence.Recipient.String(),
		Denom:   registerXRPLToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(t, xrplToCoreumXRPLTokenTransferEvidence.Amount.String(), coreumRecipientBalance.Balance.Amount.String())
}

func TestUpdateXRPLBaseFee(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	relayers := genRelayers(ctx, t, chains, 2)
	evidenceThreshold := uint32(len(relayers))
	usedTicketSequenceThreshold := uint32(150)
	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	xrplBaseFee := uint32(10)

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		evidenceThreshold,
		usedTicketSequenceThreshold,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		xrplBaseFee,
	)

	contractCfg, err := contractClient.GetContractConfig(ctx)
	require.NoError(t, err)

	require.Equal(t, coreum.ContractConfig{
		Relayers:                    relayers,
		EvidenceThreshold:           evidenceThreshold,
		UsedTicketSequenceThreshold: usedTicketSequenceThreshold,
		TrustSetLimitAmount:         defaultTrustSetLimitAmount,
		BridgeXRPLAddress:           bridgeXRPLAddress,
		BridgeState:                 coreum.BridgeStateActive,
		XRPLBaseFee:                 xrplBaseFee,
	}, contractCfg)

	// update the XRPL base fee when there are no pending operations
	xrplBaseFee = uint32(15)

	// try to update the XRPL base fee from not owner
	_, err = contractClient.UpdateXRPLBaseFee(ctx, relayers[0].CoreumAddress, xrplBaseFee)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// update from owner
	_, err = contractClient.UpdateXRPLBaseFee(ctx, owner, xrplBaseFee)
	require.NoError(t, err)

	contractCfg, err = contractClient.GetContractConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, coreum.ContractConfig{
		Relayers:                    relayers,
		EvidenceThreshold:           evidenceThreshold,
		UsedTicketSequenceThreshold: usedTicketSequenceThreshold,
		TrustSetLimitAmount:         defaultTrustSetLimitAmount,
		BridgeXRPLAddress:           bridgeXRPLAddress,
		BridgeState:                 coreum.BridgeStateActive,
		XRPLBaseFee:                 xrplBaseFee,
	}, contractCfg)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	coreumSender := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewInt(1_000_000)),
	})
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, xrpl.MaxTicketsToAllocate)

	// issue asset ft and register it
	sendingPrecision := int32(6)
	tokenDecimals := uint32(6)
	maxHoldingAmount := sdkmath.NewInt(100_000_000_000)
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSender.String(),
		Symbol:        "symbol",
		Subunit:       "subunit",
		Precision:     tokenDecimals, // token decimals in terms of the contract
		InitialAmount: maxHoldingAmount,
	}
	_, err = client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSender),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)
	denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSender)
	_, err = contractClient.RegisterCoreumToken(
		ctx, owner, denom, tokenDecimals, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)

	operationCountToGenerate := 5
	sendToXRPLRequests := make([]coreum.SendToXRPLRequest, 0, operationCountToGenerate)
	for i := 0; i < operationCountToGenerate; i++ {
		sendToXRPLRequests = append(sendToXRPLRequests, coreum.SendToXRPLRequest{
			Recipient:     xrplRecipientAddress.String(),
			Amount:        sdk.NewCoin(denom, sdkmath.NewInt(10)),
			DeliverAmount: nil,
		})
	}
	_, err = contractClient.MultiSendToXRPL(
		ctx,
		coreumSender,
		sendToXRPLRequests...,
	)
	require.NoError(t, err)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, operationCountToGenerate)

	// try to provide signature for invalid version
	operation := pendingOperations[0]
	_, err = contractClient.SaveSignature(
		ctx, relayers[0].CoreumAddress, operation.TicketSequence, operation.Version+1, xrplTxSignature,
	)
	require.True(t, coreum.IsOperationVersionMismatchError(err), err)

	assertOperationsUpdateAfterXRPLBaseFeeUpdate(ctx, t, contractClient, owner, xrplBaseFee, 20, relayers)
}

func TestUpdateXRPLBaseFeeForMaxOperationCount(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	relayers := genRelayers(ctx, t, chains, int(xrpl.MaxAllowedXRPLSigners))
	evidenceThreshold := uint32(len(relayers))
	usedTicketSequenceThreshold := uint32(150)
	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	xrplBaseFee := uint32(10)

	owner, contractClient := integrationtests.DeployAndInstantiateContract(
		ctx,
		t,
		chains,
		relayers,
		evidenceThreshold,
		usedTicketSequenceThreshold,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		xrplBaseFee,
	)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	coreumSender := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewInt(10_000_000)),
	})
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, xrpl.MaxTicketsToAllocate)

	// issue asset ft and register it
	sendingPrecision := int32(6)
	tokenDecimals := uint32(6)
	maxHoldingAmount := sdkmath.NewInt(100_000_000_000)
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSender.String(),
		Symbol:        "symbol",
		Subunit:       "subunit",
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
	_, err = contractClient.RegisterCoreumToken(
		ctx, owner, denom, tokenDecimals, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)

	// one ticket will be used for the tickets re-allocation
	operationCountToGenerate := int(xrpl.MaxTicketsToAllocate - 1)
	t.Logf("Sending %d SendToXRPL transactions", operationCountToGenerate)
	sendToXRPLRequests := make([]coreum.SendToXRPLRequest, 0, operationCountToGenerate)
	for i := 0; i < operationCountToGenerate; i++ {
		sendToXRPLRequests = append(sendToXRPLRequests, coreum.SendToXRPLRequest{
			Recipient:     xrplRecipientAddress.String(),
			Amount:        sdk.NewCoin(denom, sdkmath.NewInt(10)),
			DeliverAmount: nil,
		})
	}
	chunkSize := 50
	for _, sendToXRPLChunk := range lo.Chunk(sendToXRPLRequests, chunkSize) {
		_, err = contractClient.MultiSendToXRPL(
			ctx,
			coreumSender,
			sendToXRPLChunk...,
		)
		require.NoError(t, err)
	}

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, operationCountToGenerate)

	assertOperationsUpdateAfterXRPLBaseFeeUpdate(ctx, t, contractClient, owner, xrplBaseFee, 35, relayers)
}

func assertOperationsUpdateAfterXRPLBaseFeeUpdate(
	ctx context.Context,
	t *testing.T,
	contractClient *coreum.ContractClient,
	owner sdk.AccAddress,
	oldXRPLBaseFee, newXRPLBase uint32,
	relayers []coreum.Relayer,
) {
	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)

	// provide signatures form all relayers
	initialOperationVersion := uint32(1)

	chunkSize := 50
	require.NoError(t, parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for i, relayer := range relayers {
			t.Logf("Saving signatures for all operations for relayer %d out of %d", i, len(relayers))
			relayerCopy := relayer
			spawn(fmt.Sprintf("relayer-%d", i), parallel.Continue, func(ctx context.Context) error {
				signatures := make([]coreum.SaveSignatureRequest, 0)
				for j := 0; j < len(pendingOperations); j++ {
					operation := pendingOperations[j]
					if initialOperationVersion != operation.Version {
						return errors.Errorf(
							"versions mismatch, expected: %d, got: %d", initialOperationVersion, operation.Version)
					}
					if oldXRPLBaseFee != operation.XRPLBaseFee {
						return errors.Errorf(
							"base fee mismatch, expected: %d, got: %d", oldXRPLBaseFee, operation.XRPLBaseFee)
					}
					signatures = append(signatures, coreum.SaveSignatureRequest{
						OperationID:      operation.TicketSequence,
						OperationVersion: operation.Version,
						Signature:        xrplTxSignature,
					})
				}
				for _, saveSignatureRequestsChunk := range lo.Chunk(signatures, chunkSize) {
					if _, err := contractClient.SaveMultipleSignatures(
						ctx, relayerCopy.CoreumAddress, saveSignatureRequestsChunk...,
					); err != nil {
						return err
					}
				}

				return nil
			})
		}
		return nil
	}))

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	for _, pendingOperation := range pendingOperations {
		require.Len(t, pendingOperation.Signatures, len(relayers))
	}

	txRes, err := contractClient.UpdateXRPLBaseFee(ctx, owner, newXRPLBase)
	require.NoError(t, err)
	t.Logf("Spent gas on UpdateXRPLBaseFee with %d relayers: %d", len(relayers), txRes.GasUsed)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)

	nextOperationVersion := uint32(2)
	t.Logf("Saving signatures for first relayer with different operation version")
	relayer := relayers[0]
	signatures := make([]coreum.SaveSignatureRequest, 0)
	for i := 0; i < len(pendingOperations); i++ {
		operation := pendingOperations[i]
		require.Equal(t, nextOperationVersion, operation.Version)
		require.Equal(t, newXRPLBase, operation.XRPLBaseFee)
		require.Empty(t, operation.Signatures)
		signatures = append(signatures, coreum.SaveSignatureRequest{
			OperationID:      operation.TicketSequence,
			OperationVersion: operation.Version,
			Signature:        xrplTxSignature,
		})
	}
	for _, saveSignatureRequestsChunk := range lo.Chunk(signatures, chunkSize) {
		_, err := contractClient.SaveMultipleSignatures(
			ctx, relayer.CoreumAddress, saveSignatureRequestsChunk...,
		)
		require.NoError(t, err)
	}
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	for _, pendingOperation := range pendingOperations {
		require.Len(t, pendingOperation.Signatures, 1)
	}
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
		txRes, err := contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
			ctx, relayer.CoreumAddress, acceptedTxEvidence,
		)
		require.NoError(t, err)
		thresholdReached, err := event.FindStringEventAttribute(
			txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
		)
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
	}

	// send evidences from relayers
	for _, relayer := range relayers {
		txRes, err := contractClient.SendXRPLTrustSetTransactionResultEvidence(
			ctx, relayer.CoreumAddress, acceptedTxEvidenceTrustSet,
		)
		require.NoError(t, err)
		thresholdReached, err := event.FindStringEventAttribute(
			txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
		)
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
		txRes, err := contractClient.SendXRPLToCoreumTransferEvidence(
			ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence,
		)
		require.NoError(t, err)
		thresholdReached, err := event.FindStringEventAttribute(
			txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
		)
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
	_, err := contractClient.SendToXRPL(ctx, senderCoreumAddress, xrplRecipientAddress.String(), coin, nil)
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
		txRes, err := contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
			ctx, relayer.CoreumAddress, acceptedTxEvidence,
		)
		require.NoError(t, err)
		thresholdReached, err := event.FindStringEventAttribute(
			txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
		)
		require.NoError(t, err)
		if thresholdReached == strconv.FormatBool(true) {
			break
		}
	}
}

func genRelayers(
	ctx context.Context, t *testing.T, chains integrationtests.Chains, relayersCount int,
) []coreum.Relayer {
	relayers := make([]coreum.Relayer, 0)

	for i := 0; i < relayersCount; i++ {
		relayerXRPLSigner := chains.XRPL.GenAccount(ctx, t, 0)
		relayerCoreumAddress := chains.Coreum.GenAccount()
		chains.Coreum.FundAccountWithOptions(ctx, t, relayerCoreumAddress, coreumintegration.BalancesOptions{
			Amount: sdkmath.NewInt(10_000_000),
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

	return strings.ToUpper(hash.String())
}

func claimFeesAndMakeAssertions(
	ctx context.Context,
	t *testing.T,
	contractClient *coreum.ContractClient,
	bankClient banktypes.QueryClient,
	relayers []coreum.Relayer,
	bridgingFeeAmount sdkmath.Int,
	remainderAmount sdkmath.Int,
	denom string,
) {
	expectedFeeAmount := bridgingFeeAmount.Quo(sdk.NewInt(int64(len(relayers))))
	expectedFee := sdk.NewCoins(sdk.NewCoin(denom, expectedFeeAmount))
	for _, relayer := range relayers {
		// assert fees are calculated correctly
		fees, err := contractClient.GetFeesCollected(ctx, relayer.CoreumAddress)
		require.NoError(t, err)
		if bridgingFeeAmount.IsZero() {
			require.Empty(t, fees)
			continue
		}
		require.Len(t, fees, 1)

		// collect fees
		relayerBalanceBeforeClaim, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
			Address: relayer.CoreumAddress.String(),
			Denom:   denom,
		})
		require.NoError(t, err)
		_, err = contractClient.ClaimRelayerFees(ctx, relayer.CoreumAddress, expectedFee)
		require.NoError(t, err)
		relayerBalanceAfterClaim, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
			Address: relayer.CoreumAddress.String(),
			Denom:   denom,
		})
		require.NoError(t, err)
		balanceChange := relayerBalanceAfterClaim.Balance.
			Sub(*relayerBalanceBeforeClaim.Balance)
		require.EqualValues(t, expectedFeeAmount.String(), balanceChange.Amount.String())

		// assert fees are now collected
		fees, err = contractClient.GetFeesCollected(ctx, relayer.CoreumAddress)
		require.NoError(t, err)
		if remainderAmount.IsZero() {
			require.Empty(t, fees, 0)
		} else {
			expectedRemainder := remainderAmount.QuoRaw(int64(len(relayers)))
			require.EqualValues(t, fees[0].Amount.String(), expectedRemainder.String())
		}
	}
}
