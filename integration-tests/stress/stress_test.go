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
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestStress(t *testing.T) {
	envCfg := DefaultEnvConfig()
	env := NewEnv(t, envCfg)

	ctx, cancel := context.WithTimeout(context.Background(), envCfg.TestTimeout)
	t.Cleanup(cancel)

	type testCase struct {
		name     string
		testCase func(context.Context, *testing.T, *Env) error
	}
	tests := []testCase{
		{
			name:     "send-XRP-from-xrpl-and-back",
			testCase: sendXRPFromXRPLAndBack,
		},
		{
			name:     "send-to-XRPL-with-failure-and-claim-refund",
			testCase: sendWithFailureAndClaimRefund,
		},
	}

	require.NoError(t, parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for _, test := range tests {
			test := test
			spawn(test.name, parallel.Continue, func(ctx context.Context) error {
				t.Logf("Running test:%s", test.name)
				startTime := time.Now()
				if err := test.testCase(ctx, t, env); err != nil {
					return errors.Wrapf(err, "test failed: %s", test.name)
				}
				t.Logf("Test finished test:%s, time spent:%s", test.name, time.Since(startTime))
				return nil
			})
		}
		return nil
	}))
}

func sendXRPFromXRPLAndBack(ctx context.Context, t *testing.T, env *Env) error {
	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		coreumAccounts, xrplAccounts := env.GenCoreumAndXRPLAccounts(
			ctx,
			t,
			env.Cfg.ParallelExecutionNumber,
			sdkmath.NewIntWithDecimal(1, 5).MulRaw(int64(env.Cfg.RepeatTestCaseCount)),
			env.Cfg.ParallelExecutionNumber,
			0.1,
		)

		valueToSendFromXRPLtoCoreum, err := rippledata.NewNativeValue(1)
		require.NoError(t, err)
		amountToSendFromXRPLtoCoreum := rippledata.Amount{
			Value:    valueToSendFromXRPLtoCoreum,
			Currency: xrpl.XRPTokenCurrency,
			Issuer:   xrpl.XRPTokenIssuer,
		}

		registeredXRPToken, err := env.ContractClient.GetXRPLTokenByIssuerAndCurrency(
			ctx, xrpl.XRPTokenIssuer.String(), xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
		)
		require.NoError(t, err)

		coreumAmount := sdk.NewCoin(
			registeredXRPToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				valueToSendFromXRPLtoCoreum.String(),
				xrpl.XRPCurrencyDecimals,
			))

		for i := 0; i < env.Cfg.ParallelExecutionNumber; i++ {
			coreumAccount := coreumAccounts[i]
			xrplAccount := xrplAccounts[i]
			spawn(strconv.Itoa(i), parallel.Continue, func(ctx context.Context) error {
				for j := 0; j < env.Cfg.RepeatTestCaseCount; j++ {
					if err := func() error {
						ctx, cancel := context.WithTimeout(ctx, env.Cfg.TestCaseTimeout)
						defer cancel()

						if err := env.BridgeClient.SendFromXRPLToCoreum(
							ctx, xrplAccount.String(), amountToSendFromXRPLtoCoreum, coreumAccount,
						); err != nil {
							return err
						}

						if err := env.AwaitCoreumBalance(
							ctx,
							coreumAccount,
							coreumAmount,
						); err != nil {
							return err
						}

						xrpBalanceBefore, err := env.Chains.XRPL.RPCClient().GetXRPLBalance(
							ctx,
							xrplAccount,
							xrpl.XRPTokenCurrency,
							xrpl.XRPTokenIssuer,
						)
						if err != nil {
							return err
						}

						// send back to XRPL
						if err := env.BridgeClient.SendFromCoreumToXRPL(
							ctx,
							coreumAccount,
							xrplAccount,
							coreumAmount,
							nil,
						); err != nil {
							return err
						}

						xrplValueAfter, err := xrpBalanceBefore.Value.Add(*valueToSendFromXRPLtoCoreum)
						if err != nil {
							return err
						}

						return env.AwaitXRPLBalance(ctx, xrplAccount, rippledata.Amount{
							Value:    xrplValueAfter,
							Currency: xrpl.XRPTokenCurrency,
							Issuer:   xrpl.XRPTokenIssuer,
						})
					}(); err != nil {
						return err
					}
				}
				return nil
			})
		}
		return nil
	})
}

func sendWithFailureAndClaimRefund(ctx context.Context, t *testing.T, env *Env) error {
	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		coreumAccounts := env.GenCoreumAccounts(
			ctx,
			t,
			env.Cfg.ParallelExecutionNumber,
			sdkmath.NewIntWithDecimal(2, 5).MulRaw(int64(env.Cfg.RepeatTestCaseCount)),
		)

		amountToSendFromCoreumXRPL := sdkmath.NewInt(1)

		registeredXRPToken, err := env.ContractClient.GetXRPLTokenByIssuerAndCurrency(
			ctx, xrpl.XRPTokenIssuer.String(), xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
		)
		require.NoError(t, err)
		env.FundCoreumAccountsWithXRP(
			ctx,
			t,
			coreumAccounts,
			amountToSendFromCoreumXRPL,
		)

		bankClient := banktypes.NewQueryClient(env.Chains.Coreum.ClientContext)

		for i := 0; i < env.Cfg.ParallelExecutionNumber; i++ {
			coreumAccount := coreumAccounts[i]
			spawn(strconv.Itoa(i), parallel.Continue, func(ctx context.Context) error {
				for j := 0; j < env.Cfg.RepeatTestCaseCount; j++ {
					if err := func() error {
						ctx, cancel := context.WithTimeout(ctx, env.Cfg.TestCaseTimeout)
						defer cancel()

						xrplAccount := xrpl.GenPrivKeyTxSigner().Account()

						if err = env.BridgeClient.SendFromCoreumToXRPL(
							ctx,
							coreumAccount,
							xrplAccount,
							sdk.NewCoin(
								registeredXRPToken.CoreumDenom,
								amountToSendFromCoreumXRPL),
							nil,
						); err != nil {
							return err
						}

						refunds, err := env.AwaitPendingRefund(ctx, coreumAccount)
						if err != nil {
							return err
						}
						if len(refunds) != 1 {
							return errors.Errorf("got unexpected number of refunds, refunds:%v", refunds)
						}
						balanceBefore, err := bankClient.Balance(
							ctx, banktypes.NewQueryBalanceRequest(coreumAccount, registeredXRPToken.CoreumDenom),
						)
						if err != nil {
							return err
						}
						if err = env.BridgeClient.ClaimRefund(ctx, coreumAccount, refunds[0].ID); err != nil {
							return err
						}
						balanceAfter, err := bankClient.Balance(
							ctx, banktypes.NewQueryBalanceRequest(coreumAccount, registeredXRPToken.CoreumDenom),
						)
						if err != nil {
							return err
						}
						balanceChange := balanceAfter.GetBalance().Amount.Sub(balanceBefore.Balance.Amount)
						if balanceChange.String() != amountToSendFromCoreumXRPL.String() {
							return errors.Errorf(
								"got unexpected balance change exected %s, got:%s",
								balanceChange.String(), amountToSendFromCoreumXRPL.String(),
							)
						}

						return nil
					}(); err != nil {
						return err
					}
				}
				return nil
			})
		}
		return nil
	})
}
