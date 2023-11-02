//go:build integrationtests
// +build integrationtests

package coreum_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/gogoproto/proto"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

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
	xrpIssuer     = "rrrrrrrrrrrrrrrrrrrrrho"
	xrpCurrency   = "XRP"

	eventAttributeThresholdReached = "threshold_reached"
	trustSetLimitAmount            = "10000000000000000"
)

func TestDeployAndInstantiateContract(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	assetftClient := assetfttypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 1)

	usedTicketsThreshold := 10
	owner, contractClient := integrationtests.DeployAndInstantiateContract(ctx, t, chains, relayers, len(relayers), usedTicketsThreshold, trustSetLimitAmount)

	contractCfg, err := contractClient.GetContractConfig(ctx)
	require.NoError(t, err)

	require.Equal(t, coreum.ContractConfig{
		Relayers:             relayers,
		EvidenceThreshold:    len(relayers),
		UsedTicketsThreshold: usedTicketsThreshold,
		TrustSetLimitAmount:  trustSetLimitAmount,
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
		Issuer:      xrpIssuer,
		Currency:    xrpCurrency,
		CoreumDenom: coreumDenom,
		State:       coreum.TokenStateActive,
	}, xrplTokens[0])
}

func TestChangeContractOwnership(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 1)
	usedTicketsThreshold := 10

	owner, contractClient := integrationtests.DeployAndInstantiateContract(ctx, t, chains, relayers, len(relayers), usedTicketsThreshold, trustSetLimitAmount)

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
	xrplRelayerSigner := xrpl.GenPrivKeyTxSigner()
	relayers := []coreum.Relayer{
		{
			CoreumAddress: chains.Coreum.GenAccount(),
			XRPLAddress:   xrplRelayerSigner.Account().String(),
			XRPLPubKey:    xrplRelayerSigner.PubKey().String(),
		},
	}
	usedTicketsThreshold := 10

	notOwner := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, notOwner, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})

	owner, contractClient := integrationtests.DeployAndInstantiateContract(ctx, t, chains, relayers, len(relayers), usedTicketsThreshold, trustSetLimitAmount)

	denom1 := "denom1"
	denom1Decimals := uint32(17)

	// try to register from not owner
	_, err := contractClient.RegisterCoreumToken(ctx, notOwner, denom1, denom1Decimals)
	require.True(t, coreum.IsNotOwnerError(err), err)

	// register from the owner
	_, err = contractClient.RegisterCoreumToken(ctx, owner, denom1, denom1Decimals)
	require.NoError(t, err)

	// try to register the same denom one more time
	_, err = contractClient.RegisterCoreumToken(ctx, owner, denom1, denom1Decimals)
	require.True(t, coreum.IsCoreumTokenAlreadyRegisteredError(err), err)

	coreumTokens, err := contractClient.GetCoreumTokens(ctx)
	require.NoError(t, err)
	require.Len(t, coreumTokens, 1)

	registeredToken := coreumTokens[0]
	require.Equal(t, denom1, registeredToken.Denom)
	require.Equal(t, denom1Decimals, registeredToken.Decimals)
	require.NotEmpty(t, registeredToken.XRPLCurrency)

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

	relayers := genRelayers(ctx, t, chains, 1)
	usedTicketsThreshold := 3

	notOwner := chains.Coreum.GenAccount()

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	// fund with issuance fee and some coins on to cover fees
	chains.Coreum.FundAccountWithOptions(ctx, t, notOwner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.AddRaw(1_000_000),
	})
	chains.Coreum.FundAccountWithOptions(ctx, t, relayers[0].CoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})

	owner, contractClient := integrationtests.DeployAndInstantiateContract(ctx, t, chains, relayers, len(relayers), usedTicketsThreshold, trustSetLimitAmount)

	// fund owner to cover registration fees twice
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Mul(sdkmath.NewIntFromUint64(2)),
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := "CRA"
	sendingPrecision := int32(15)
	maxHoldingAmount := sdk.NewIntFromUint64(10000)

	// recover tickets so that we can create a pending operation to activate the token
	numberOfTicketsToInit := uint32(6)
	firstBridgeAccountSeqNumber := uint32(1)
	_, err := contractClient.RecoverTickets(ctx, owner, firstBridgeAccountSeqNumber, &numberOfTicketsToInit)
	require.NoError(t, err)

	acceptedTxHash := "D5F78F452DFFBE239EFF668E4B34B1AF66CD2F4D5C5D9E54A5AF34121B5862C5"
	acceptedTxEvidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            acceptedTxHash,
			SequenceNumber:    &firstBridgeAccountSeqNumber,
			TransactionResult: coreum.TransactionResultAccepted,
		},
		Tickets: []uint32{3, 5, 6, 7},
	}

	// send evidence from relayer
	txRes, err := contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[0].CoreumAddress, acceptedTxEvidence)
	require.NoError(t, err)
	thresholdReached, err := event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, "true", thresholdReached)

	// try to register from not owner
	_, err = contractClient.RegisterXRPLToken(ctx, notOwner, issuer, currency, sendingPrecision, maxHoldingAmount)
	require.True(t, coreum.IsNotOwnerError(err), err)

	// register from the owner
	_, err = contractClient.RegisterXRPLToken(ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount)
	require.NoError(t, err)

	// try to register the same denom one more time
	_, err = contractClient.RegisterXRPLToken(ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount)
	require.True(t, coreum.IsXRPLTokenAlreadyRegisteredError(err), err)

	xrplTokens, err := contractClient.GetXRPLTokens(ctx)
	require.NoError(t, err)
	// one XRP token and registered
	require.Len(t, xrplTokens, 2)

	var registeredToken coreum.XRPLToken
	for _, token := range xrplTokens {
		if token.Issuer == issuer && token.Currency == currency {
			registeredToken = token
			break
		}
	}
	require.Equal(t, issuer, registeredToken.Issuer)
	require.Equal(t, currency, registeredToken.Currency)
	require.Equal(t, registeredToken.State, coreum.TokenStateProcessing)
	require.NotEmpty(t, registeredToken.CoreumDenom)

	// check that corresponding token is issued
	contractAddress := contractClient.GetContractAddress()

	tokenRes, err := assetftClient.Token(ctx, &assetfttypes.QueryTokenRequest{
		Denom: registeredToken.CoreumDenom,
	})
	require.NoError(t, err)

	// deconstruct the denom to get prefix used for the symbol and subunit
	prefix, _, err := assetfttypes.DeconstructDenom(registeredToken.CoreumDenom)
	require.NoError(t, err)

	require.Equal(t, assetfttypes.Token{
		Denom:              registeredToken.CoreumDenom,
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
}

func TestSendFromXRPLToCoreumXRPLNativeToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumRecipient := chains.Coreum.GenAccount()
	randomAddress := chains.Coreum.GenAccount()
	relayers := genRelayers(ctx, t, chains, 2)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	chains.Coreum.FundAccountWithOptions(ctx, t, randomAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})

	usedTicketsThreshold := 3

	owner, contractClient := integrationtests.DeployAndInstantiateContract(ctx, t, chains, relayers, len(relayers), usedTicketsThreshold, trustSetLimitAmount)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	// fund owner to cover registration fees twice
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Mul(sdkmath.NewIntFromUint64(2)),
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := "RCR"
	sendingPrecision := int32(15)
	maxHoldingAmount := sdk.NewIntFromUint64(10000)

	// recover tickets so that we can create a pending operation to activate the token
	numberOfTicketsToInit := uint32(4)
	firstBridgeAccountSeqNumber := uint32(1)
	_, err := contractClient.RecoverTickets(ctx, owner, firstBridgeAccountSeqNumber, &numberOfTicketsToInit)
	require.NoError(t, err)

	acceptedTxHash := "D5F78F452DFFBE239EFF668E4B34B1AF66CD2F4D5C5D9E54A5AF34121B5862C8"
	acceptedTxEvidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            acceptedTxHash,
			SequenceNumber:    &firstBridgeAccountSeqNumber,
			TransactionResult: coreum.TransactionResultAccepted,
		},
		Tickets: []uint32{3, 5, 6, 7},
	}

	// send evidence from first relayer
	txRes, err := contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[0].CoreumAddress, acceptedTxEvidence)
	require.NoError(t, err)
	thresholdReached, err := event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, "false", thresholdReached)

	// esnd evidence from second relayer
	txRes, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[1].CoreumAddress, acceptedTxEvidence)
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, "true", thresholdReached)

	// register from the owner
	_, err = contractClient.RegisterXRPLToken(ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount)
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

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	ticketAllocated := pendingOperations[0].TicketNumber

	// activate token

	acceptedTxHashTrustSet := "D5F78F452DFFBE239EFF668E4B34B1AF66CD2F4D5C5D9E54A5AF34121B5862C5"
	acceptedTxEvidenceTrustSet := coreum.XRPLTransactionResultTrustSetEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            acceptedTxHashTrustSet,
			TicketNumber:      &ticketAllocated,
			TransactionResult: coreum.TransactionResultAccepted,
		},
		Issuer:   issuer,
		Currency: currency,
	}

	// send evidence from first relayer
	txResTrustSet, err := contractClient.SendXRPLTrustSetTransactionResultEvidence(ctx, relayers[0].CoreumAddress, acceptedTxEvidenceTrustSet)
	require.NoError(t, err)
	thresholdReachedTrustSet, err := event.FindStringEventAttribute(txResTrustSet.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, "false", thresholdReachedTrustSet)

	// send evidence from second relayer
	txResTrustSet, err = contractClient.SendXRPLTrustSetTransactionResultEvidence(ctx, relayers[1].CoreumAddress, acceptedTxEvidenceTrustSet)
	require.NoError(t, err)
	thresholdReachedTrustSet, err = event.FindStringEventAttribute(txResTrustSet.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, "true", thresholdReachedTrustSet)

	// create an evidence to transfer tokens from XRPL to Coreum
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
	txRes, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence)
	require.NoError(t, err)
	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.True(t, recipientBalanceRes.Balance.IsZero())
	thresholdReached, err = event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, "false", thresholdReached)

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
	require.Equal(t, "true", thresholdReached)

	require.NoError(t, err)
	// expect new token on the recipient balance
	require.Equal(t, xrplToCoreumTransferEvidence.Amount.String(), recipientBalanceRes.Balance.Amount.String())

	// try to push the same evidence
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence)
	require.True(t, coreum.IsOperationAlreadyExecutedError(err), err)
}

func TestSendFromXRPLToCoreumXRPLNativeTokenWithDifferentSendingPrecision(t *testing.T) {
	t.Parallel()

	var (
		tokenDecimals        = int64(15)
		highMaxHoldingAmount = integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)
	)

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 2)
	coreumRecipient := chains.Coreum.GenAccount()

	usedTicketsThreshold := 10
	owner, contractClient := integrationtests.DeployAndInstantiateContract(ctx, t, chains, relayers, len(relayers), usedTicketsThreshold)
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
			t.Parallel()

			// fund owner to cover registration fee twice
			chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
				Amount: issueFee.Amount,
			})

			issuerAcc := xrpl.GenPrivKeyTxSigner().Account()
			issuer := issuerAcc.String()
			currency := "CRC"

			// register from the owner
			txRes, err := contractClient.RegisterXRPLToken(ctx, owner, issuer, currency, tt.sendingPrecision, tt.maxHoldingAmount)
			require.NoError(t, err)
			issuedDenom := findOneIssuedDenomInTxResponse(t, txRes)

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
				Denom:   issuedDenom,
			})
			require.NoError(t, err)
			require.Equal(t, tt.wantReceivedAmount.String(), balanceRes.Balance.Amount.String())
		})
	}
}

func TestRecoverTickets(t *testing.T) {
	t.Parallel()

	// TODO(dzmitryhil) extend the test to check multiple operations once we have them allowed to be created
	usedTicketsThreshold := 5
	numberOfTicketsToInit := uint32(6)

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 3)
	owner, contractClient := integrationtests.DeployAndInstantiateContract(ctx, t, chains, relayers, 2, usedTicketsThreshold, trustSetLimitAmount)

	// ********** Ticket allocation / Recovery **********
	firstBridgeAccountSeqNumber := uint32(1)

	// try to call from not owner
	_, err := contractClient.RecoverTickets(ctx, relayers[0].CoreumAddress, firstBridgeAccountSeqNumber, &numberOfTicketsToInit)
	require.True(t, coreum.IsNotOwnerError(err), err)

	// try to use more than max allowed tickets
	_, err = contractClient.RecoverTickets(ctx, owner, firstBridgeAccountSeqNumber, lo.ToPtr(uint32(251)))
	require.True(t, coreum.IsInvalidTicketNumberToAllocateError(err), err)

	// try to use zero tickets
	_, err = contractClient.RecoverTickets(ctx, owner, firstBridgeAccountSeqNumber, lo.ToPtr(uint32(0)))
	require.True(t, coreum.IsInvalidTicketNumberToAllocateError(err), err)

	_, err = contractClient.RecoverTickets(ctx, owner, firstBridgeAccountSeqNumber, &numberOfTicketsToInit)
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
		TicketNumber:   0,
		SequenceNumber: firstBridgeAccountSeqNumber,
		Signatures:     make([]coreum.Signature, 0),
		OperationType: coreum.OperationType{
			AllocateTickets: &coreum.OperationTypeAllocateTickets{
				Number: numberOfTicketsToInit,
			},
		},
	}, ticketsAllocationOperation)

	// try to recover tickets when the tickets allocation is in-process
	_, err = contractClient.RecoverTickets(ctx, owner, firstBridgeAccountSeqNumber, &numberOfTicketsToInit)
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
	_, err = contractClient.RegisterSignature(ctx, owner, firstBridgeAccountSeqNumber, signerItem1.TxnSignature.String())
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try to send with incorrect operation ID
	_, err = contractClient.RegisterSignature(ctx, relayers[0].CoreumAddress, uint32(999), signerItem1.TxnSignature.String())
	require.True(t, coreum.IsPendingOperationNotFoundError(err), err)

	// send from first relayer
	_, err = contractClient.RegisterSignature(ctx, relayers[0].CoreumAddress, firstBridgeAccountSeqNumber, signerItem1.TxnSignature.String())
	require.NoError(t, err)

	// try to send from the same relayer one more time
	_, err = contractClient.RegisterSignature(ctx, relayers[0].CoreumAddress, firstBridgeAccountSeqNumber, signerItem1.TxnSignature.String())
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
	_, err = contractClient.RegisterSignature(ctx, relayers[1].CoreumAddress, firstBridgeAccountSeqNumber, signerItem2.TxnSignature.String())
	require.NoError(t, err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	ticketsAllocationOperation = pendingOperations[0]
	require.Equal(t, coreum.Operation{
		TicketNumber:   0,
		SequenceNumber: firstBridgeAccountSeqNumber,
		Signatures: []coreum.Signature{
			{
				Relayer:   relayers[0].CoreumAddress,
				Signature: signerItem1.TxnSignature.String(),
			},
			{
				Relayer:   relayers[1].CoreumAddress,
				Signature: signerItem2.TxnSignature.String(),
			},
		},
		OperationType: coreum.OperationType{
			AllocateTickets: &coreum.OperationTypeAllocateTickets{
				Number: numberOfTicketsToInit,
			},
		},
	}, ticketsAllocationOperation)

	// ********** TransactionResultEvidence / Transaction rejected **********

	rejectedTxHash := "FC7B3043C73998C6696C788D73621D55D7C05BEBBA0A14C186AF43F6B12AE8E3"
	rejectedTxEvidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            rejectedTxHash,
			SequenceNumber:    &firstBridgeAccountSeqNumber,
			TransactionResult: coreum.TransactionResultRejected,
		},
		Tickets: nil,
	}

	// try to send with not existing sequence
	invalidRejectedTxEvidence := rejectedTxEvidence
	invalidRejectedTxEvidence.SequenceNumber = lo.ToPtr(uint32(999))
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[0].CoreumAddress, invalidRejectedTxEvidence)
	require.True(t, coreum.IsPendingOperationNotFoundError(err), err)

	// try to send with not existing ticket
	invalidRejectedTxEvidence = rejectedTxEvidence
	invalidRejectedTxEvidence.SequenceNumber = nil
	invalidRejectedTxEvidence.TicketNumber = lo.ToPtr(uint32(999))
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
	require.Equal(t, "false", thresholdReached)

	// try to send evidence from second relayer one more time
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[0].CoreumAddress, rejectedTxEvidence)
	require.True(t, coreum.IsEvidenceAlreadyProvidedError(err), err)

	// send evidence from second relayer
	txRes, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[1].CoreumAddress, rejectedTxEvidence)
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, "true", thresholdReached)

	// try to send the evidence one more time
	_, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[0].CoreumAddress, rejectedTxEvidence)
	require.True(t, coreum.IsOperationAlreadyExecutedError(err), err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 0)

	availableTickets, err = contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	// ********** Ticket allocation after previous failure / Recovery **********

	secondBridgeAccountSeqNumber := uint32(2)
	// start the process one more time
	_, err = contractClient.RecoverTickets(ctx, owner, secondBridgeAccountSeqNumber, &numberOfTicketsToInit)
	require.NoError(t, err)

	// ********** TransactionResultEvidence / Transaction accepted **********

	// we can omit the signing here since it is required only for the tx submission.
	acceptedTxHash := "D5F78F452DFFBE239EFF668E4B34B1AF66CD2F4D5C5D9E54A5AF34121B5862C8"
	acceptedTxEvidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            acceptedTxHash,
			SequenceNumber:    &secondBridgeAccountSeqNumber,
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
	require.Equal(t, "false", thresholdReached)

	// send evidence from second relayer
	txRes, err = contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, relayers[1].CoreumAddress, acceptedTxEvidence)
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached)
	require.NoError(t, err)
	require.Equal(t, "true", thresholdReached)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 0)

	availableTickets, err = contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Equal(t, acceptedTxEvidence.Tickets, availableTickets)
}

func findOneIssuedDenomInTxResponse(t *testing.T, txRes *sdk.TxResponse) string {
	t.Helper()

	eventIssuedName := proto.MessageName(&assetfttypes.EventIssued{})
	foundDenom := ""
	for i := range txRes.Events {
		if txRes.Events[i].Type != eventIssuedName {
			continue
		}
		if foundDenom != "" {
			require.Failf(t, "found multiple issued denom is the tx response, but expected one", "events:%+v", txRes.Events)
		}
		eventsTokenIssued, err := event.FindTypedEvents[*assetfttypes.EventIssued](txRes.Events)
		require.NoError(t, err)
		foundDenom = eventsTokenIssued[0].Denom
	}
	if foundDenom == "" {
		require.Failf(t, "not found in the issue response", "event: %s ", eventIssuedName)
	}

	return foundDenom
}

func genRelayers(ctx context.Context, t *testing.T, chains integrationtests.Chains, relayersCount int) []coreum.Relayer {
	relayers := make([]coreum.Relayer, 0)

	for i := 0; i < relayersCount; i++ {
		xrplRelayerSigner := chains.XRPL.GenAccount(ctx, t, 0)
		relayerCoreumAddress := chains.Coreum.GenAccount()
		chains.Coreum.FundAccountWithOptions(ctx, t, relayerCoreumAddress, coreumintegration.BalancesOptions{
			Amount: sdkmath.NewInt(1_000_000),
		})
		relayers = append(relayers, coreum.Relayer{
			CoreumAddress: relayerCoreumAddress,
			XRPLAddress:   xrplRelayerSigner.String(),
			XRPLPubKey:    chains.XRPL.GetSignerPubKey(t, xrplRelayerSigner).String(),
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
