//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	prometheusdto "github.com/prometheus/client_model/go"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v5/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/buildinfo"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/metrics"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

const (
	//nolint:lll // the signature sample doesn't require to be split
	xrplTxSignature = "304502210097099E9AB2C41DA3F672004924B3557D58D101A5745C57C6336C5CF36B59E8F5022003984E50483C921E3FDF45BC7DE4E6ED9D340F0E0CAA6BB1828C647C6665A1CC"
)

func TestXRPLChainBaseFeeMetric(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.RelayersCount = 1
	envCfg.SigningThreshold = 1
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerPeriodicMetricCollectors()

	time.Sleep(3 * time.Second)

	awaitGaugeMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.XRPLChainBaseFeeGauge,
		10,
	)
}

func TestContractConfigXRPLBaseFeeMetric(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.RelayersCount = 1
	envCfg.SigningThreshold = 1
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerPeriodicMetricCollectors()

	awaitGaugeMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.ContractConfigXRPLBaseFeeGauge,
		float64(envCfg.XRPLBaseFee),
	)

	newXRPLBaseFee := envCfg.XRPLBaseFee + envCfg.XRPLBaseFee
	require.NoError(t, runnerEnv.BridgeClient.UpdateXRPLBaseFee(ctx, runnerEnv.ContractOwner, newXRPLBaseFee))
	awaitGaugeMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.ContractConfigXRPLBaseFeeGauge,
		float64(newXRPLBaseFee),
	)
}

func TestXRPLBridgeAccountBalancesMetric(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.RelayersCount = 1
	envCfg.SigningThreshold = 1
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerPeriodicMetricCollectors()
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumRecipient := chains.Coreum.GenAccount()
	xrplIssuerAddress := chains.XRPL.GenAccount(ctx, t, 1)
	// enable to be able to send to any address
	runnerEnv.EnableXRPLAccountRippling(ctx, t, xrplIssuerAddress)

	// register token and send to Coreum to have it on XRPL bridge account
	registeredXRPLCurrency := integrationtests.GenerateXRPLCurrency(t)
	registeredXRPLToken := runnerEnv.RegisterXRPLOriginatedToken(
		ctx,
		t,
		xrplIssuerAddress,
		registeredXRPLCurrency,
		int32(2),
		integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30),
		sdkmath.ZeroInt(),
	)

	valueSentToCoreum, err := rippledata.NewValue("10", false)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueSentToCoreum,
		Currency: registeredXRPLCurrency,
		Issuer:   xrplIssuerAddress,
	}

	runnerEnv.SendFromXRPLToCoreum(ctx, t, xrplIssuerAddress.String(), amountToSendFromXRPLtoCoreum, coreumRecipient)
	require.NoError(t, err)
	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumRecipient,
		sdk.NewCoin(
			registeredXRPLToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				valueSentToCoreum.String(),
				xrpl.XRPLIssuedTokenDecimals,
			),
		),
	)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	balances := runnerEnv.Chains.XRPL.GetAccountBalances(ctx, t, runnerEnv.BridgeXRPLAddress)

	// check XRPL balances metric for the XRPL originated token

	xrpKey := fmt.Sprintf("%s/%s", xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency), xrpl.XRPTokenIssuer.String())
	xrpBalance, ok := balances[xrpKey]
	require.True(t, ok)
	require.NotZero(t, xrpBalance.Float())

	awaitGaugeVecMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.XRPLBridgeAccountBalancesGaugeVec,
		map[string]string{
			metrics.XRPLCurrencyIssuerLabel: xrpKey,
		},
		truncateFloatByMetricCollectorTruncationPrecision(xrpBalance.Float()),
	)

	registeredTokenKey := fmt.Sprintf("%s/%s", registeredXRPLToken.Currency, registeredXRPLToken.Issuer)
	registeredTokenBalance, ok := balances[registeredTokenKey]
	require.True(t, ok)
	require.NotZero(t, registeredTokenBalance.Float())

	awaitGaugeVecMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.XRPLBridgeAccountBalancesGaugeVec,
		map[string]string{
			metrics.XRPLCurrencyIssuerLabel: registeredTokenKey,
		},
		truncateFloatByMetricCollectorTruncationPrecision(registeredTokenBalance.Float()),
	)
}

func TestContractBalancesMetric(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.RelayersCount = 1
	envCfg.SigningThreshold = 1
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerPeriodicMetricCollectors()
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 6)),
	})

	xrplSenderAddressAddress := chains.XRPL.GenAccount(ctx, t, 10)

	// issue asset ft and register it
	sendingPrecision := int32(5)
	tokenDecimals := uint32(5)
	initialAmount := sdkmath.NewIntWithDecimal(1, 16)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 16)
	registeredCoreumOriginatedToken := runnerEnv.IssueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		coreumSenderAddress,
		tokenDecimals,
		initialAmount,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)

	amountToSendToXRPL := sdkmath.NewInt(1234567890)
	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSenderAddress,
		xrplSenderAddressAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
		nil,
	)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	registeredTokenKey := fmt.Sprintf("%s/%s", registeredCoreumOriginatedToken.XRPLCurrency, runnerEnv.BridgeXRPLAddress)

	expectedMetricValue := float64(amountToSendToXRPL.Int64()) / math.Pow(10, float64(tokenDecimals))
	awaitGaugeVecMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.ContractBalancesGaugeVec,
		map[string]string{
			metrics.XRPLCurrencyIssuerLabel: registeredTokenKey,
			metrics.CoreumDenomLabel:        registeredCoreumOriginatedToken.Denom,
		},
		truncateFloatByMetricCollectorTruncationPrecision(expectedMetricValue),
	)

	// send XRP token form XRPL to Coreum
	valueToSendFromXRPLToCoreum, err := rippledata.NewValue("1.0", true)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueToSendFromXRPLToCoreum,
		Currency: xrpl.XRPTokenCurrency,
		Issuer:   xrpl.XRPTokenIssuer,
	}
	runnerEnv.SendFromXRPLToCoreum(
		ctx,
		t,
		xrplSenderAddressAddress.String(),
		amountToSendFromXRPLtoCoreum,
		coreumSenderAddress,
	)

	registeredXRPToken, err := runnerEnv.ContractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrpl.XRPTokenIssuer.String(), xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
	)
	require.NoError(t, err)

	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumSenderAddress,
		sdk.NewCoin(
			registeredXRPToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				valueToSendFromXRPLToCoreum.String(),
				xrpl.XRPCurrencyDecimals,
			),
		),
	)

	// send back to account which doesn't exist to lock the amount on the contract
	runnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSenderAddress,
		xrpl.GenPrivKeyTxSigner().Account(),
		sdk.NewCoin(registeredXRPToken.CoreumDenom, sdkmath.NewIntWithDecimal(1, xrpl.XRPCurrencyDecimals)),
		nil,
	)
	runnerEnv.AwaitNoPendingOperations(ctx, t)

	xrpTokenKey := fmt.Sprintf(
		"%s/%s", registeredXRPToken.Currency, registeredXRPToken.Issuer,
	)
	awaitGaugeVecMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.ContractBalancesGaugeVec,
		map[string]string{
			metrics.XRPLCurrencyIssuerLabel: xrpTokenKey,
			metrics.CoreumDenomLabel:        registeredXRPToken.CoreumDenom,
		},
		truncateFloatByMetricCollectorTruncationPrecision(valueToSendFromXRPLToCoreum.Float()),
	)
}

func TestPendingOperationsMetric(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.RelayersCount = 2
	envCfg.SigningThreshold = 1
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	runnerEnv.StartAllRunnerPeriodicMetricCollectors()
	// create tickets allocation operation
	ticketsToAllocate := uint32(200)
	runnerEnv.Chains.XRPL.FundAccountForTicketAllocation(ctx, t, runnerEnv.BridgeXRPLAddress, ticketsToAllocate)
	require.NoError(t, runnerEnv.BridgeClient.RecoverTickets(ctx, runnerEnv.ContractOwner, &ticketsToAllocate))
	// check that the operation is in the queue
	pendingOperations, err := runnerEnv.ContractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)

	pendingOperation := pendingOperations[0]
	// save the signature for the operation
	relayerCoreumAddress, err := sdk.AccAddressFromBech32(runnerEnv.BootstrappingConfig.Relayers[0].CoreumAddress)
	require.NoError(t, err)
	_, err = runnerEnv.ContractClient.SaveSignature(
		ctx,
		relayerCoreumAddress,
		pendingOperation.GetOperationSequence(),
		pendingOperation.Version,
		xrplTxSignature,
	)
	require.NoError(t, err)

	awaitGaugeVecMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.PendingOperationsGaugeVec,
		map[string]string{
			metrics.OperationSequenceLabel: strconv.Itoa(int(pendingOperation.GetOperationSequence())),
		},
		1,
	)

	// start all processes to let the relayers complete the operation
	runnerEnv.StartAllRunnerProcesses()

	// check that value is remove
	awaitGaugeVecMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.PendingOperationsGaugeVec,
		map[string]string{
			metrics.OperationSequenceLabel: strconv.Itoa(int(pendingOperation.GetOperationSequence())),
		},
		0,
	)
}

func TestTransactionEvidencesMetric(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.RelayersCount = 2
	envCfg.SigningThreshold = 2
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)

	runnerEnv.StartAllRunnerPeriodicMetricCollectors()

	xrplToCoreumTransferEvidence1 := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    integrationtests.GenXRPLTxHash(t),
		Issuer:    xrpl.XRPTokenIssuer.String(),
		Currency:  xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
		Amount:    sdkmath.NewInt(10),
		Recipient: coreum.GenAccount(),
	}

	xrplToCoreumTransferEvidence2 := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    integrationtests.GenXRPLTxHash(t),
		Issuer:    xrpl.XRPTokenIssuer.String(),
		Currency:  xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
		Amount:    sdkmath.NewInt(10),
		Recipient: coreum.GenAccount(),
	}

	relayer1Address, err := sdk.AccAddressFromBech32(runnerEnv.BootstrappingConfig.Relayers[0].CoreumAddress)
	require.NoError(t, err)
	relayer1ContractClient := runnerEnv.RunnerComponents[0].CoreumContractClient

	relayer2Address, err := sdk.AccAddressFromBech32(runnerEnv.BootstrappingConfig.Relayers[1].CoreumAddress)
	require.NoError(t, err)
	relayer2ContractClient := runnerEnv.RunnerComponents[1].CoreumContractClient

	// save evidences manually from relayer 1
	_, err = relayer1ContractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayer1Address,
		xrplToCoreumTransferEvidence1,
	)
	require.NoError(t, err)

	require.NoError(t, err)
	_, err = relayer1ContractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayer1Address,
		xrplToCoreumTransferEvidence2,
	)
	require.NoError(t, err)

	transactionEvidencesBefore, err := relayer1ContractClient.GetTransactionEvidences(ctx)
	require.NoError(t, err)
	require.Len(t, transactionEvidencesBefore, 2)

	for _, evidence := range transactionEvidencesBefore {
		awaitGaugeVecMetricState(
			ctx,
			t,
			runnerEnv,
			runnerEnv.RunnerComponents[0].MetricsRegistry.TransactionEvidencesGaugeVec,
			map[string]string{
				metrics.EvidenceHashLabel: evidence.Hash,
			},
			float64(len(evidence.RelayerAddresses)),
		)
	}

	// save one evidences manually from relayer 2
	_, err = relayer2ContractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayer2Address,
		xrplToCoreumTransferEvidence1,
	)
	require.NoError(t, err)

	transactionEvidencesAfter, err := relayer1ContractClient.GetTransactionEvidences(ctx)
	require.NoError(t, err)
	require.Len(t, transactionEvidencesAfter, 1)

	// check that one evidence metric remains the same
	awaitGaugeVecMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.TransactionEvidencesGaugeVec,
		map[string]string{
			metrics.EvidenceHashLabel: transactionEvidencesAfter[0].Hash,
		},
		float64(len(transactionEvidencesAfter[0].RelayerAddresses)),
	)

	confirmedEvidences := findConfirmedEvidences(transactionEvidencesBefore, transactionEvidencesAfter)
	require.Len(t, confirmedEvidences, 1)

	// check that the evidence metric is removed
	awaitGaugeVecMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.TransactionEvidencesGaugeVec,
		map[string]string{
			metrics.EvidenceHashLabel: confirmedEvidences[0].Hash,
		},
		0,
	)
}

func TestRelayerBalancesMetric(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.RelayersCount = 2
	envCfg.SigningThreshold = 2
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerPeriodicMetricCollectors()

	relayer1Address := runnerEnv.BootstrappingConfig.Relayers[0].CoreumAddress
	relayer2Address := runnerEnv.BootstrappingConfig.Relayers[1].CoreumAddress

	relayer1BalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: relayer1Address,
		Denom:   chains.Coreum.ChainSettings.Denom,
	})
	require.NoError(t, err)

	relayer2BalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: relayer2Address,
		Denom:   chains.Coreum.ChainSettings.Denom,
	})
	require.NoError(t, err)

	awaitGaugeVecMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.RelayerBalancesGaugeVec,
		map[string]string{
			metrics.RelayerCoremAddressLabel: relayer1Address,
		},
		truncateFloatByMetricCollectorTruncationPrecision(
			truncateAmountWithDecimals(coreum.TokenDecimals, relayer1BalanceRes.Balance.Amount),
		),
	)

	awaitGaugeVecMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.RelayerBalancesGaugeVec,
		map[string]string{
			metrics.RelayerCoremAddressLabel: relayer2Address,
		},
		truncateFloatByMetricCollectorTruncationPrecision(
			truncateAmountWithDecimals(coreum.TokenDecimals, relayer2BalanceRes.Balance.Amount),
		),
	)
}

func TestXRPLAccountLedgerMetrics(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.RelayersCount = 1
	envCfg.SigningThreshold = 1
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	currentHistoricalScanXRPLedger := getGaugeValue(
		t, runnerEnv.RunnerComponents[0].MetricsRegistry.XRPLAccountFullHistoryScanLedgerIndexGauge,
	)
	require.NotZero(t, currentHistoricalScanXRPLedger)

	currentRecentScanXRPLedger := getGaugeValue(
		t, runnerEnv.RunnerComponents[0].MetricsRegistry.XRPLAccountRecentHistoryScanLedgerIndexGauge,
	)
	require.NotZero(t, currentRecentScanXRPLedger)
}

func TestFreeTicketsMetric(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.RelayersCount = 1
	envCfg.SigningThreshold = 1
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerPeriodicMetricCollectors()
	runnerEnv.StartAllRunnerProcesses()

	ticketsToAllocate := 200
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	awaitGaugeMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.FreeContractTicketsGauge,
		float64(ticketsToAllocate),
	)

	awaitGaugeMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.FreeXRPLTicketsGauge,
		float64(ticketsToAllocate),
	)
}

func TestBridgeStateMetric(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.RelayersCount = 1
	envCfg.SigningThreshold = 1
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerPeriodicMetricCollectors()
	runnerEnv.StartAllRunnerProcesses()

	// await active
	awaitGaugeMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.BridgeStateGauge,
		float64(1),
	)

	require.NoError(t, runnerEnv.BridgeClient.HaltBridge(ctx, runnerEnv.ContractOwner))

	// await halted
	awaitGaugeMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.BridgeStateGauge,
		float64(0),
	)
}

func TestMaliciousBehaviourMetric(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.RelayersCount = 2
	envCfg.SigningThreshold = 1
	envCfg.UsedTicketSequenceThreshold = 2
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerPeriodicMetricCollectors()
	require.NoError(t, runnerEnv.BridgeClient.RecoverTickets(ctx, runnerEnv.ContractOwner, lo.ToPtr(uint32(3))))

	// send invalid signature from the relayer
	pendingOperations, err := runnerEnv.ContractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)

	pendingOperation := pendingOperations[0]
	// save invalid signature for the operation
	maliciousCoreumAddress, err := sdk.AccAddressFromBech32(runnerEnv.BootstrappingConfig.Relayers[0].CoreumAddress)
	require.NoError(t, err)
	_, err = runnerEnv.ContractClient.SaveSignature(
		ctx,
		maliciousCoreumAddress,
		pendingOperation.GetOperationSequence(),
		pendingOperation.Version,
		xrplTxSignature,
	)
	require.NoError(t, err)

	// start relayers now
	runnerEnv.StartAllRunnerProcesses()

	awaitGaugeVecMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.MaliciousBehaviourGaugeVec,
		map[string]string{
			metrics.MaliciousBehaviourKeyLabel: fmt.Sprintf(
				"invalid_signature_for_operation_%d_relayer_%s",
				pendingOperation.GetOperationSequence(), maliciousCoreumAddress.String(),
			),
		},
		1,
	)

	// sign and send unexpected tx from a relayer
	regularKey, err := rippledata.NewRegularKeyFromAddress(runnerEnv.BootstrappingConfig.Relayers[0].XRPLAddress)
	require.NoError(t, err)
	unexpectedXRPLTx := rippledata.SetRegularKey{
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SET_REGULAR_KEY,
		},
		RegularKey: regularKey,
	}
	txHash := multiSignAndSubmitBrdigeTxFromFirstRelayer(ctx, t, runnerEnv, &unexpectedXRPLTx)

	awaitGaugeVecMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.MaliciousBehaviourGaugeVec,
		map[string]string{
			metrics.MaliciousBehaviourKeyLabel: fmt.Sprintf(
				"%s%s", metrics.UnexpectedXRPLTxTypeTxHash, txHash.String(),
			),
		},
		1,
	)

	xrpAmount, err := rippledata.NewAmount("0.001")
	require.NoError(t, err)
	xrplRecipient, err := rippledata.NewAccountFromAddress(runnerEnv.BootstrappingConfig.Relayers[0].XRPLAddress)
	require.NoError(t, err)
	xrpPaymentTxWithoutOperation := rippledata.Payment{
		Destination: *xrplRecipient,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	txHash = multiSignAndSubmitBrdigeTxFromFirstRelayer(ctx, t, runnerEnv, &xrpPaymentTxWithoutOperation)

	awaitGaugeVecMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.MaliciousBehaviourGaugeVec,
		map[string]string{
			metrics.MaliciousBehaviourKeyLabel: fmt.Sprintf(
				"%s%s", metrics.PotentialMaliciousXRPLBehaviourTxHashPrefix, txHash.String(),
			),
		},
		1,
	)
}

func TestRelayerActivityMetrics(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.RelayersCount = 2
	envCfg.SigningThreshold = 2
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.StartAllRunnerPeriodicMetricCollectors()
	// 1 signature, 1 evidence
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	xrplSenderAddressAddress := chains.XRPL.GenAccount(ctx, t, 10)

	valueToSendFromXRPLToCoreum, err := rippledata.NewValue("1.0", true)
	require.NoError(t, err)

	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueToSendFromXRPLToCoreum,
		Currency: xrpl.XRPTokenCurrency,
		Issuer:   xrpl.XRPTokenIssuer,
	}
	// 1 evidence
	runnerEnv.SendFromXRPLToCoreum(
		ctx,
		t,
		xrplSenderAddressAddress.String(),
		amountToSendFromXRPLtoCoreum,
		coreumSenderAddress,
	)

	for _, relayer := range runnerEnv.BootstrappingConfig.Relayers {
		awaitGaugeVecMetricState(
			ctx,
			t,
			runnerEnv,
			runnerEnv.RunnerComponents[0].MetricsRegistry.RelayerActivityGaugeVec,
			map[string]string{
				metrics.RelayerCoremAddressLabel: relayer.CoreumAddress,
				metrics.ActionLabel:              "save_evidence",
			},
			2,
		)
		awaitGaugeVecMetricState(
			ctx,
			t,
			runnerEnv,
			runnerEnv.RunnerComponents[0].MetricsRegistry.RelayerActivityGaugeVec,
			map[string]string{
				metrics.RelayerCoremAddressLabel: relayer.CoreumAddress,
				metrics.ActionLabel:              "save_signature",
			},
			1,
		)
	}
}

func TestRelayerVersionMetrics(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.RelayersCount = 2
	envCfg.SigningThreshold = 2
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.StartAllRunnerPeriodicMetricCollectors()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	xrplSenderAddressAddress := chains.XRPL.GenAccount(ctx, t, 10)

	valueToSendFromXRPLToCoreum, err := rippledata.NewValue("1.0", true)
	require.NoError(t, err)

	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueToSendFromXRPLToCoreum,
		Currency: xrpl.XRPTokenCurrency,
		Issuer:   xrpl.XRPTokenIssuer,
	}
	// save evidence to generate the tx with memo
	runnerEnv.SendFromXRPLToCoreum(
		ctx,
		t,
		xrplSenderAddressAddress.String(),
		amountToSendFromXRPLtoCoreum,
		coreumSenderAddress,
	)

	for _, relayer := range runnerEnv.BootstrappingConfig.Relayers {
		awaitGaugeVecMetricState(
			ctx,
			t,
			runnerEnv,
			runnerEnv.RunnerComponents[0].MetricsRegistry.RelayerVersion,
			map[string]string{
				metrics.RelayerCoremAddressLabel: relayer.CoreumAddress,
				metrics.VersionLabel:             buildinfo.VersionTag,
			},
			1,
		)
	}
}

func TestXRPLTokensCoreumSupplyMetric(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.RelayersCount = 1
	envCfg.SigningThreshold = 1
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerPeriodicMetricCollectors()
	runnerEnv.StartAllRunnerProcesses()
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 6)),
	})

	xrplSenderAddressAddress := chains.XRPL.GenAccount(ctx, t, 10)

	// send XRP token form XRPL to Coreum
	valueToSendFromXRPLToCoreum, err := rippledata.NewValue("1.0", true)
	require.NoError(t, err)
	amountToSendFromXRPLtoCoreum := rippledata.Amount{
		Value:    valueToSendFromXRPLToCoreum,
		Currency: xrpl.XRPTokenCurrency,
		Issuer:   xrpl.XRPTokenIssuer,
	}
	runnerEnv.SendFromXRPLToCoreum(
		ctx,
		t,
		xrplSenderAddressAddress.String(),
		amountToSendFromXRPLtoCoreum,
		coreumSenderAddress,
	)

	registeredXRPToken, err := runnerEnv.ContractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrpl.XRPTokenIssuer.String(), xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
	)
	require.NoError(t, err)

	runnerEnv.AwaitCoreumBalance(
		ctx,
		t,
		coreumSenderAddress,
		sdk.NewCoin(
			registeredXRPToken.CoreumDenom,
			integrationtests.ConvertStringWithDecimalsToSDKInt(
				t,
				valueToSendFromXRPLToCoreum.String(),
				xrpl.XRPCurrencyDecimals,
			),
		),
	)
	xrpTokenKey := fmt.Sprintf(
		"%s/%s", registeredXRPToken.Currency, registeredXRPToken.Issuer,
	)
	awaitGaugeVecMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.XRPLTokensCoreumSupplyGaugeVec,
		map[string]string{
			metrics.XRPLCurrencyIssuerLabel: xrpTokenKey,
			metrics.CoreumDenomLabel:        registeredXRPToken.CoreumDenom,
		},
		truncateFloatByMetricCollectorTruncationPrecision(valueToSendFromXRPLToCoreum.Float()),
	)
}

func TestXRPLBridgeAccountReservesGaugeMetric(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	envCfg := DefaultRunnerEnvConfig()
	envCfg.RelayersCount = 1
	envCfg.SigningThreshold = 1
	runnerEnv := NewRunnerEnv(ctx, t, envCfg, chains)
	runnerEnv.StartAllRunnerPeriodicMetricCollectors()
	runnerEnv.StartAllRunnerProcesses()

	// await halted
	awaitGaugeMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.XRPLBridgeAccountReservesGauge,
		14,
	)
	runnerEnv.AllocateTickets(ctx, t, uint32(200))

	// tickets are allocated so the reserve is increased
	awaitGaugeMetricState(
		ctx,
		t,
		runnerEnv,
		runnerEnv.RunnerComponents[0].MetricsRegistry.XRPLBridgeAccountReservesGauge,
		414,
	)
}

func awaitGaugeMetricState(
	ctx context.Context,
	t *testing.T,
	runnerEnv *RunnerEnv,
	m prometheus.Gauge,
	expectedValue float64,
) {
	runnerEnv.AwaitState(ctx, t, func(t *testing.T) error {
		return assertGaugeMetric(t, m, expectedValue)
	})
}

func awaitGaugeVecMetricState(
	ctx context.Context,
	t *testing.T,
	runnerEnv *RunnerEnv,
	m *prometheus.GaugeVec,
	labels prometheus.Labels,
	expectedValue float64,
) {
	runnerEnv.AwaitState(ctx, t, func(t *testing.T) error {
		mtr, err := m.GetMetricWith(labels)
		require.NoError(t, err)
		return assertGaugeMetric(t, mtr, expectedValue)
	})
}

func assertGaugeMetric(t *testing.T, m prometheus.Metric, expectedValue float64) error {
	if got := getGaugeValue(t, m); expectedValue != got {
		return errors.Errorf(
			"expected metric value is different from the current, expected:%f, got:%f", expectedValue, got,
		)
	}
	return nil
}

func getGaugeValue(t *testing.T, m prometheus.Metric) float64 {
	metricDTO := prometheusdto.Metric{}
	require.NoError(t, m.Write(&metricDTO))
	require.NotNil(t, metricDTO.GetGauge())
	return metricDTO.GetGauge().GetValue()
}

func truncateAmountWithDecimals(decimals uint32, amount sdkmath.Int) float64 {
	tenPowerDec := big.NewInt(0).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	balanceRat := big.NewRat(0, 1).SetFrac(amount.BigInt(), tenPowerDec)
	// float64 should cover the range with enough precision
	floatValue, _ := balanceRat.Float64()
	return floatValue
}

func truncateFloatByMetricCollectorTruncationPrecision(val float64) float64 {
	ratio := math.Pow(10, float64(metrics.DefaultPeriodicCollectorConfig().FloatTruncationPrecision))
	return math.Trunc(val*ratio) / ratio
}

func findConfirmedEvidences(before, after []coreum.TransactionEvidence) []coreum.TransactionEvidence {
	afterMap := lo.SliceToMap(after, func(evidence coreum.TransactionEvidence) (string, struct{}) {
		return evidence.Hash, struct{}{}
	})
	return lo.Filter(before, func(evidence coreum.TransactionEvidence, index int) bool {
		_, found := afterMap[evidence.Hash]
		return !found
	})
}

type multiSignableTransaction interface {
	rippledata.Transaction
	rippledata.MultiSignable
}

func multiSignAndSubmitBrdigeTxFromFirstRelayer(
	ctx context.Context,
	t *testing.T,
	runnerEnv *RunnerEnv,
	tx multiSignableTransaction,
) *rippledata.Hash256 {
	txToSign := tx
	runnerEnv.Chains.XRPL.AutoFillTx(ctx, t, txToSign, runnerEnv.BridgeXRPLAddress)
	txToSign.GetBase().SigningPubKey = &rippledata.PublicKey{}

	relayerRunnerCfg := runner.DefaultConfig()
	signer, err := runnerEnv.RunnerComponents[0].XRPLKeyringTxSigner.MultiSign(
		txToSign, relayerRunnerCfg.XRPL.MultiSignerKeyName,
	)
	require.NoError(t, err)
	txToSign.GetBase().SigningPubKey = &rippledata.PublicKey{}
	txToSign.GetBase().TxnSignature = nil

	require.NoError(t, rippledata.SetSigners(txToSign, signer))
	require.NoError(t, runnerEnv.Chains.XRPL.RPCClient().SubmitAndAwaitSuccess(ctx, txToSign))
	return txToSign.GetHash()
}
