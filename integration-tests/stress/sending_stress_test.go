//go:build integrationtests
// +build integrationtests

package stress_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	"github.com/CoreumFoundation/coreum-tools/pkg/retry"
	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/pkg/errors"

	// processtest "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests/processes"
	// "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests/processes"

	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
	sdk "github.com/cosmos/cosmos-sdk/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"
)

func TestStressSendFromXRPLToCoreumAndBack(t *testing.T) {
	ctx, chains := integrationtests.NewTestingContext(t)
	testCount := 10
	sendAmount := 1

	contractClient := coreum.NewContractClient(
		coreum.DefaultContractClientConfig(integrationtests.GetContractAddress(t)),
		chains.Log,
		chains.Coreum.ClientContext,
	)

	xrplTxSigner := xrpl.NewKeyringTxSigner(chains.XRPL.GetSignerKeyring())
	bridgeClient := bridgeclient.NewBridgeClient(
		chains.Log,
		chains.Coreum.ClientContext,
		contractClient,
		chains.XRPL.RPCClient(),
		xrplTxSigner,
	)

	// setup amount to send
	registeredXRPToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrpl.XRPTokenIssuer.String(), xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
	)
	require.NoError(t, err)

	valueToSendFromXRPLtoCoreum, err := rippledata.NewNativeValue(int64(sendAmount))
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueToSendFromXRPLtoCoreum,
		Currency: xrpl.XRPTokenCurrency,
		Issuer:   xrpl.XRPTokenIssuer,
	}

	// generate and fund accounts
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 1)
	xrplRecipientBalanceBefore := chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, xrpl.XRPTokenIssuer, xrpl.XRPTokenCurrency,
	)
	coreumAccounts := []sdk.AccAddress{}
	xrplAccounts := []rippledata.Account{}

	t.Log("Generating and funding accounts")
	for i := 0; i < testCount; i++ {
		newCoreumAccount := chains.Coreum.GenAccount()
		coreumAccounts = append(coreumAccounts, newCoreumAccount)
		chains.Coreum.FundAccountWithOptions(ctx, t, newCoreumAccount, coreumintegration.BalancesOptions{
			Amount: sdkmath.NewIntFromUint64(1_000_000).MulRaw(int64(testCount)),
		})
		newXRPLAccount := chains.XRPL.GenAccount(ctx, t, 1)
		xrplAccounts = append(xrplAccounts, newXRPLAccount)
	}

	t.Log("Accounts generated and funded")

	startTime := time.Now()
	err = parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for i := 0; i < testCount; i++ {
			coreumAccount := coreumAccounts[i]
			xrplAccount := xrplAccounts[i]
			spawn(fmt.Sprint(i), parallel.Fail, func(ctx context.Context) error {
				err = bridgeClient.SendFromXRPLToCoreum(ctx, xrplAccount.String(), amountToSendFromXRPLtoCoreum, coreumAccount)
				if err != nil {
					return err
				}
				err = chains.Coreum.AwaitForBalance(
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
				if err != nil {
					return err
				}

				// send back to xrpl right after it is received on coreum
				return bridgeClient.SendFromCoreumToXRPL(
					ctx,
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
			})
		}
		return nil
	})
	require.NoError(t, err)

	awaitNoPendingOperations(ctx, t, contractClient)
	testDuration := time.Since(startTime)

	xrplRecipientBalanceAfter := chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, xrpl.XRPTokenIssuer, xrpl.XRPTokenCurrency,
	)
	received, err := xrplRecipientBalanceAfter.Value.Subtract(*xrplRecipientBalanceBefore.Value)
	require.NoError(t, err)
	expectedRecieved, err := rippledata.NewAmount(fmt.Sprint(sendAmount * testCount))
	require.NoError(t, err)
	require.Equal(t, expectedRecieved.Value.String(), received.String())

	t.Logf("Ran %d Operations in %s, %s per operation", testCount, testDuration, testDuration/time.Duration(testCount))
}

func awaitNoPendingOperations(ctx context.Context, t *testing.T, contractClient *coreum.ContractClient) {
	t.Helper()

	awaitState(ctx, t, func(t *testing.T) error {
		operations, err := contractClient.GetPendingOperations(ctx)
		require.NoError(t, err)
		if len(operations) != 0 {
			return errors.Errorf("there are still pending operatrions: %+v", operations)
		}
		return nil
	})
}

func awaitState(ctx context.Context, t *testing.T, stateChecker func(t *testing.T) error) {
	t.Helper()
	retryCtx, retryCancel := context.WithTimeout(ctx, time.Second/2)
	defer retryCancel()
	err := retry.Do(retryCtx, 500*time.Millisecond, func() error {
		if err := stateChecker(t); err != nil {
			return retry.Retryable(err)
		}

		return nil
	})
	require.NoError(t, err)
}
