//go:build integrationtests
// +build integrationtests

package contract_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v5/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestChangeContractOwnership(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 1)

	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
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
