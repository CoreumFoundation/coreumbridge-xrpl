//go:build integrationtests
// +build integrationtests

package stress_test

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	"github.com/CoreumFoundation/coreum-tools/pkg/retry"
	"github.com/CoreumFoundation/coreum/v4/pkg/client"
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
					err := bridgeClient.SendFromXRPLToCoreum(
						ctx, xrplAccount.String(), amountToSendFromXRPLtoCoreum, coreumAccount,
					)
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

func TestStressSendWithFailureAndClaimRefund(t *testing.T) {
	_, chains := integrationtests.NewTestingContext(t)
	testAccounts := 10
	iterationPerAccount := 5
	testCount := testAccounts * iterationPerAccount
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*time.Duration(testCount))
	t.Cleanup(cancel)

	contractClient := coreum.NewContractClient(
		coreum.DefaultContractClientConfig(integrationtests.GetContractAddress(t)),
		chains.Log,
		chains.Coreum.ClientContext,
	)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	xrplTxSigner := xrpl.NewKeyringTxSigner(chains.XRPL.GetSignerKeyring())
	bridgeClient := bridgeclient.NewBridgeClient(
		chains.Log,
		chains.Coreum.ClientContext,
		contractClient,
		chains.XRPL.RPCClient(),
		xrplTxSigner,
	)

	sendAmount := 1
	valueToSendFromXRPLtoCoreum, err := rippledata.NewNativeValue(int64(sendAmount))
	require.NoError(t, err)

	registeredXRPToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrpl.XRPTokenIssuer.String(), xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
	)
	// generate and fund accounts
	type xrplAccount struct {
		Acccount rippledata.Account
		Exists   bool
	}
	xrplAccounts := make([]xrplAccount, 0)
	coreumAccounts := make([]sdk.AccAddress, 0)

	t.Log("Generating and funding accounts")
	for i := 0; i < testAccounts; i++ {
		newCoreumAccount := chains.Coreum.GenAccount()
		coreumAccounts = append(coreumAccounts, newCoreumAccount)
		chains.Coreum.FundAccountWithOptions(ctx, t, newCoreumAccount, coreumintegration.BalancesOptions{
			Amount: sdkmath.NewIntFromUint64(500_000 * uint64(iterationPerAccount)),
		})
		var newXRPLAccount xrplAccount
		// every 1 in 5 accounts should be empty to simulate failure.
		if i%5 == 0 {
			newXRPLAccount = xrplAccount{chains.XRPL.GenEmptyAccount(t), false}
		} else {
			newXRPLAccount = xrplAccount{chains.XRPL.GenAccount(ctx, t, 0), true}
		}
		xrplAccounts = append(xrplAccounts, newXRPLAccount)
	}

	fundCoreumAccountsWithXRP(
		ctx,
		t,
		chains,
		*bridgeClient,
		registeredXRPToken.CoreumDenom,
		coreumAccounts,
		xrpValueMulRaw(t, valueToSendFromXRPLtoCoreum, int64(iterationPerAccount)),
	)

	t.Log("Accounts generated and funded")

	err = parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for i := 0; i < testAccounts; i++ {
			coreumAccount := coreumAccounts[i]
			xrplAccount := xrplAccounts[i]
			// accounts start with 10 initial xrp balance.
			expectedBalance, err := rippledata.NewValue("10000000", true)
			require.NoError(t, err)
			spawn(strconv.Itoa(i), parallel.Continue, func(ctx context.Context) error {
				for j := 0; j < iterationPerAccount; j++ {
					err = bridgeClient.SendFromCoreumToXRPL(
						ctx,
						coreumAccount,
						xrplAccount.Acccount,
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

					if xrplAccount.Exists {
						expectedBalance, err = expectedBalance.Add(*valueToSendFromXRPLtoCoreum)
						if err != nil {
							return err
						}
						waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Second)
						defer waitCancel()
						awaitXRPLBalance(
							waitCtx,
							t,
							chains.XRPL,
							xrplAccount.Acccount,
							xrpl.XRPTokenIssuer,
							xrpl.XRPTokenCurrency,
							*expectedBalance,
						)
					} else {
						waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Second)
						defer waitCancel()
						refunds := awaitPendingRefund(waitCtx, t, contractClient, coreumAccount)
						require.Len(t, refunds, 1)
						balanceBefore, err := bankClient.Balance(ctx, banktypes.NewQueryBalanceRequest(coreumAccount, registeredXRPToken.CoreumDenom))
						require.NoError(t, err)
						_, err = contractClient.ClaimRefund(ctx, coreumAccount, refunds[0].ID)
						require.NoError(t, err)
						balanceAfter, err := bankClient.Balance(ctx, banktypes.NewQueryBalanceRequest(coreumAccount, registeredXRPToken.CoreumDenom))
						require.NoError(t, err)
						balanceChange := balanceAfter.GetBalance().Amount.Sub(balanceBefore.Balance.Amount)
						require.EqualValues(t, balanceChange.Int64(), sendAmount)
					}
				}
				return nil
			})
		}
		return nil
	})
	require.NoError(t, err)
}

func xrpValueMulRaw(t *testing.T, rValue *rippledata.Value, n int64) *rippledata.Value {
	var nValue *rippledata.Value
	var err error
	if rValue.IsNative() {
		nValue, err = rippledata.NewNativeValue(n)
		require.NoError(t, err)
	} else {
		nValue, err = rippledata.NewNonNativeValue(n, 0)
		require.NoError(t, err)
	}
	res, err := rValue.Multiply(*nValue)
	require.NoError(t, err)
	return res
}

func fundCoreumAccountsWithXRP(
	ctx context.Context,
	t *testing.T,
	chains integrationtests.Chains,
	bridgeClient bridgeclient.BridgeClient,
	registeredXrpDenomOnCoreum string,
	coreumAccounts []sdk.AccAddress,
	xrpToEachAccount *rippledata.Value,
) {
	coreumAccount := chains.Coreum.GenAccount()
	totalSendValue := xrpValueMulRaw(t, xrpToEachAccount, int64(len(coreumAccounts)))
	xrplAccount := chains.XRPL.GenAccount(ctx, t, totalSendValue.Float())
	xrpAmount := rippledata.Amount{
		Value:    totalSendValue,
		Currency: xrpl.XRPTokenCurrency,
		Issuer:   xrpl.XRPTokenIssuer,
	}
	err := bridgeClient.SendFromXRPLToCoreum(
		ctx, xrplAccount.String(), xrpAmount, coreumAccount,
	)
	require.NoError(t, err)
	err = chains.Coreum.AwaitForBalance(
		ctx,
		t,
		coreumAccount,
		sdk.NewCoin(
			registeredXrpDenomOnCoreum,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				totalSendValue.String(),
				xrpl.XRPCurrencyDecimals,
			)),
	)
	require.NoError(t, err)

	sdkIntAmount := sdkmath.NewInt(int64(math.Ceil(xrpToEachAccount.Float() * 1_000_000)))
	msg := &banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{{
			Address: coreumAccount.String(),
			Coins:   sdk.NewCoins(sdk.NewCoin(registeredXrpDenomOnCoreum, sdkIntAmount.MulRaw(int64(len(coreumAccounts))))),
		}},
		Outputs: []banktypes.Output{},
	}
	for _, acc := range coreumAccounts {
		acc := acc
		msg.Outputs = append(msg.Outputs, banktypes.Output{
			Address: acc.String(),
			Coins:   sdk.NewCoins(sdk.NewCoin(registeredXrpDenomOnCoreum, sdkIntAmount)),
		})
	}
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumAccount, coreumintegration.BalancesOptions{
		Messages: []sdk.Msg{msg},
	})

	_, err = client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumAccount),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		msg,
	)
	require.NoError(t, err)
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

func awaitPendingRefund(
	ctx context.Context,
	t *testing.T,
	contractClient *coreum.ContractClient,
	account sdk.AccAddress,
) []coreum.PendingRefund {
	t.Helper()
	var refunds []coreum.PendingRefund
	awaitState(ctx, t, func(t *testing.T) error {
		var err error
		refunds, err = contractClient.GetPendingRefunds(ctx, account)
		if err != nil {
			return err
		}
		if len(refunds) == 0 {
			return errors.Errorf("no pending refunds for address %s", account.String())
		}
		return nil
	})
	return refunds
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
