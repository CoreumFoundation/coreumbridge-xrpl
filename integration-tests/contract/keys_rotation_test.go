//go:build integrationtests
// +build integrationtests

package contract_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestKeysRotationWithRecovery(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumRecipient := chains.Coreum.GenAccount()
	randomAddress := chains.Coreum.GenAccount()
	initialRelayers := genRelayers(ctx, t, chains, 2)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	chains.Coreum.FundAccountWithOptions(ctx, t, randomAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
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

	// register XRPL token
	xrplCurrency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))
	_, err := contractClient.RegisterXRPLToken(
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
		TxHash:    integrationtests.GenXRPLTxHash(t),
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
		TxHash:    integrationtests.GenXRPLTxHash(t),
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
	xrplToCoreumXRPLTokenTransferEvidence.TxHash = integrationtests.GenXRPLTxHash(t)
	xrplToCoreumCoreumTokenTransferEvidence.TxHash = integrationtests.GenXRPLTxHash(t)

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
			TxHash:            integrationtests.GenXRPLTxHash(t),
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
			TxHash:            integrationtests.GenXRPLTxHash(t),
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
