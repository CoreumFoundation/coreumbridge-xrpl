//go:build integrationtests
// +build integrationtests

package contract_test

import (
	"strconv"
	"testing"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum/v4/testutil/event"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestRecoverTickets(t *testing.T) {
	t.Parallel()

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
