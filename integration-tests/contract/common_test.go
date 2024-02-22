//go:build integrationtests
// +build integrationtests

package contract_test

import (
	"context"
	"strconv"
	"testing"

	sdkmath "cosmossdk.io/math"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/google/uuid"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum/v4/pkg/client"
	"github.com/CoreumFoundation/coreum/v4/testutil/event"
	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	assetfttypes "github.com/CoreumFoundation/coreum/v4/x/asset/ft/types"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
)

const (
	eventAttributeThresholdReached = "threshold_reached"
	//nolint:lll // the signature sample doesn't require to be split
	xrplTxSignature = "304502210097099E9AB2C41DA3F672004924B3557D58D101A5745C57C6336C5CF36B59E8F5022003984E50483C921E3FDF45BC7DE4E6ED9D340F0E0CAA6BB1828C647C6665A1CC"
)

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
			TxHash:            integrationtests.GenXRPLTxHash(t),
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
		trustSetOperation coreum.Operation
		found             bool
	)
	for _, operation := range pendingOperations {
		operationType := operation.OperationType.TrustSet
		if operationType != nil && operationType.Issuer == issuer && operationType.Currency == currency {
			found = true
			trustSetOperation = operation
			break
		}
	}
	require.True(t, found)
	require.NotNil(t, trustSetOperation.OperationType.TrustSet)

	acceptedTxEvidenceTrustSet := coreum.XRPLTransactionResultTrustSetEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            integrationtests.GenXRPLTxHash(t),
			TicketSequence:    &trustSetOperation.TicketSequence,
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
		TxHash:    integrationtests.GenXRPLTxHash(t),
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
			TxHash:            integrationtests.GenXRPLTxHash(t),
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
			Amount: sdkmath.NewIntWithDecimal(1, 7),
		})
		relayers = append(relayers, coreum.Relayer{
			CoreumAddress: relayerCoreumAddress,
			XRPLAddress:   relayerXRPLSigner.String(),
			XRPLPubKey:    chains.XRPL.GetSignerPubKey(t, relayerXRPLSigner).String(),
		})
	}
	return relayers
}

func issueAndRegisterCoreumOriginatedToken(
	ctx context.Context,
	t *testing.T,
	contractClient *coreum.ContractClient,
	coreumChain integrationtests.CoreumChain,
	issuerAddress sdk.AccAddress,
	ownerAddress sdk.AccAddress,
	tokenDecimals uint32,
	initialAmount sdkmath.Int,
	sendingPrecision int32,
	maxHoldingAmount sdkmath.Int,
	bridgingFee sdkmath.Int,
) coreum.CoreumToken {
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        issuerAddress.String(),
		Symbol:        "symbol" + uuid.NewString()[:4],
		Subunit:       "subunit" + uuid.NewString()[:4],
		Precision:     tokenDecimals, // token decimals in terms of the contract
		InitialAmount: initialAmount,
	}
	_, err := client.BroadcastTx(
		ctx,
		coreumChain.ClientContext.WithFromAddress(issuerAddress),
		coreumChain.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)
	require.NoError(t, err)
	coreumDenom := assetfttypes.BuildDenom(issueMsg.Subunit, issuerAddress)
	_, err = contractClient.RegisterCoreumToken(
		ctx, ownerAddress, coreumDenom, tokenDecimals, sendingPrecision, maxHoldingAmount, bridgingFee,
	)
	require.NoError(t, err)
	registeredCoreumToken, err := contractClient.GetCoreumTokenByDenom(ctx, coreumDenom)
	require.NoError(t, err)

	return registeredCoreumToken
}
