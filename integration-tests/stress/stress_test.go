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
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestStress(t *testing.T) {
	t.Parallel()

	envCfg := DefaultEnvConfig()
	env := NewEnv(t, envCfg)

	ctx, cancel := context.WithTimeout(context.Background(), envCfg.TestTimeout)
	t.Cleanup(cancel)

	type testCase struct {
		name     string
		testCase func(context.Context, *testing.T, *Env)
	}
	tests := []testCase{
		{
			name:     "send_XRP_from_XRPL_and_back",
			testCase: sendXRPFromXRPLAndBack,
		},
		{
			name:     "send_to_XRPL_with_failure_and_claim_refund",
			testCase: sendWithFailureAndClaimRefund,
		},
		{
			name:     "enable_and_disable_XRP_token",
			testCase: enableAndDisableXRPToken,
		},
		{
			name:     "halt_and_resume_bridge",
			testCase: haltAndResumeBridge,
		},
		{
			name:     "change_XRPL_base_fee_to_low_and_back",
			testCase: changeXRPLBaseFeeToLowAndBack,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			t.Logf("Running test:%s", tt.name)
			startTime := time.Now()
			tt.testCase(ctx, t, env)
			t.Logf("Test finished test:%s, time spent:%s", tt.name, time.Since(startTime))
		})
	}
}

func sendXRPFromXRPLAndBack(ctx context.Context, t *testing.T, env *Env) {
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

	registeredXRPToken, err := env.BridgeClient.GetXRPLTokenByIssuerAndCurrency(
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

	require.NoError(t, parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for i := range env.Cfg.ParallelExecutionNumber {
			coreumAccount := coreumAccounts[i]
			xrplAccount := xrplAccounts[i]
			spawn(strconv.Itoa(i), parallel.Continue, func(ctx context.Context) error {
				// get new instance of the bridge client to allow parallel execution for each account
				bridgeClient := env.NewBridgeClient()
				for range env.Cfg.RepeatTestCaseCount {
					if err := func() error {
						ctx, cancel := context.WithTimeout(ctx, env.Cfg.TestCaseTimeout)
						defer cancel()

						_, err := bridgeClient.SendFromXRPLToCoreum(
							ctx, xrplAccount.String(), amountToSendFromXRPLtoCoreum, coreumAccount,
						)
						if err != nil {
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
						if err = env.AwaitContractCall(ctx, func() error {
							_, err := bridgeClient.SendFromCoreumToXRPL(
								ctx,
								coreumAccount,
								xrplAccount,
								coreumAmount,
								nil,
							)
							return err
						}); err != nil {
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
	}))
}

func sendWithFailureAndClaimRefund(ctx context.Context, t *testing.T, env *Env) {
	coreumAccounts := env.GenCoreumAccounts(
		ctx,
		t,
		env.Cfg.ParallelExecutionNumber,
		sdkmath.NewIntWithDecimal(2, 5).MulRaw(int64(env.Cfg.RepeatTestCaseCount)),
	)

	amountToSendFromCoreumXRPL := sdkmath.NewInt(1)

	registeredXRPToken, err := env.BridgeClient.GetXRPLTokenByIssuerAndCurrency(
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

	require.NoError(t, parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		for i := range env.Cfg.ParallelExecutionNumber {
			coreumAccount := coreumAccounts[i]
			spawn(strconv.Itoa(i), parallel.Continue, func(ctx context.Context) error {
				// get new instance of the bridge client to allow parallel execution for each account
				bridgeClient := env.NewBridgeClient()
				for range env.Cfg.RepeatTestCaseCount {
					if err := func() error {
						ctx, cancel := context.WithTimeout(ctx, env.Cfg.TestCaseTimeout)
						defer cancel()

						xrplAccount := xrpl.GenPrivKeyTxSigner().Account()

						if err = env.AwaitContractCall(ctx, func() error {
							_, err := bridgeClient.SendFromCoreumToXRPL(
								ctx,
								coreumAccount,
								xrplAccount,
								sdk.NewCoin(
									registeredXRPToken.CoreumDenom,
									amountToSendFromCoreumXRPL),
								nil,
							)
							return err
						}); err != nil {
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

						if err = env.AwaitContractCall(ctx, func() error {
							return bridgeClient.ClaimRefund(ctx, coreumAccount, refunds[0].ID)
						}); err != nil {
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
	}))
}

func enableAndDisableXRPToken(ctx context.Context, t *testing.T, env *Env) {
	require.NoError(t, env.RepeatOwnerActionWithDelay(
		ctx,
		func() error {
			return env.BridgeClient.UpdateXRPLToken(
				ctx,
				env.ContractOwner,
				xrpl.XRPTokenIssuer.String(),
				xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
				lo.ToPtr(coreum.TokenStateDisabled),
				nil,
				nil,
				nil,
			)
		},
		func() error {
			return env.BridgeClient.UpdateXRPLToken(
				ctx,
				env.ContractOwner,
				xrpl.XRPTokenIssuer.String(),
				xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
				lo.ToPtr(coreum.TokenStateEnabled),
				nil,
				nil,
				nil,
			)
		},
	),
	)
}

func haltAndResumeBridge(ctx context.Context, t *testing.T, env *Env) {
	require.NoError(t, env.RepeatOwnerActionWithDelay(
		ctx,
		func() error {
			return env.BridgeClient.HaltBridge(
				ctx,
				env.ContractOwner,
			)
		},
		func() error {
			return env.BridgeClient.ResumeBridge(
				ctx,
				env.ContractOwner,
			)
		},
	),
	)
}

func changeXRPLBaseFeeToLowAndBack(ctx context.Context, t *testing.T, env *Env) {
	contractCfg, err := env.BridgeClient.GetContractConfig(ctx)
	require.NoError(t, err)
	initialXRPLBaseFee := contractCfg.XRPLBaseFee

	require.NoError(t, env.RepeatOwnerActionWithDelay(
		ctx,
		func() error {
			return env.BridgeClient.UpdateXRPLBaseFee(
				ctx,
				env.ContractOwner,
				// low base XRPL fee, so the XRPL transactions won't pass
				1,
			)
		},
		func() error {
			return env.BridgeClient.UpdateXRPLBaseFee(
				ctx,
				env.ContractOwner,
				// normal base XRPL fee, so the XRPL transactions pass
				initialXRPLBaseFee,
			)
		},
	),
	)
}
