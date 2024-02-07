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

	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
)

func TestBridgeHalting(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient address: %s", xrplRecipientAddress)

	coreumSender := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	coreumRecipient := chains.Coreum.GenAccount()
	t.Logf("Coreum recipient: %s", coreumRecipient.String())

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	// start relayers
	runnerEnv.StartAllRunnerProcesses()
	// recover tickets so we can register tokens
	runnerEnv.AllocateTickets(ctx, t, 200)

	// issue asset ft and register it
	sendingPrecision := int32(6)
	tokenDecimals := uint32(6)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 16)
	initialAmount := sdkmath.NewIntWithDecimal(1, 16)
	registeredCoreumOriginatedToken := runnerEnv.IssueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		coreumSender,
		tokenDecimals,
		initialAmount,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)

	// send TrustSet to be able to receive coins from the bridge
	xrplCurrency, err := rippledata.NewCurrency(registeredCoreumOriginatedToken.XRPLCurrency)
	require.NoError(t, err)
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency)

	amountToSendToXRPL := sdkmath.NewIntWithDecimal(1, 6)
	_, err = runnerEnv.ContractClient.SendToXRPL(
		ctx,
		coreumSender,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
		nil,
	)
	require.NoError(t, err)

	// halt the bridge and await for some time to check that the relayers expect halting error
	haltBridgeForTime(ctx, t, runnerEnv, 5*time.Second)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency,
	)
	require.Equal(t, "1", xrplRecipientBalance.Value.String())

	// send back and await for some time to check that the relayers expect halting error
	valueSentToCoreum, err := rippledata.NewValue("1", false)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueSentToCoreum,
		Currency: xrplCurrency,
		Issuer:   runnerEnv.BridgeXRPLAddress,
	}
	runnerEnv.SendFromXRPLToCoreum(
		ctx,
		t,
		xrplRecipientAddress.String(),
		amountToSendFromXRPLtoCoreum,
		coreumRecipient,
	)
	haltBridgeForTime(ctx, t, runnerEnv, 5*time.Second)
	runnerEnv.AwaitCoreumBalance(
		ctx, t, coreumRecipient, sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
	)
}

func haltBridgeForTime(
	ctx context.Context,
	t *testing.T,
	runnerEnv *RunnerEnv,
	timeToHalt time.Duration,
) {
	require.NoError(t, runnerEnv.BridgeClient.HaltBridge(ctx, runnerEnv.ContractOwner))
	select {
	case <-ctx.Done():
		require.NoError(t, ctx.Err())
	case <-time.After(timeToHalt):
	}
	require.NoError(t, runnerEnv.BridgeClient.ResumeBridge(ctx, runnerEnv.ContractOwner))
}
