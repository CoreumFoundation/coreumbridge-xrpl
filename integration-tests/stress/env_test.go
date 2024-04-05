//go:build integrationtests
// +build integrationtests

package stress_test

import (
	"context"
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

// EnvConfig is stress test env config.
type EnvConfig struct {
	TestTimeout             time.Duration
	TestCaseTimeout         time.Duration
	AwaitStateTimeout       time.Duration
	ParallelExecutionNumber int
	RepeatTestCaseCount     int
	RepeatOwnerActionCount  int
	OwnerActionDelay        time.Duration
}

// DefaultEnvConfig returns default env config.
func DefaultEnvConfig() EnvConfig {
	return EnvConfig{
		TestTimeout:             time.Hour,
		TestCaseTimeout:         5 * time.Minute,
		AwaitStateTimeout:       time.Second,
		ParallelExecutionNumber: 3,
		RepeatTestCaseCount:     5,
		RepeatOwnerActionCount:  10,
		OwnerActionDelay:        5 * time.Second,
	}
}

// Env is stress test env.
type Env struct {
	Cfg            EnvConfig
	Chains         integrationtests.Chains
	ContractOwner  sdk.AccAddress
	BridgeClient   *bridgeclient.BridgeClient
	ContractClient *coreum.ContractClient
}

// NewEnv returns new instance of Env.
func NewEnv(t *testing.T, cfg EnvConfig) *Env {
	_, chains := integrationtests.NewTestingContext(t)
	bridgeCfg := integrationtests.GetBridgeConfig()

	contractClient := coreum.NewContractClient(
		coreum.DefaultContractClientConfig(sdk.MustAccAddressFromBech32(bridgeCfg.ContractAddress)),
		chains.Log,
		chains.Coreum.ClientContext,
	)

	bridgeClient := bridgeclient.NewBridgeClient(
		chains.Log,
		chains.Coreum.ClientContext,
		contractClient,
		chains.XRPL.RPCClient(),
		xrpl.NewKeyringTxSigner(chains.XRPL.GetSignerKeyring()),
	)

	// import contract owner mnemonic
	contractOwner := chains.Coreum.ChainContext.ImportMnemonic(bridgeCfg.OwnerMnemonic)

	return &Env{
		Cfg:            cfg,
		Chains:         chains,
		ContractOwner:  contractOwner,
		BridgeClient:   bridgeClient,
		ContractClient: contractClient,
	}
}

// NewBridgeClient returns new instance of BridgeClient.
func (env *Env) NewBridgeClient() *bridgeclient.BridgeClient {
	bridgeCfg := integrationtests.GetBridgeConfig()
	contractClient := coreum.NewContractClient(
		coreum.DefaultContractClientConfig(sdk.MustAccAddressFromBech32(bridgeCfg.ContractAddress)),
		env.Chains.Log,
		env.Chains.Coreum.ClientContext,
	)

	return bridgeclient.NewBridgeClient(
		env.Chains.Log,
		env.Chains.Coreum.ClientContext,
		contractClient,
		env.Chains.XRPL.RPCClient(),
		xrpl.NewKeyringTxSigner(env.Chains.XRPL.GetSignerKeyring()),
	)
}

// FundCoreumAccountsWithXRP funds the Coreum accounts with the particular XRP token thought the bridge on the
// Coreum chain.
func (env *Env) FundCoreumAccountsWithXRP(
	ctx context.Context,
	t *testing.T,
	coreumAccounts []sdk.AccAddress,
	amount sdkmath.Int,
) {
	totalXRPLValueToSend, err := rippledata.NewNativeValue(int64(amount.Uint64() * uint64(len(coreumAccounts))))
	require.NoError(t, err)

	xrplFaucetAccount := env.Chains.XRPL.GenAccount(ctx, t, totalXRPLValueToSend.Float())

	registeredXRPToken, err := env.ContractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrpl.XRPTokenIssuer.String(), xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
	)
	require.NoError(t, err)

	xrpAmount := rippledata.Amount{
		Value:    totalXRPLValueToSend,
		Currency: xrpl.XRPTokenCurrency,
		Issuer:   xrpl.XRPTokenIssuer,
	}

	coreumFaucetAccount := env.Chains.Coreum.GenAccount()

	require.NoError(
		t, env.BridgeClient.SendFromXRPLToCoreum(ctx, xrplFaucetAccount.String(), xrpAmount, coreumFaucetAccount),
	)

	require.NoError(t, env.AwaitCoreumBalance(
		ctx,
		coreumFaucetAccount,
		sdk.NewCoin(
			registeredXRPToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				totalXRPLValueToSend.String(),
				xrpl.XRPCurrencyDecimals,
			)),
	))

	msg := &banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{{
			Address: coreumFaucetAccount.String(),
			Coins:   sdk.NewCoins(sdk.NewCoin(registeredXRPToken.CoreumDenom, amount.MulRaw(int64(len(coreumAccounts))))),
		}},
		Outputs: []banktypes.Output{},
	}
	for _, acc := range coreumAccounts {
		acc := acc
		msg.Outputs = append(msg.Outputs, banktypes.Output{
			Address: acc.String(),
			Coins:   sdk.NewCoins(sdk.NewCoin(registeredXRPToken.CoreumDenom, amount)),
		})
	}
	env.Chains.Coreum.FundAccountWithOptions(ctx, t, coreumFaucetAccount, coreumintegration.BalancesOptions{
		Messages: []sdk.Msg{msg},
	})

	_, err = client.BroadcastTx(
		ctx,
		env.Chains.Coreum.ClientContext.WithFromAddress(coreumFaucetAccount),
		env.Chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		msg,
	)
	require.NoError(t, err)
}

// GenCoreumAndXRPLAccounts generates Coreum and XRPL accounts.
func (env *Env) GenCoreumAndXRPLAccounts(
	ctx context.Context,
	t *testing.T,
	coreumAccountCount int,
	coreumAccountAmount sdkmath.Int,
	xrplAccountCount int,
	xrplAccountAmount float64,
) ([]sdk.AccAddress, []rippledata.Account) {
	var (
		coreumAccounts []sdk.AccAddress
		xrplAccounts   []rippledata.Account
	)
	require.NoError(t, parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		spawn("gen-coreum-accounts", parallel.Continue, func(ctx context.Context) error {
			coreumAccounts = env.GenCoreumAccounts(ctx, t, coreumAccountCount, coreumAccountAmount)
			return nil
		})
		spawn("gen-xrpl-accounts", parallel.Continue, func(ctx context.Context) error {
			xrplAccounts = env.GenXRPLAccounts(ctx, t, xrplAccountCount, xrplAccountAmount)
			return nil
		})
		return nil
	}))

	return coreumAccounts, xrplAccounts
}

// GenCoreumAccounts generates coreum accounts with the provided amount.
func (env *Env) GenCoreumAccounts(ctx context.Context, t *testing.T, count int, amount sdkmath.Int) []sdk.AccAddress {
	coreumAccounts := make([]sdk.AccAddress, 0, count)
	for i := 0; i < count; i++ {
		acc := env.Chains.Coreum.GenAccount()
		env.Chains.Coreum.FundAccountWithOptions(ctx, t, acc, coreumintegration.BalancesOptions{
			Amount: amount,
		})
		coreumAccounts = append(coreumAccounts, acc)
	}

	return coreumAccounts
}

// GenXRPLAccounts generates XRPL accounts.
func (env *Env) GenXRPLAccounts(ctx context.Context, t *testing.T, count int, amount float64) []rippledata.Account {
	xrplAccounts := make([]rippledata.Account, 0, count)
	for i := 0; i < count; i++ {
		acc := env.Chains.XRPL.GenAccount(ctx, t, amount)
		xrplAccounts = append(xrplAccounts, acc)
	}

	return xrplAccounts
}

// AwaitCoreumBalance waits for expected coreum balance.
func (env *Env) AwaitCoreumBalance(
	ctx context.Context,
	address sdk.AccAddress,
	expectedBalance sdk.Coin,
) error {
	bankClient := banktypes.NewQueryClient(env.Chains.Coreum.ClientContext)
	return env.AwaitState(ctx, func() error {
		balancesRes, err := bankClient.AllBalances(ctx, &banktypes.QueryAllBalancesRequest{
			Address: address.String(),
		})
		if err != nil {
			return err
		}

		if balancesRes.Balances.AmountOf(expectedBalance.Denom).String() != expectedBalance.Amount.String() {
			return retry.Retryable(errors.Errorf(
				"balance of %s is not as expected, all balances: %s",
				expectedBalance.String(),
				balancesRes.Balances.String()),
			)
		}

		return nil
	})
}

// AwaitXRPLBalance awaits for the balance on the XRPL change.
func (env *Env) AwaitXRPLBalance(
	ctx context.Context,
	account rippledata.Account,
	amount rippledata.Amount,
) error {
	return env.AwaitState(ctx, func() error {
		balances, err := env.Chains.XRPL.RPCClient().GetXRPLBalances(ctx, account)
		if err != nil {
			return err
		}
		for _, balance := range balances {
			if balance.String() == amount.String() {
				return nil
			}
		}
		return errors.Errorf("balance is not euqal to expected (%+v), all balances:%v", amount.String(), balances)
	})
}

// AwaitPendingRefund awaits for pending refunds on the Coreum change.
func (env *Env) AwaitPendingRefund(
	ctx context.Context,
	account sdk.AccAddress,
) ([]coreum.PendingRefund, error) {
	var refunds []coreum.PendingRefund
	if err := env.AwaitState(ctx, func() error {
		var err error
		refunds, err = env.ContractClient.GetPendingRefunds(ctx, account)
		if err != nil {
			return err
		}
		if len(refunds) == 0 {
			return errors.Errorf("no pending refunds for address %s", account.String())
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return refunds, nil
}

// AwaitState awaits for particular state.
func (env *Env) AwaitState(ctx context.Context, stateChecker func() error) error {
	return retry.Do(ctx, env.Cfg.AwaitStateTimeout, func() error {
		if err := stateChecker(); err != nil {
			return retry.Retryable(err)
		}

		return nil
	})
}

// RepeatOwnerActionWithDelay calls the action, waits for some time and call the actionCompensation.
func (env *Env) RepeatOwnerActionWithDelay(ctx context.Context, action, rollbackAction func() error) error {
	for j := 0; j < env.Cfg.RepeatOwnerActionCount; j++ {
		if err := env.callAdminAction(ctx, action, rollbackAction); err != nil {
			return err
		}
	}
	return nil
}

// AwaitContractCall awaits for the call to the contract to be executed if the error is expected.
func (env *Env) AwaitContractCall(ctx context.Context, call func() error) error {
	return retry.Do(ctx, env.Cfg.AwaitStateTimeout, func() error {
		if err := call(); err != nil {
			if coreum.IsTokenNotEnabledError(err) ||
				coreum.IsBridgeHaltedError(err) ||
				coreum.IsNoAvailableTicketsError(err) ||
				coreum.IsLastTicketReservedError(err) {
				return retry.Retryable(err)
			}
			return err
		}

		return nil
	})
}

func (env *Env) callAdminAction(ctx context.Context, action func() error, rollbackAction func() error) error {
	ctx, cancel := context.WithTimeout(ctx, env.Cfg.TestCaseTimeout)
	defer cancel()
	// use common BridgeClient to prevent sequence mismatch
	if err := env.AwaitContractCall(ctx, func() error {
		return action()
	}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(env.Cfg.OwnerActionDelay):
	}
	// use common BridgeClient to prevent sequence mismatch
	return env.AwaitContractCall(ctx, func() error {
		return rollbackAction()
	})
}
