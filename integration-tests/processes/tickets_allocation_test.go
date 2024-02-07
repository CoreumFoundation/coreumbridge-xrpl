//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"context"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum-tools/pkg/retry"
	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
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
	chains.XRPL.FundAccountForTicketAllocation(ctx, t, runnerEnv.BridgeXRPLAddress, numberOfTicketsToAllocate)

	bridgeXRPLAccountInfo, err := chains.XRPL.RPCClient().AccountInfo(ctx, runnerEnv.BridgeXRPLAddress)
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

	bridgeXRPLAccountInfo, err := chains.XRPL.RPCClient().AccountInfo(ctx, runnerEnv.BridgeXRPLAddress)
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
	chains.XRPL.FundAccountForTicketAllocation(ctx, t, runnerEnv.BridgeXRPLAddress, numberOfTicketsToAllocate)

	bridgeXRPLAccountInfo, err := chains.XRPL.RPCClient().AccountInfo(ctx, runnerEnv.BridgeXRPLAddress)
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

	chains.XRPL.FundAccountForTicketAllocation(ctx, t, runnerEnv.BridgeXRPLAddress, numberOfTicketsToAllocate)

	bridgeXRPLAccountInfo, err := chains.XRPL.RPCClient().AccountInfo(ctx, runnerEnv.BridgeXRPLAddress)
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
	chains.XRPL.FundAccountForTicketAllocation(ctx, t, runnerEnv.BridgeXRPLAddress, numberOfTicketsToAllocate)
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

	for i := 0; i < int(numberOfXRPLTokensToRegister); i++ {
		registeredXRPLCurrency := integrationtests.GenerateXRPLCurrency(t)
		runnerEnv.RegisterXRPLOriginatedToken(
			ctx,
			t,
			xrplCurrencyIssuerAcc,
			registeredXRPLCurrency,
			int32(6),
			integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30),
			sdkmath.ZeroInt(),
		)
	}
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	availableTicketsAfterReallocation, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Len(t, availableTicketsAfterReallocation, int(envCfg.UsedTicketSequenceThreshold))
	// check that tickets are used
	require.NotEqualValues(t, initialAvailableTickets, availableTicketsAfterReallocation)

	// use re-allocated tickets
	for i := 0; i < int(numberOfXRPLTokensToRegister); i++ {
		registeredXRPLCurrency := integrationtests.GenerateXRPLCurrency(t)
		runnerEnv.RegisterXRPLOriginatedToken(
			ctx,
			t,
			xrplCurrencyIssuerAcc,
			registeredXRPLCurrency,
			int32(6),
			integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30),
			sdkmath.ZeroInt(),
		)
	}
	runnerEnv.AwaitNoPendingOperations(ctx, t)
	availableTicketsAfterSecondReallocation, err := runnerEnv.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.NotEqualValues(t, initialAvailableTickets, availableTicketsAfterSecondReallocation)
	require.NotEqualValues(t, availableTicketsAfterReallocation, availableTicketsAfterSecondReallocation)
}

func TestSendXRPLOriginatedTokenFromXRPLToCoreumWithTicketsReallocation(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.UsedTicketSequenceThreshold = 3
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(5))
	sendingCount := 10

	coreumSender := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})
	t.Logf("Coreum sender: %s", coreumSender.String())
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient: %s", xrplRecipientAddress.String())

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)
	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)

	registeredXRPLCurrency := integrationtests.GenerateXRPLCurrency(t)
	registeredXRPLToken := runnerEnv.RegisterXRPLOriginatedToken(
		ctx,
		t,
		xrplIssuerAddress,
		registeredXRPLCurrency,
		int32(6),
		integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30),
		sdkmath.ZeroInt(),
	)

	valueToSendFromXRPLtoCoreum, err := rippledata.NewValue("1e10", false)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueToSendFromXRPLtoCoreum,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}

	runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplIssuerAddress.String(), amountToSendFromXRPLtoCoreum, coreumSender)
	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumSender,
		sdk.NewCoin(
			registeredXRPLToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				valueToSendFromXRPLtoCoreum.String(),
				xrpl.XRPLIssuedTokenDecimals,
			),
		),
	)

	// send TrustSet to be able to receive coins
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency)

	totalSent := sdkmath.ZeroInt()
	amountToSend := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "10", xrpl.XRPLIssuedTokenDecimals)
	for i := 0; i < sendingCount; i++ {
		retryCtx, retryCtxCancel := context.WithTimeout(ctx, 15*time.Second)
		require.NoError(t, retry.Do(retryCtx, 500*time.Millisecond, func() error {
			_, err = runnerEnv.ContractClient.SendToXRPL(
				ctx,
				coreumSender,
				xrplRecipientAddress.String(),
				sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSend),
				nil,
			)
			if err == nil {
				return nil
			}
			if coreum.IsLastTicketReservedError(err) || coreum.IsNoAvailableTicketsError(err) {
				t.Logf("No tickets left, waiting for new tickets...")
				return retry.Retryable(err)
			}
			require.NoError(t, err)
			return nil
		}))
		retryCtxCancel()
		totalSent = totalSent.Add(amountToSend)
	}
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx,
		t,
		xrplRecipientAddress,
		xrplIssuerAddress,
		registeredXRPLCurrency,
	)
	require.Equal(
		t,
		totalSent.Quo(sdkmath.NewIntWithDecimal(1, xrpl.XRPLIssuedTokenDecimals)).String(),
		xrplRecipientBalance.Value.String(),
	)
}
