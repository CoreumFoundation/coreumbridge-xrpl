//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v3/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestRegisterXRPLOriginatedTokensSendFromXRPLToCoreumAndBack(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses(ctx, t)
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumSender := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})
	t.Logf("Coreum sender: %s", coreumSender.String())
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient: %s", xrplRecipientAddress.String())

	registeredXRPLCurrency, err := rippledata.NewCurrency("RCP")
	require.NoError(t, err)

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)
	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)
	registeredXRPLToken := runnerEnv.RegisterXRPLOriginatedToken(ctx, t, xrplIssuerAddress, registeredXRPLCurrency, int32(6), integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30))

	valueToSendFromXRPLtoCoreum, err := rippledata.NewValue("1e10", false)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueToSendFromXRPLtoCoreum,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}
	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumSender)
	require.NoError(t, err)

	runnerEnv.SendXRPLPaymentTx(ctx, t, xrplIssuerAddress, runnerEnv.bridgeXRPLAddress, amountToSendFromXRPLtoCoreum, memo)
	runnerEnv.AwaitCoreumBalance(ctx, t, chains.Coreum, coreumSender, sdk.NewCoin(registeredXRPLToken.CoreumDenom, integrationtests.ConvertStringWithDecimalsToSDKInt(t, valueToSendFromXRPLtoCoreum.String(), XRPLTokenDecimals)))

	// send the full amount in 4 transactions to XRPL
	amountToSend := integrationtests.ConvertStringWithDecimalsToSDKInt(t, valueToSendFromXRPLtoCoreum.String(), XRPLTokenDecimals).QuoRaw(4)

	// send 2 transactions without the trust set to be reverted
	// TODO(dzmitryhil) update assertion once we add the final tx revert/recovery
	_, err = runnerEnv.ContractClient.SendToXRPL(ctx, coreumSender, xrplRecipientAddress.String(), sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSend))
	require.NoError(t, err)
	_, err = runnerEnv.ContractClient.SendToXRPL(ctx, coreumSender, xrplRecipientAddress.String(), sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSend))
	require.NoError(t, err)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	// send TrustSet to be able to receive coins
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency)

	_, err = runnerEnv.ContractClient.SendToXRPL(ctx, coreumSender, xrplRecipientAddress.String(), sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSend))
	require.NoError(t, err)
	_, err = runnerEnv.ContractClient.SendToXRPL(ctx, coreumSender, xrplRecipientAddress.String(), sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSend))
	require.NoError(t, err)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	balance := runnerEnv.Chains.XRPL.GetAccountBalance(ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency)
	require.Equal(t, "5000000000", balance.Value.String())
}

func TestSendFromXRPLToCoreumWithMaliciousRelayer(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.MaliciousRelayerNumber = 1
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses(ctx, t)
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumSender := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})
	t.Logf("Coreum sender: %s", coreumSender.String())
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient: %s", xrplRecipientAddress.String())

	currencyHexSymbol := hex.EncodeToString([]byte(strings.Repeat("X", 20)))
	registeredXRPLCurrency, err := rippledata.NewCurrency(currencyHexSymbol)
	require.NoError(t, err)

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)
	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)
	registeredXRPLToken := runnerEnv.RegisterXRPLOriginatedToken(ctx, t, xrplIssuerAddress, registeredXRPLCurrency, int32(6), integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30))

	valueToSendFromXRPLtoCoreum, err := rippledata.NewValue("1e10", false)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueToSendFromXRPLtoCoreum,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}
	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumSender)
	require.NoError(t, err)

	runnerEnv.SendXRPLPaymentTx(ctx, t, xrplIssuerAddress, runnerEnv.bridgeXRPLAddress, amountToSendFromXRPLtoCoreum, memo)
	runnerEnv.AwaitCoreumBalance(ctx, t, chains.Coreum, coreumSender, sdk.NewCoin(registeredXRPLToken.CoreumDenom, integrationtests.ConvertStringWithDecimalsToSDKInt(t, valueToSendFromXRPLtoCoreum.String(), XRPLTokenDecimals)))

	// send TrustSet to be able to receive coins
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency)

	amountToSend := integrationtests.ConvertStringWithDecimalsToSDKInt(t, valueToSendFromXRPLtoCoreum.String(), XRPLTokenDecimals).QuoRaw(4)
	_, err = runnerEnv.ContractClient.SendToXRPL(ctx, coreumSender, xrplRecipientAddress.String(), sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSend))
	require.NoError(t, err)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	balance := runnerEnv.Chains.XRPL.GetAccountBalance(ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency)
	require.Equal(t, "2500000000", balance.Value.String())
}

func TestSendFromXRPLToCoreumWithTicketsReallocation(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.UsedTicketSequenceThreshold = 3
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses(ctx, t)
	runnerEnv.AllocateTickets(ctx, t, uint32(5))
	sendingCount := 10

	coreumSender := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntFromUint64(1_000_000),
	})
	t.Logf("Coreum sender: %s", coreumSender.String())
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("XRPL recipient: %s", xrplRecipientAddress.String())

	currencyHexSymbol := hex.EncodeToString([]byte(strings.Repeat("X", 20)))
	registeredXRPLCurrency, err := rippledata.NewCurrency(currencyHexSymbol)
	require.NoError(t, err)

	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)
	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)
	registeredXRPLToken := runnerEnv.RegisterXRPLOriginatedToken(ctx, t, xrplIssuerAddress, registeredXRPLCurrency, int32(6), integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30))

	valueToSendFromXRPLtoCoreum, err := rippledata.NewValue("1e10", false)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueToSendFromXRPLtoCoreum,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}
	memo, err := xrpl.EncodeCoreumRecipientToMemo(coreumSender)
	require.NoError(t, err)

	runnerEnv.SendXRPLPaymentTx(ctx, t, xrplIssuerAddress, runnerEnv.bridgeXRPLAddress, amountToSendFromXRPLtoCoreum, memo)
	runnerEnv.AwaitCoreumBalance(ctx, t, chains.Coreum, coreumSender, sdk.NewCoin(registeredXRPLToken.CoreumDenom, integrationtests.ConvertStringWithDecimalsToSDKInt(t, valueToSendFromXRPLtoCoreum.String(), XRPLTokenDecimals)))

	// send TrustSet to be able to receive coins
	runnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency)

	totalSent := sdkmath.ZeroInt()
	amountToSend := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "10", XRPLTokenDecimals)
	for i := 0; i < sendingCount; i++ {
		for {
			_, err = runnerEnv.ContractClient.SendToXRPL(ctx, coreumSender, xrplRecipientAddress.String(), sdk.NewCoin(registeredXRPLToken.CoreumDenom, amountToSend))
			if err == nil {
				break
			}
			if coreum.IsLastTicketReservedError(err) || coreum.IsNoAvailableTicketsError(err) {
				t.Logf("No tickets left, waiting for new tickets...")
				<-time.After(500 * time.Millisecond)
				continue
			}
			require.NoError(t, err)
		}
		totalSent = totalSent.Add(amountToSend)
	}
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	balance := runnerEnv.Chains.XRPL.GetAccountBalance(ctx, t, xrplRecipientAddress, xrplIssuerAddress, registeredXRPLCurrency)
	require.Equal(t, totalSent.Quo(sdkmath.NewIntWithDecimal(1, XRPLTokenDecimals)).String(), balance.Value.String())
}
