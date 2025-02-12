//go:build integrationtests
// +build integrationtests

package contract_test

import (
	"context"
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestUpdateXRPLBaseFee(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	relayers := genRelayers(ctx, t, chains, 2)
	evidenceThreshold := uint32(len(relayers))
	usedTicketSequenceThreshold := uint32(150)
	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	xrplBaseFee := uint32(10)

	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
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
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 6)),
	})
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, xrpl.MaxTicketsToAllocate)

	// issue asset ft and register it
	sendingPrecision := int32(6)
	tokenDecimals := uint32(6)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 11)
	initialAmount := sdkmath.NewIntWithDecimal(1, 11)
	registeredCoreumOriginatedToken := issueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		contractClient,
		chains.Coreum,
		coreumSender,
		owner,
		tokenDecimals,
		initialAmount,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)

	operationCountToGenerate := 5
	sendToXRPLRequests := make([]coreum.SendToXRPLRequest, 0, operationCountToGenerate)
	for range operationCountToGenerate {
		sendToXRPLRequests = append(sendToXRPLRequests, coreum.SendToXRPLRequest{
			Recipient:     xrplRecipientAddress.String(),
			Amount:        sdk.NewCoin(registeredCoreumOriginatedToken.Denom, sdkmath.NewInt(10)),
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

	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
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
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 7)),
	})
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, xrpl.MaxTicketsToAllocate)

	// issue asset ft and register it
	sendingPrecision := int32(6)
	tokenDecimals := uint32(6)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 11)
	initialAmount := sdkmath.NewIntWithDecimal(1, 11)
	registeredCoreumOriginatedToken := issueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		contractClient,
		chains.Coreum,
		coreumSender,
		owner,
		tokenDecimals,
		initialAmount,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)

	// one ticket will be used for the tickets re-allocation
	operationCountToGenerate := int(xrpl.MaxTicketsToAllocate - 1)
	t.Logf("Sending %d SendToXRPL transactions", operationCountToGenerate)
	sendToXRPLRequests := make([]coreum.SendToXRPLRequest, 0, operationCountToGenerate)
	for range operationCountToGenerate {
		sendToXRPLRequests = append(sendToXRPLRequests, coreum.SendToXRPLRequest{
			Recipient:     xrplRecipientAddress.String(),
			Amount:        sdk.NewCoin(registeredCoreumOriginatedToken.Denom, sdkmath.NewInt(10)),
			DeliverAmount: nil,
		})
	}
	chunkSize := 50
	for _, sendToXRPLChunk := range lo.Chunk(sendToXRPLRequests, chunkSize) {
		_, err := contractClient.MultiSendToXRPL(
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
			t.Logf("Saving signatures for all operations for relayer %d out of %d", i+1, len(relayers))
			relayerCopy := relayer
			spawn(fmt.Sprintf("relayer-%d", i), parallel.Continue, func(ctx context.Context) error {
				signatures := make([]coreum.SaveSignatureRequest, 0)
				for j := range len(pendingOperations) {
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
	for i := range len(pendingOperations) {
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
