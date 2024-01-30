//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum/v4/pkg/client"
	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	assetfttypes "github.com/CoreumFoundation/coreum/v4/x/asset/ft/types"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestUpdateXRPLBaseFee(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	runnerEnvCfg := DefaultRunnerEnvConfig()
	// set 32 relayers and signing threshold eq to 32, to have enough min required fee to fail expected XRPL transaction
	runnerEnvCfg.SigningThreshold = xrpl.MaxAllowedXRPLSigners
	runnerEnvCfg.RelayersCount = xrpl.MaxAllowedXRPLSigners

	runnerEnv := NewRunnerEnv(ctx, t, runnerEnvCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, 200)

	// update the XRPL base fee to 1, to make the fee not enough,
	// since expected fee is (32 + 1) * 10 = 330,
	// but will be 33 * 1 = 33
	require.NoError(t, runnerEnv.BridgeClient.UpdateXRPLBaseFee(ctx, runnerEnv.ContractOwner, 1))

	// register a token and transfer from Coreum to XRPL
	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewInt(1_000_000)),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	// issue asset ft and register it
	sendingPrecision := int32(6)
	tokenDecimals := uint32(6)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 30)
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSenderAddress.String(),
		Symbol:        "symbol",
		Subunit:       "subunit",
		Precision:     tokenDecimals, // token decimals in terms of the contract
		InitialAmount: maxHoldingAmount,
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)

	registeredCoreumOriginatedToken := runnerEnv.RegisterCoreumOriginatedToken(
		ctx,
		t,
		// use Coreum denom
		assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress),
		tokenDecimals,
		sendingPrecision,
		sdkmath.NewIntWithDecimal(1, 30),
		sdkmath.NewInt(40),
	)

	// send TrustSet to be able to receive coins from the bridge
	xrplCurrency, err := rippledata.NewCurrency(registeredCoreumOriginatedToken.XRPLCurrency)
	require.NoError(t, err)
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency)

	amountToSendToXRPL := sdkmath.NewInt(1000040)
	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSenderAddress,
		xrplRecipientAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
		nil,
	)
	runnerEnv.AwaitState(ctx, t, func(t *testing.T) error {
		pendingOperations, err := runnerEnv.ContractClient.GetPendingOperations(ctx)
		require.NoError(t, err)
		if len(pendingOperations) == 1 && len(pendingOperations[0].Signatures) == int(runnerEnvCfg.SigningThreshold) {
			return nil
		}
		return errors.Errorf("no pending operations or not all signatures are saved")
	})
	// await some time to be sure that the tx wasn't submitted
	select {
	case <-ctx.Done():
		require.NoError(t, ctx.Err())
	case <-time.After(5 * time.Second):
	}
	pendingOperations, err := runnerEnv.ContractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	require.Len(t, pendingOperations[0].Signatures, int(runnerEnvCfg.SigningThreshold))

	// check that XRPL balance is still zero
	xrplRecipientBalance := runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency,
	)
	require.Equal(t, "0", xrplRecipientBalance.Value.String())

	// update XRPL base fee to let the signers sign and send to tx with new fee
	require.NoError(t, runnerEnv.BridgeClient.UpdateXRPLBaseFee(ctx, runnerEnv.ContractOwner, xrpl.DefaultXRPLBaseFee))
	pendingOperations, err = runnerEnv.ContractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	// version is incremented
	require.Equal(t, uint32(2), pendingOperations[0].Version)

	runnerEnv.AwaitNoPendingOperations(ctx, t)
	xrplRecipientBalance = runnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, runnerEnv.BridgeXRPLAddress, xrplCurrency,
	)
	require.Equal(t, "1", xrplRecipientBalance.Value.String())
}
