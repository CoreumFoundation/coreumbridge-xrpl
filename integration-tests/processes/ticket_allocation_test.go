//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"fmt"
	"testing"

	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
)

func TestTicketsAllocationRecoveryWithAccountSequence(t *testing.T) {
	t.Parallel()

	numberOfTicketsToAllocate := uint32(250)
	ctx, chains := integrationtests.NewTestingContext(t)

	runnerEnv := NewRunnerEnv(ctx, t, DefaultRunnerEnvConfig(), chains)
	availableTickets, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	runnerEnv.StartAllRunnerProcesses()
	chains.XRPL.FundAccountForTicketAllocation(ctx, t, runnerEnv.bridgeXRPLAddress, numberOfTicketsToAllocate)

	bridgeXRPLAccountInfo, err := chains.XRPL.RPCClient().AccountInfo(ctx, runnerEnv.bridgeXRPLAddress)
	require.NoError(t, err)

	_, err = runnerEnv.ContractClient.RecoverTickets(
		ctx,
		runnerEnv.ContractOwner,
		*bridgeXRPLAccountInfo.AccountData.Sequence,
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

	runnerEnv.StartAllRunnerProcesses()
	// we don't fund the contract for the tickets allocation to let the chain reject the allocation transaction

	bridgeXRPLAccountInfo, err := chains.XRPL.RPCClient().AccountInfo(ctx, runnerEnv.bridgeXRPLAddress)
	require.NoError(t, err)

	// we don't have enough balance on the contract so the recovery will be rejected
	_, err = runnerEnv.ContractClient.RecoverTickets(
		ctx,
		runnerEnv.ContractOwner,
		*bridgeXRPLAccountInfo.AccountData.Sequence,
		&numberOfTicketsToAllocate,
	)
	require.NoError(t, err)

	runnerEnv.AwaitNoPendingOperations(ctx, t)

	availableTickets, err = runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)
}

func TestTicketsAllocationRecoveryWithInvalidAccountSequence(t *testing.T) {
	t.Parallel()

	numberOfTicketsToAllocate := uint32(250)
	ctx, chains := integrationtests.NewTestingContext(t)

	runnerEnv := NewRunnerEnv(ctx, t, DefaultRunnerEnvConfig(), chains)
	availableTickets, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	runnerEnv.StartAllRunnerProcesses()
	chains.XRPL.FundAccountForTicketAllocation(ctx, t, runnerEnv.bridgeXRPLAddress, numberOfTicketsToAllocate)

	bridgeXRPLAccountInfo, err := chains.XRPL.RPCClient().AccountInfo(ctx, runnerEnv.bridgeXRPLAddress)
	require.NoError(t, err)

	// make the sequence number lower than current
	_, err = runnerEnv.ContractClient.RecoverTickets(
		ctx,
		runnerEnv.ContractOwner,
		*bridgeXRPLAccountInfo.AccountData.Sequence-1,
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
		*bridgeXRPLAccountInfo.AccountData.Sequence+1,
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
		*bridgeXRPLAccountInfo.AccountData.Sequence,
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
	envCfg.RelayersCount = 5
	envCfg.MaliciousRelayerNumber = 2
	envCfg.SigningThreshold = 3

	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	availableTickets, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Empty(t, availableTickets)

	runnerEnv.StartAllRunnerProcesses()

	chains.XRPL.FundAccountForTicketAllocation(ctx, t, runnerEnv.bridgeXRPLAddress, numberOfTicketsToAllocate)

	bridgeXRPLAccountInfo, err := chains.XRPL.RPCClient().AccountInfo(ctx, runnerEnv.bridgeXRPLAddress)
	require.NoError(t, err)

	_, err = runnerEnv.ContractClient.RecoverTickets(
		ctx,
		runnerEnv.ContractOwner,
		*bridgeXRPLAccountInfo.AccountData.Sequence,
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
	envCfg.UsedTicketSequenceThreshold = 3
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	runnerEnv.StartAllRunnerProcesses()

	// allocate first five tickets
	numberOfTicketsToAllocate := uint32(5)
	chains.XRPL.FundAccountForTicketAllocation(ctx, t, runnerEnv.bridgeXRPLAddress, numberOfTicketsToAllocate)
	runnerEnv.AllocateTickets(ctx, t, numberOfTicketsToAllocate)
	initialAvailableTickets, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)

	xrplCurrencyIssuerAcc := chains.XRPL.GenAccount(ctx, t, 100)

	// register more than threshold to activate tickets re-allocation
	numberOfXRPLTokensToRegister := envCfg.UsedTicketSequenceThreshold + 1
	// fund owner to cover asset FT issuance fees
	chains.Coreum.FundAccountWithOptions(ctx, t, runnerEnv.ContractOwner, coreumintegration.BalancesOptions{
		Amount: chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount.
			MulRaw(int64(numberOfXRPLTokensToRegister)).MulRaw(2),
	})

	for i := 0; i < numberOfXRPLTokensToRegister; i++ {
		registeredXRPLCurrency, err := rippledata.NewCurrency(fmt.Sprintf("CR%d", i))
		require.NoError(t, err)
		runnerEnv.RegisterXRPLOriginatedToken(
			ctx,
			t,
			xrplCurrencyIssuerAcc,
			registeredXRPLCurrency,
			int32(6),
			integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30),
		)
	}
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	availableTicketsAfterReallocation, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Len(t, availableTicketsAfterReallocation, envCfg.UsedTicketSequenceThreshold)
	// check that tickets are used
	require.NotEqualValues(t, initialAvailableTickets, availableTicketsAfterReallocation)

	// use re-allocated tickets
	for i := 0; i < numberOfXRPLTokensToRegister; i++ {
		registeredXRPLCurrency, err := rippledata.NewCurrency(fmt.Sprintf("DR%d", i))
		require.NoError(t, err)
		runnerEnv.RegisterXRPLOriginatedToken(
			ctx,
			t,
			xrplCurrencyIssuerAcc,
			registeredXRPLCurrency,
			int32(6),
			integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30),
		)
	}
	runnerEnv.AwaitNoPendingOperations(ctx, t)
	availableTicketsAfterSecondReallocation, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.NotEqualValues(t, initialAvailableTickets, availableTicketsAfterSecondReallocation)
	require.NotEqualValues(t, availableTicketsAfterReallocation, availableTicketsAfterSecondReallocation)
}
