//go:build integrationtests
// +build integrationtests

package stress_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	"github.com/CoreumFoundation/coreum-tools/pkg/retry"
	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestStressSendFromXRPLToCoreumAndBack(t *testing.T) {
	_, chains := integrationtests.NewTestingContext(t)
	testAccounts := 2
	iterationPerAccount := 30
	testCount := testAccounts * iterationPerAccount
	sendAmount := 1
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*time.Duration(testCount)+5*time.Minute)
	t.Cleanup(cancel)

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
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	xrplRecipientBalanceBefore := chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, xrpl.XRPTokenIssuer, xrpl.XRPTokenCurrency,
	)
	coreumAccounts := make([]sdk.AccAddress, 0)
	xrplAccounts := make([]rippledata.Account, 0)

	t.Log("Generating and funding accounts")
	for i := 0; i < testAccounts; i++ {
		newCoreumAccount := chains.Coreum.GenAccount()
		coreumAccounts = append(coreumAccounts, newCoreumAccount)
		chains.Coreum.FundAccountWithOptions(ctx, t, newCoreumAccount, coreumintegration.BalancesOptions{
			Amount: sdkmath.NewIntFromUint64(500_000 * uint64(iterationPerAccount)),
		})
		newXRPLAccount := chains.XRPL.GenAccount(ctx, t, 0.1*float64(iterationPerAccount))
		xrplAccounts = append(xrplAccounts, newXRPLAccount)
	}

	t.Log("Accounts generated and funded")

	startTime := time.Now()
	err = parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for i := 0; i < testAccounts; i++ {
			coreumAccount := coreumAccounts[i]
			xrplAccount := xrplAccounts[i]
			spawn(strconv.Itoa(i), parallel.Continue, func(ctx context.Context) error {
				for j := 0; j < iterationPerAccount; j++ {
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
					err = bridgeClient.SendFromCoreumToXRPL(
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
					if err != nil {
						return err
					}
				}
				return nil
			})
		}
		return nil
	})
	require.NoError(t, err)

	expectedReceived, err := rippledata.NewNativeValue(int64(sendAmount * testCount))
	require.NoError(t, err)
	expectedCurrentBalance, err := expectedReceived.Add(*xrplRecipientBalanceBefore.Value)
	require.NoError(t, err)

	waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Second)
	defer waitCancel()
	awaitXRPLBalance(
		waitCtx,
		t,
		chains.XRPL,
		xrplRecipientAddress,
		xrpl.XRPTokenIssuer,
		xrpl.XRPTokenCurrency,
		*expectedCurrentBalance,
	)
	testDuration := time.Since(startTime)

	xrplRecipientBalanceAfter := chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, xrpl.XRPTokenIssuer, xrpl.XRPTokenCurrency,
	)
	received, err := xrplRecipientBalanceAfter.Value.Subtract(*xrplRecipientBalanceBefore.Value)
	require.NoError(t, err)
	require.Equal(t, expectedReceived.String(), received.String())

	t.Logf("Ran %d Operations in %s, %s per operation", testCount, testDuration, testDuration/time.Duration(testCount))
}

func awaitXRPLBalance(
	ctx context.Context,
	t *testing.T,
	xrpl integrationtests.XRPLChain,
	account rippledata.Account,
	issuer rippledata.Account,
	currency rippledata.Currency,
	expectedBalance rippledata.Value,
) {
	t.Helper()
	awaitState(ctx, t, func(t *testing.T) error {
		balance := xrpl.GetAccountBalance(ctx, t, account, issuer, currency)
		if !balance.Value.Equals(expectedBalance) {
			return errors.Errorf("balance (%+v) is not euqal to expected (%+v)", balance.Value, expectedBalance)
		}
		return nil
	})
}

func awaitState(ctx context.Context, t *testing.T, stateChecker func(t *testing.T) error) {
	t.Helper()
	err := retry.Do(ctx, 500*time.Millisecond, func() error {
		if err := stateChecker(t); err != nil {
			return retry.Retryable(err)
		}

		return nil
	})
	require.NoError(t, err)
}
