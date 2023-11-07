//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"fmt"
	"testing"

	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v3/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
)

// TODO(dzmitryhil) add the additional test for each operation which might cause the re-allocation

func TestTicketsAllocationRecoveryWithSequenceNumber(t *testing.T) {
	t.Parallel()

	numberOfTicketsToAllocate := uint32(250)
	ctx, chains := integrationtests.NewTestingContext(t)

	runnerEnv := NewRunnerEnv(ctx, t, DefaultRunnerEnvConfig(), chains)
	availableTickets, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	runnerEnv.StartAllRunnerProcesses(ctx, t)
	chains.XRPL.FundAccountForTicketAllocation(ctx, t, runnerEnv.XRPLBridgeAccount, numberOfTicketsToAllocate)

	xrplBridgeAccountInfo, err := chains.XRPL.RPCClient().AccountInfo(ctx, runnerEnv.XRPLBridgeAccount)
	require.NoError(t, err)

	_, err = runnerEnv.ContractClient.RecoverTickets(
		ctx,
		runnerEnv.ContractOwner,
		*xrplBridgeAccountInfo.AccountData.Sequence,
		&numberOfTicketsToAllocate,
	)
	require.NoError(t, err)

	runnerEnv.AwaitNoPendingOperations(ctx, t)

	availableTickets, err = runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Len(t, availableTickets, int(numberOfTicketsToAllocate))
}

func TestTicketsAllocationRecoveryWithRejection(t *testing.T) {
	t.Parallel()

	numberOfTicketsToAllocate := uint32(250)
	ctx, chains := integrationtests.NewTestingContext(t)

	runnerEnv := NewRunnerEnv(ctx, t, DefaultRunnerEnvConfig(), chains)
	availableTickets, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	runnerEnv.StartAllRunnerProcesses(ctx, t)
	// we don't fund the contract for the tickets allocation to let the chain reject the allocation transaction

	xrplBridgeAccountInfo, err := chains.XRPL.RPCClient().AccountInfo(ctx, runnerEnv.XRPLBridgeAccount)
	require.NoError(t, err)

	// we don't have enough balance on the contract so the recovery will be rejected
	_, err = runnerEnv.ContractClient.RecoverTickets(
		ctx,
		runnerEnv.ContractOwner,
		*xrplBridgeAccountInfo.AccountData.Sequence,
		&numberOfTicketsToAllocate,
	)
	require.NoError(t, err)

	runnerEnv.AwaitNoPendingOperations(ctx, t)

	availableTickets, err = runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)
}

func TestTicketsAllocationRecoveryWithInvalidSequenceNumber(t *testing.T) {
	t.Parallel()

	numberOfTicketsToAllocate := uint32(250)
	ctx, chains := integrationtests.NewTestingContext(t)

	runnerEnv := NewRunnerEnv(ctx, t, DefaultRunnerEnvConfig(), chains)
	availableTickets, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	runnerEnv.StartAllRunnerProcesses(ctx, t)
	chains.XRPL.FundAccountForTicketAllocation(ctx, t, runnerEnv.XRPLBridgeAccount, numberOfTicketsToAllocate)

	xrplBridgeAccountInfo, err := chains.XRPL.RPCClient().AccountInfo(ctx, runnerEnv.XRPLBridgeAccount)
	require.NoError(t, err)

	// make the sequence number lower than current
	_, err = runnerEnv.ContractClient.RecoverTickets(
		ctx,
		runnerEnv.ContractOwner,
		*xrplBridgeAccountInfo.AccountData.Sequence-1,
		&numberOfTicketsToAllocate,
	)
	require.NoError(t, err)
	runnerEnv.AwaitNoPendingOperations(ctx, t)
	availableTickets, err = runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	// make the sequence number greater than current
	_, err = runnerEnv.ContractClient.RecoverTickets(
		ctx,
		runnerEnv.ContractOwner,
		*xrplBridgeAccountInfo.AccountData.Sequence+1,
		&numberOfTicketsToAllocate,
	)
	require.NoError(t, err)
	runnerEnv.AwaitNoPendingOperations(ctx, t)
	availableTickets, err = runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	// use correct input to be sure that we can recover tickets after the failures
	_, err = runnerEnv.ContractClient.RecoverTickets(
		ctx,
		runnerEnv.ContractOwner,
		*xrplBridgeAccountInfo.AccountData.Sequence,
		&numberOfTicketsToAllocate,
	)
	require.NoError(t, err)
	runnerEnv.AwaitNoPendingOperations(ctx, t)
	availableTickets, err = runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Len(t, availableTickets, int(numberOfTicketsToAllocate))
}

func TestTicketsAllocationRecoveryWithMaliciousRelayers(t *testing.T) {
	t.Parallel()

	numberOfTicketsToAllocate := uint32(200)
	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	// add malicious relayers to the config
	envCfg.RelayerNumber = 5
	envCfg.MaliciousRelayerNumber = 2
	envCfg.SigningThreshold = 3

	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	availableTickets, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	runnerEnv.StartAllRunnerProcesses(ctx, t)

	chains.XRPL.FundAccountForTicketAllocation(ctx, t, runnerEnv.XRPLBridgeAccount, numberOfTicketsToAllocate)

	xrplBridgeAccountInfo, err := chains.XRPL.RPCClient().AccountInfo(ctx, runnerEnv.XRPLBridgeAccount)
	require.NoError(t, err)

	_, err = runnerEnv.ContractClient.RecoverTickets(
		ctx,
		runnerEnv.ContractOwner,
		*xrplBridgeAccountInfo.AccountData.Sequence,
		&numberOfTicketsToAllocate,
	)
	require.NoError(t, err)

	runnerEnv.AwaitNoPendingOperations(ctx, t)

	availableTickets, err = runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Len(t, availableTickets, int(numberOfTicketsToAllocate))
}

func TestTicketsReAllocationByTheXRPLTokenRegistration(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.UsedTicketsThreshold = 3
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	runnerEnv.StartAllRunnerProcesses(ctx, t)

	// allocate first five tickets
	numberOfTicketsToAllocate := uint32(5)
	chains.XRPL.FundAccountForTicketAllocation(ctx, t, runnerEnv.XRPLBridgeAccount, numberOfTicketsToAllocate)
	runnerEnv.AllocateTickets(ctx, t, numberOfTicketsToAllocate)
	initialAvailableTickets, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)

	xrplCurrencyIssuerAcc := chains.XRPL.GenAccount(ctx, t, 100)

	// register more than threshold to activate tickets re-allocation
	numberOfXRPLTokensToRegister := int(numberOfTicketsToAllocate) - 1
	// fund owner to cover registration fees
	chains.Coreum.FundAccountWithOptions(ctx, t, runnerEnv.ContractOwner, coreumintegration.BalancesOptions{
		Amount: chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount.MulRaw(int64(numberOfXRPLTokensToRegister)).MulRaw(2),
	})

	for i := 0; i < numberOfXRPLTokensToRegister; i++ {
		xrplRegisteredCurrency, err := rippledata.NewCurrency(fmt.Sprintf("CR%d", i))
		require.NoError(t, err)
		runnerEnv.RegisterXRPLTokenAndAwaitTrustSet(ctx, t, xrplCurrencyIssuerAcc, xrplRegisteredCurrency, int32(6), integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30))
	}
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	availableTicketsAfterReAllocation, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Len(t, availableTicketsAfterReAllocation, envCfg.UsedTicketsThreshold)
	// check that tickets are used
	require.NotEqualValues(t, initialAvailableTickets, availableTicketsAfterReAllocation)

	// use re-allocated tickets
	for i := 0; i < numberOfXRPLTokensToRegister; i++ {
		xrplRegisteredCurrency, err := rippledata.NewCurrency(fmt.Sprintf("DR%d", i))
		require.NoError(t, err)
		runnerEnv.RegisterXRPLTokenAndAwaitTrustSet(ctx, t, xrplCurrencyIssuerAcc, xrplRegisteredCurrency, int32(6), integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30))
	}
	runnerEnv.AwaitNoPendingOperations(ctx, t)
	availableTicketsAfterSecondReallocation, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.NotEqualValues(t, initialAvailableTickets, availableTicketsAfterSecondReallocation)
	require.NotEqualValues(t, availableTicketsAfterReAllocation, availableTicketsAfterSecondReallocation)
}
