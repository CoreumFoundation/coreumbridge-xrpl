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

func TestCancelPendingOperation(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 2)

	randomCoreumAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, randomCoreumAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 7),
	})

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		2,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)

	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.MulRaw(2).Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 4)

	initialAvailableTickets, err := contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)

	// register XRPL originated token to create trust set operation
	xrplTokenIssuer := chains.XRPL.GenAccount(ctx, t, 0)
	xrplTokenCurrency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))
	xrplTokenSendingPrecision := int32(15)
	xrplTokenMaxHoldingAmount := sdkmath.NewIntWithDecimal(1, 20)

	_, err = contractClient.RegisterXRPLToken(
		ctx,
		owner,
		xrplTokenIssuer.String(),
		xrplTokenCurrency,
		xrplTokenSendingPrecision,
		xrplTokenMaxHoldingAmount,
		sdkmath.ZeroInt(),
	)
	require.NoError(t, err)

	coreumTokenDecimals := uint32(15)
	coreumTokenSendingPrecision := int32(15)
	coreumTokenMaxHoldingAmount := sdkmath.NewIntWithDecimal(1, 10)
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
		coreumTokenSendingPrecision,
		coreumTokenMaxHoldingAmount,
		sdkmath.ZeroInt(),
	)

	// create send operation to cancel
	amountToSendToXRPL := sdk.NewCoin(registeredCoreumOriginatedToken.Denom, sdkmath.NewInt(1000))
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplTokenIssuer.String(),
		amountToSendToXRPL,
		nil)
	require.NoError(t, err)

	// create rotate key operation
	_, err = contractClient.RotateKeys(ctx,
		owner,
		relayers,
		2,
	)
	require.NoError(t, err)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)

	// trust set + send to XRPL + keys rotation
	require.Len(t, pendingOperations, 3)

	for _, operation := range pendingOperations {
		_, err := contractClient.CancelPendingOperation(ctx, randomCoreumAddress, operation.GetOperationSequence())
		require.True(t, coreum.IsUnauthorizedSenderError(err), err)

		_, err = contractClient.CancelPendingOperation(ctx, relayers[0].CoreumAddress, operation.GetOperationSequence())
		require.True(t, coreum.IsUnauthorizedSenderError(err), err)

		_, err = contractClient.CancelPendingOperation(ctx, owner, operation.GetOperationSequence())
		require.NoError(t, err)
	}

	// check that all tickets are released now
	availableTickets, err := contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, initialAvailableTickets, availableTickets)

	// check that token registration is cancelled correctly
	registeredXRPLToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrplTokenIssuer.String(), xrplTokenCurrency,
	)
	require.NoError(t, err)
	require.Equal(t, coreum.TokenStateInactive, registeredXRPLToken.State)

	// check that send to XRPL is cancelled correctly
	pendingRefunds, err := contractClient.GetPendingRefunds(ctx, coreumSenderAddress)
	require.NoError(t, err)
	require.Len(t, pendingRefunds, 1)
	require.Equal(t, amountToSendToXRPL.String(), pendingRefunds[0].Coin.String())
}
