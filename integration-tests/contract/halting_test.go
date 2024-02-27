//go:build integrationtests
// +build integrationtests

package contract_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestBridgeHalting(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	randomAddress := chains.Coreum.GenAccount()
	relayers := genRelayers(ctx, t, chains, 2)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	chains.Coreum.FundAccountWithOptions(ctx, t, randomAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
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
	initialAmount := sdkmath.NewIntWithDecimal(1, 8)
	registeredCoreumOriginatedToken := issueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		contractClient,
		chains.Coreum,
		coreumSenderAddress,
		owner,
		coreumTokenDecimals,
		initialAmount,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)

	// try to halt from not owner and not relayer
	_, err := contractClient.HaltBridge(ctx, randomAddress)
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
		xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t)),
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)
	require.True(t, coreum.IsBridgeHaltedError(err), err)

	_, err = contractClient.RegisterCoreumToken(
		ctx,
		owner,
		registeredCoreumOriginatedToken.Denom,
		coreumTokenDecimals,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)
	require.True(t, coreum.IsBridgeHaltedError(err), err)

	_, err = contractClient.HaltBridge(ctx, owner)
	require.True(t, coreum.IsBridgeHaltedError(err), err)

	_, err = contractClient.ClaimRelayerFees(ctx, relayers[0].CoreumAddress, sdk.NewCoins())
	require.True(t, coreum.IsBridgeHaltedError(err), err)

	// try to provide transfer evidence with the halted bridge
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    integrationtests.GenXRPLTxHash(t),
		Issuer:    xrpl.GenPrivKeyTxSigner().Account().String(),
		Currency:  xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t)),
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
			Amount:        sdk.NewInt64Coin(registeredCoreumOriginatedToken.Denom, 1),
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
		hash := integrationtests.GenXRPLTxHash(t)
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
	xrplTxHash := integrationtests.GenXRPLTxHash(t)
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
