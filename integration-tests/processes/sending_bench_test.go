//go:build benchmarks

package processes_test

import (
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
	sdk "github.com/cosmos/cosmos-sdk/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"
)

func TestBenchmarkSendFromXRPLToCoreumAndBack(t *testing.T) {
	ctx, chains := integrationtests.NewTestingContext(t)
	testCount := 10
	sendAmount := 1

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	// setup amount to send
	registeredXRPToken, err := runnerEnv.ContractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrpl.XRPTokenIssuer.String(), xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
	)
	require.NoError(t, err)

	valueToSendFromXRPLtoCoreum, err := rippledata.NewValue(fmt.Sprint(sendAmount), true)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueToSendFromXRPLtoCoreum,
		Currency: xrpl.XRPTokenCurrency,
		Issuer:   xrpl.XRPTokenIssuer,
	}

	// generate and fund accounts
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	xrplRecipientBalanceBefore := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, xrpl.XRPTokenIssuer, xrpl.XRPTokenCurrency,
	)
	coreumAccounts := []sdk.AccAddress{}
	xrplAccounts := []rippledata.Account{}

	t.Log("Generating and funding accounts")
	for i := 0; i < testCount; i++ {
		newCoreumLAccount := chains.Coreum.GenAccount()
		coreumAccounts = append(coreumAccounts, newCoreumLAccount)
		chains.Coreum.FundAccountWithOptions(ctx, t, newCoreumLAccount, coreumintegration.BalancesOptions{
			Amount: sdkmath.NewIntFromUint64(1_000_000).MulRaw(int64(testCount)),
		})
		newXRPLAccount := chains.XRPL.GenAccount(ctx, t, 1)
		xrplAccounts = append(xrplAccounts, newXRPLAccount)
	}

	t.Log("Accounts generated and funded")

	startTime := time.Now()
	for i := 0; i < testCount; i++ {
		coreumAccount := coreumAccounts[i]
		xrplAccount := xrplAccounts[i]
		go runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplAccount.String(), amountToSendFromXRPLtoCoreum, coreumAccount)
	}

	for i := 0; i < testCount; i++ {
		coreumAccount := coreumAccounts[i]
		runnerEnv.AwaitCoreumBalance(
			ctx,
			t,
			coreumAccount,
			sdk.NewCoin(
				registeredXRPToken.CoreumDenom,
				integrationtests.ConvertStringWithDecimalsToSDKInt(
					t,
					valueToSendFromXRPLtoCoreum.String(),
					xrpl.XRPCurrencyDecimals,
				)),
		)

		// send back to xrpl right after it is received on coreum
		runnerEnv.SendFromCoreumToXRPL(
			ctx,
			t,
			coreumAccount,
			xrplRecipientAddress,
			sdk.NewCoin(
				registeredXRPToken.CoreumDenom,
				integrationtests.ConvertStringWithDecimalsToSDKInt(
					t,
					valueToSendFromXRPLtoCoreum.String(),
					xrpl.XRPCurrencyDecimals,
				)),
			nil,
		)
	}

	runnerEnv.AwaitNoPendingOperations(ctx, t)
	testDuration := time.Since(startTime)

	xrplRecipientBalanceAfter := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, xrpl.XRPTokenIssuer, xrpl.XRPTokenCurrency,
	)
	received, err := xrplRecipientBalanceAfter.Value.Subtract(*xrplRecipientBalanceBefore.Value)
	require.NoError(t, err)
	expectedRecieved, err := rippledata.NewAmount(fmt.Sprint(sendAmount * testCount))
	require.NoError(t, err)
	require.Equal(t, expectedRecieved.Value.String(), received.String())

	t.Logf("Ran %d Operations in %s, %s per operation", testCount, testDuration, testDuration/time.Duration(testCount))
}
