//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v5/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestRecoverXRPLOriginatedTokenRegistrationAndSendFromXRPLToCoreumAndBack(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumSender := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})
	t.Logf("Coreum sender: %s", coreumSender.String())
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient: %s", xrplRecipientAddress.String())

	registeredXRPLCurrency := integrationtests.GenerateXRPLCurrency(t)

	// register the XRPL token and await for the tx to be failed
	runnerEnv.Chains.Coreum.FundAccountWithOptions(ctx, t, runnerEnv.ContractOwner, coreumintegration.BalancesOptions{
		Amount: runnerEnv.Chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount,
	})

	// gen account but don't fund it to let the tx to fail since the account won't exist on the XRPL side
	xrplIssuerAddress := chains.XRPL.GenEmptyAccount(t)

	_, err := runnerEnv.ContractClient.RegisterXRPLToken(
		ctx,
		runnerEnv.ContractOwner,
		xrplIssuerAddress.String(),
		xrpl.ConvertCurrencyToString(registeredXRPLCurrency),
		int32(6),
		integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30),
		sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	runnerEnv.AwaitNoPendingOperations(ctx, t)
	registeredXRPLToken, err := runnerEnv.ContractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrplIssuerAddress.String(), xrpl.ConvertCurrencyToString(registeredXRPLCurrency),
	)
	require.NoError(t, err)
	require.Equal(t, coreum.TokenStateInactive, registeredXRPLToken.State)

	// create the account on the XRPL and send some XRP on top to cover fees
	runnerEnv.Chains.XRPL.CreateAccount(ctx, t, xrplIssuerAddress, 1)
	// recover from owner
	_, err = runnerEnv.ContractClient.RecoverXRPLTokenRegistration(
		ctx, runnerEnv.ContractOwner, xrplIssuerAddress.String(), xrpl.ConvertCurrencyToString(registeredXRPLCurrency),
	)
	require.NoError(t, err)
	runnerEnv.AwaitNoPendingOperations(ctx, t)
	// now the token is enabled
	registeredXRPLToken, err = runnerEnv.ContractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrplIssuerAddress.String(), xrpl.ConvertCurrencyToString(registeredXRPLCurrency),
	)
	require.NoError(t, err)
	require.Equal(t, coreum.TokenStateEnabled, registeredXRPLToken.State)

	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)

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

	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSender,
		xrplRecipientAddress,
		sdk.NewCoin(
			registeredXRPLToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				valueToSendFromXRPLtoCoreum.String(),
				xrpl.XRPLIssuedTokenDecimals),
		),
		nil,
	)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx,
		t,
		xrplRecipientAddress,
		xrplIssuerAddress,
		registeredXRPLCurrency,
	)
	require.Equal(t, valueToSendFromXRPLtoCoreum.String(), xrplRecipientBalance.Value.String())
}

func TestXRPLOriginatedTokenRegistrationForAccountWithDisallowIncomingTrustLine(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	registeredXRPLCurrency := integrationtests.GenerateXRPLCurrency(t)
	runnerEnv.Chains.Coreum.FundAccountWithOptions(ctx, t, runnerEnv.ContractOwner, coreumintegration.BalancesOptions{
		Amount: runnerEnv.Chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount,
	})

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)
	setTransferRateTx := rippledata.AccountSet{
		SetFlag: lo.ToPtr(uint32(15)), // asfDisallowIncomingTrustline
		TxBase: rippledata.TxBase{
			Account:         xrplIssuerAddress,
			TransactionType: rippledata.ACCOUNT_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &setTransferRateTx, xrplIssuerAddress))

	_, err := runnerEnv.ContractClient.RegisterXRPLToken(
		ctx,
		runnerEnv.ContractOwner,
		xrplIssuerAddress.String(),
		xrpl.ConvertCurrencyToString(registeredXRPLCurrency),
		int32(6),
		integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30),
		sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	registeredXRPLToken, err := runnerEnv.ContractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrplIssuerAddress.String(), xrpl.ConvertCurrencyToString(registeredXRPLCurrency),
	)
	require.NoError(t, err)
	require.Equal(t, coreum.TokenStateInactive, registeredXRPLToken.State)
}
