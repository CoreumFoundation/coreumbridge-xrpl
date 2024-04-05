//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	"github.com/CoreumFoundation/coreum-tools/pkg/retry"
	coreumapp "github.com/CoreumFoundation/coreum/v4/app"
	"github.com/CoreumFoundation/coreum/v4/pkg/client"
	coreumconfig "github.com/CoreumFoundation/coreum/v4/pkg/config"
	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	assetfttypes "github.com/CoreumFoundation/coreum/v4/x/asset/ft/types"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

// RunnerEnvConfig is runner environment config.
type RunnerEnvConfig struct {
	AwaitTimeout                time.Duration
	SigningThreshold            uint32
	RelayersCount               uint32
	MaliciousRelayerNumber      uint32
	UsedTicketSequenceThreshold uint32
	XRPLBaseFee                 uint32
	TrustSetLimitAmount         sdkmath.Int
	CustomBridgeXRPLAddress     *rippledata.Account
	CustomContractAddress       *sdk.AccAddress
	CustomContractOwner         *sdk.AccAddress
	// if custom error handler returns false, the runner env fails with the input error
	CustomErrorHandler func(err error) bool
}

// DefaultRunnerEnvConfig returns default runner environment config.
func DefaultRunnerEnvConfig() RunnerEnvConfig {
	defBootstrappingCfg := bridgeclient.DefaultBootstrappingConfig()
	defaultTrustSetLimitAmount, ok := sdkmath.NewIntFromString(defBootstrappingCfg.TrustSetLimitAmount)
	if !ok {
		panic(errors.Errorf("failed to convert string to sdkmath.Int, string:%s", defBootstrappingCfg.TrustSetLimitAmount))
	}
	return RunnerEnvConfig{
		AwaitTimeout:                time.Minute,
		SigningThreshold:            2,
		RelayersCount:               3,
		MaliciousRelayerNumber:      0,
		UsedTicketSequenceThreshold: defBootstrappingCfg.UsedTicketSequenceThreshold,
		XRPLBaseFee:                 defBootstrappingCfg.XRPLBaseFee,
		TrustSetLimitAmount:         defaultTrustSetLimitAmount,
		CustomBridgeXRPLAddress:     nil,
		CustomContractAddress:       nil,
		CustomContractOwner:         nil,
		CustomErrorHandler:          nil,
	}
}

// RunnerEnv is runner environment used for the integration tests.
type RunnerEnv struct {
	Cfg                  RunnerEnvConfig
	BridgeXRPLAddress    rippledata.Account
	BootstrappingConfig  bridgeclient.BootstrappingConfig
	ContractClient       *coreum.ContractClient
	Chains               integrationtests.Chains
	ContractOwner        sdk.AccAddress
	BridgeClient         *bridgeclient.BridgeClient
	RunnersParallelGroup *parallel.Group
	Runners              []*runner.Runner
	RunnerComponents     []runner.Components
}

// NewRunnerEnv returns new instance of the RunnerEnv.
func NewRunnerEnv(ctx context.Context, t *testing.T, cfg RunnerEnvConfig, chains integrationtests.Chains) *RunnerEnv {
	ctx, cancel := context.WithCancel(ctx)
	relayerCoreumAddresses := genCoreumRelayers(
		ctx,
		t,
		chains.Coreum,
		cfg.RelayersCount,
	)
	bridgeXRPLAddress, relayerXRPLAddresses, relayerXRPLPubKeys := genBridgeXRPLAccountWithRelayers(
		ctx,
		t,
		chains.XRPL,
		cfg.RelayersCount,
	)
	if cfg.CustomBridgeXRPLAddress != nil {
		bridgeXRPLAddress = *cfg.CustomBridgeXRPLAddress
	}

	var contractOwner sdk.AccAddress
	if cfg.CustomContractOwner == nil {
		contractOwner = chains.Coreum.GenAccount()
	} else {
		contractOwner = *cfg.CustomContractOwner
	}

	// fund to cover the fees
	chains.Coreum.FundAccountWithOptions(ctx, t, contractOwner, coreumintegration.BalancesOptions{
		Amount: chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount.AddRaw(10_000_000),
	})

	contractClient := coreum.NewContractClient(
		coreum.DefaultContractClientConfig(sdk.AccAddress(nil)),
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

	bootstrappingRelayers := make([]bridgeclient.RelayerConfig, 0)
	for i := 0; i < int(cfg.RelayersCount); i++ {
		relayerCoreumAddress := relayerCoreumAddresses[i]
		relayerXRPLAddress := relayerXRPLAddresses[i]
		relayerXRPLPubKey := relayerXRPLPubKeys[i]
		bootstrappingRelayers = append(bootstrappingRelayers, bridgeclient.RelayerConfig{
			CoreumAddress: relayerCoreumAddress.String(),
			XRPLAddress:   relayerXRPLAddress.String(),
			XRPLPubKey:    relayerXRPLPubKey.String(),
		})
	}

	bootstrappingCfg := bridgeclient.BootstrappingConfig{
		Owner:                       contractOwner.String(),
		Admin:                       contractOwner.String(),
		Relayers:                    bootstrappingRelayers,
		EvidenceThreshold:           cfg.SigningThreshold,
		UsedTicketSequenceThreshold: cfg.UsedTicketSequenceThreshold,
		TrustSetLimitAmount:         cfg.TrustSetLimitAmount.String(),
		ContractByteCodePath:        integrationtests.ContractFilePathV110,
		XRPLBaseFee:                 cfg.XRPLBaseFee,
		SkipXRPLBalanceValidation:   true,
	}

	if cfg.CustomContractAddress == nil {
		contractAddress, err := bridgeClient.Bootstrap(
			ctx, contractOwner, bridgeXRPLAddress.String(), bootstrappingCfg,
		)
		require.NoError(t, err)
		require.NoError(t, contractClient.SetContractAddress(contractAddress))
		_, codeID, err := bridgeClient.DeployContract(ctx, contractOwner, integrationtests.CompiledContractFilePath)
		require.NoError(t, err)

		_, err = contractClient.MigrateContract(ctx, contractOwner, codeID)
		require.NoError(t, err)
	} else {
		require.NoError(t, contractClient.SetContractAddress(*cfg.CustomContractAddress))
	}

	runners := make([]*runner.Runner, 0, cfg.RelayersCount)
	runnerComponents := make([]runner.Components, 0, cfg.RelayersCount)
	// add correct relayers
	for i := 0; i < int(cfg.RelayersCount-cfg.MaliciousRelayerNumber); i++ {
		rnrComponents, rnr := createDevRunner(
			ctx,
			t,
			chains,
			relayerXRPLAddresses[i],
			contractClient.GetContractAddress(),
			relayerCoreumAddresses[i],
		)
		runners = append(runners, rnr)
		runnerComponents = append(runnerComponents, rnrComponents)
	}
	// add malicious relayers
	// we keep the relayer indexes to make all config valid apart from the XRPL signing
	for i := cfg.RelayersCount - cfg.MaliciousRelayerNumber; i < cfg.RelayersCount; i++ {
		maliciousXRPLAddress := chains.XRPL.GenAccount(ctx, t, 0)
		rnrComponents, rnr := createDevRunner(
			ctx,
			t,
			chains,
			maliciousXRPLAddress,
			contractClient.GetContractAddress(),
			relayerCoreumAddresses[i],
		)
		runners = append(runners, rnr)
		runnerComponents = append(runnerComponents, rnrComponents)
	}

	runnerEnv := &RunnerEnv{
		Cfg:                  cfg,
		BridgeXRPLAddress:    bridgeXRPLAddress,
		BootstrappingConfig:  bootstrappingCfg,
		ContractClient:       contractClient,
		Chains:               chains,
		ContractOwner:        contractOwner,
		BridgeClient:         bridgeClient,
		RunnersParallelGroup: parallel.NewGroup(ctx),
		Runners:              runners,
		RunnerComponents:     runnerComponents,
	}
	t.Cleanup(func() {
		// we can cancel the context now and wait for the runner to stop gracefully
		cancel()
		err := runnerEnv.RunnersParallelGroup.Wait()
		if err == nil || errors.Is(err, context.Canceled) {
			return
		}
		// the client replies with that error if the context is canceled at the time of the request,
		// and the error is in the internal package, so we can't check the type
		if strings.Contains(err.Error(), "context canceled") {
			return
		}
		if cfg.CustomErrorHandler != nil {
			if !cfg.CustomErrorHandler(err) {
				require.NoError(
					t,
					err,
					"Found unexpected runner process errors after the execution from custom handler",
				)
			}
			return
		}

		require.NoError(t, err, "Found unexpected runner process errors after the execution")
	})

	return runnerEnv
}

// StartAllRunnerPeriodicMetricCollectors starts all relayer periodic metrics collector.
func (r *RunnerEnv) StartAllRunnerPeriodicMetricCollectors() {
	for i := range r.RunnerComponents {
		components := r.RunnerComponents[i]
		r.RunnersParallelGroup.Spawn(
			fmt.Sprintf("runner-periodic-metric-collector-%d", i),
			parallel.Exit,
			components.MetricsPeriodicCollector.Start,
		)
	}
}

// StartAllRunnerProcesses starts all relayer processes.
func (r *RunnerEnv) StartAllRunnerProcesses() {
	for i := range r.Runners {
		relayerRunner := r.Runners[i]
		r.RunnersParallelGroup.Spawn(fmt.Sprintf("runner-%d", i), parallel.Exit, relayerRunner.Start)
	}
}

// AwaitNoPendingOperations waits for no pending contract transactions.
func (r *RunnerEnv) AwaitNoPendingOperations(ctx context.Context, t *testing.T) {
	t.Helper()

	r.AwaitState(ctx, t, func(t *testing.T) error {
		operations, err := r.BridgeClient.GetPendingOperations(ctx)
		require.NoError(t, err)
		if len(operations) != 0 {
			return errors.Errorf("there are still pending operatrions: %+v", operations)
		}
		return nil
	})
}

// AwaitCoreumBalance waits for expected coreum balance.
func (r *RunnerEnv) AwaitCoreumBalance(
	ctx context.Context,
	t *testing.T,
	address sdk.AccAddress,
	expectedBalance sdk.Coin,
) {
	t.Helper()
	awaitContext, awaitContextCancel := context.WithTimeout(ctx, r.Cfg.AwaitTimeout)
	t.Cleanup(awaitContextCancel)
	require.NoError(t, r.Chains.Coreum.AwaitForBalance(awaitContext, t, address, expectedBalance))
}

// AwaitState waits for stateChecker function to rerun nil and retires in case of failure.
func (r *RunnerEnv) AwaitState(ctx context.Context, t *testing.T, stateChecker func(t *testing.T) error) {
	t.Helper()
	retryCtx, retryCancel := context.WithTimeout(ctx, r.Cfg.AwaitTimeout)
	defer retryCancel()
	err := retry.Do(retryCtx, 500*time.Millisecond, func() error {
		if err := stateChecker(t); err != nil {
			return retry.Retryable(err)
		}

		return nil
	})
	require.NoError(t, err)
}

// AllocateTickets allocate initial tickets amount.
func (r *RunnerEnv) AllocateTickets(
	ctx context.Context,
	t *testing.T,
	numberOfTicketsToAllocate uint32,
) {
	r.Chains.XRPL.FundAccountForTicketAllocation(ctx, t, r.BridgeXRPLAddress, numberOfTicketsToAllocate)
	require.NoError(t, r.BridgeClient.RecoverTickets(ctx, r.ContractOwner, &numberOfTicketsToAllocate))

	r.AwaitNoPendingOperations(ctx, t)
	availableTickets, err := r.ContractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	require.Len(t, availableTickets, int(numberOfTicketsToAllocate))
}

// RegisterXRPLOriginatedToken registers XRPL currency and awaits for the trust set ot be set.
func (r *RunnerEnv) RegisterXRPLOriginatedToken(
	ctx context.Context,
	t *testing.T,
	issuer rippledata.Account,
	currency rippledata.Currency,
	sendingPrecision int32,
	maxHoldingAmount sdkmath.Int,
	bridgingFee sdkmath.Int,
) coreum.XRPLToken {
	r.Chains.Coreum.FundAccountWithOptions(ctx, t, r.ContractOwner, coreumintegration.BalancesOptions{
		Amount: r.Chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount,
	})
	_, err := r.BridgeClient.RegisterXRPLToken(
		ctx,
		r.ContractOwner,
		issuer,
		currency,
		sendingPrecision,
		maxHoldingAmount,
		bridgingFee,
	)
	require.NoError(t, err)
	// await for the trust set
	r.AwaitNoPendingOperations(ctx, t)
	registeredXRPLToken, err := r.ContractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, issuer.String(), xrpl.ConvertCurrencyToString(currency),
	)
	require.NoError(t, err)
	require.Equal(t, coreum.TokenStateEnabled, registeredXRPLToken.State)

	return registeredXRPLToken
}

// RegisterCoreumOriginatedToken registers Coreum token and reruns stored token.
func (r *RunnerEnv) RegisterCoreumOriginatedToken(
	ctx context.Context,
	t *testing.T,
	denom string,
	decimals uint32,
	sendingPrecision int32,
	maxHoldingAmount sdkmath.Int,
	bridgingFee sdkmath.Int,
) coreum.CoreumToken {
	token, err := r.BridgeClient.RegisterCoreumToken(
		ctx, r.ContractOwner, denom, decimals, sendingPrecision, maxHoldingAmount, bridgingFee,
	)
	require.NoError(t, err)
	return token
}

// IssueAndRegisterCoreumOriginatedToken issues new Coreum originated token and registers it in the contract.
func (r *RunnerEnv) IssueAndRegisterCoreumOriginatedToken(
	ctx context.Context,
	t *testing.T,
	issuerAddress sdk.AccAddress,
	tokenDecimals uint32,
	initialAmount sdkmath.Int,
	sendingPrecision int32,
	maxHoldingAmount sdkmath.Int,
	bridgingFee sdkmath.Int,
) coreum.CoreumToken {
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        issuerAddress.String(),
		Symbol:        "symbol" + uuid.NewString()[:4],
		Subunit:       "subunit" + uuid.NewString()[:4],
		Precision:     tokenDecimals, // token decimals in terms of the contract
		InitialAmount: initialAmount,
	}
	_, err := client.BroadcastTx(
		ctx,
		r.Chains.Coreum.ClientContext.WithFromAddress(issuerAddress),
		r.Chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)
	registeredCoreumOriginatedToken := r.RegisterCoreumOriginatedToken(
		ctx,
		t,
		// use Coreum denom
		assetfttypes.BuildDenom(issueMsg.Subunit, issuerAddress),
		tokenDecimals,
		sendingPrecision,
		maxHoldingAmount,
		bridgingFee,
	)

	return registeredCoreumOriginatedToken
}

// SendFromCoreumToXRPL sends tokens form Coreum to XRPL.
func (r *RunnerEnv) SendFromCoreumToXRPL(
	ctx context.Context,
	t *testing.T,
	sender sdk.AccAddress,
	recipient rippledata.Account,
	amount sdk.Coin,
	deliverAmount *sdkmath.Int,
) {
	require.NoError(t, r.BridgeClient.SendFromCoreumToXRPL(ctx, sender, recipient, amount, deliverAmount))
}

// SendFromXRPLToCoreum sends tokens form XRPL to Coreum.
func (r *RunnerEnv) SendFromXRPLToCoreum(
	ctx context.Context,
	t *testing.T,
	senderKeyName string,
	amount rippledata.Amount,
	recipient sdk.AccAddress,
) {
	_, err := r.BridgeClient.SendFromXRPLToCoreum(ctx, senderKeyName, amount, recipient)
	require.NoError(t, err)
}

// SendXRPLPaymentTx sends Payment transaction.
func (r *RunnerEnv) SendXRPLPaymentTx(
	ctx context.Context,
	t *testing.T,
	senderAcc, recipientAcc rippledata.Account,
	amount rippledata.Amount,
	memo rippledata.Memo,
) {
	xrpPaymentTx := rippledata.Payment{
		Destination: recipientAcc,
		Amount:      amount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
			Memos: rippledata.Memos{
				memo,
			},
		},
	}
	require.NoError(t, r.Chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &xrpPaymentTx, senderAcc))
}

// SendXRPLPartialPaymentTx sends Payment transaction with partial payment.
func (r *RunnerEnv) SendXRPLPartialPaymentTx(
	ctx context.Context,
	t *testing.T,
	senderAcc, recipientAcc rippledata.Account,
	amount rippledata.Amount,
	maxAmount rippledata.Amount,
	memo rippledata.Memo,
) {
	xrpPaymentTx := rippledata.Payment{
		Destination: recipientAcc,
		Amount:      amount,
		SendMax:     &maxAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
			Memos: rippledata.Memos{
				memo,
			},
			Flags: lo.ToPtr(rippledata.TxPartialPayment),
		},
	}
	require.NoError(t, r.Chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &xrpPaymentTx, senderAcc))
}

func (r *RunnerEnv) EnableXRPLAccountRippling(ctx context.Context, t *testing.T, account rippledata.Account) {
	// enable rippling on this account's trust lines.
	accountSetTx := rippledata.AccountSet{
		SetFlag: lo.ToPtr(uint32(rippledata.TxDefaultRipple)),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.ACCOUNT_SET,
		},
	}
	require.NoError(t, r.Chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &accountSetTx, account))
}

func (r *RunnerEnv) SendXRPLMaxTrustSetTx(
	ctx context.Context,
	t *testing.T,
	account rippledata.Account,
	issuer rippledata.Account,
	currency rippledata.Currency,
) {
	value, err := rippledata.NewValue("9999999999999999e80", false)
	require.NoError(t, err)
	require.NoError(t, r.BridgeClient.SetXRPLTrustSet(ctx, account.String(), rippledata.Amount{
		Value:    value,
		Currency: currency,
		Issuer:   issuer,
	}))
}

// UpdateCoreumToken updates Coreum token.
func (r *RunnerEnv) UpdateCoreumToken(
	ctx context.Context,
	t *testing.T,
	sender sdk.AccAddress,
	denom string,
	state *coreum.TokenState,
	sendingPrecision *int32,
	maxHoldingAmount *sdkmath.Int,
	bridgingFee *sdkmath.Int,
) {
	require.NoError(
		t,
		r.BridgeClient.UpdateCoreumToken(
			ctx,
			sender,
			denom,
			state,
			sendingPrecision,
			maxHoldingAmount,
			bridgingFee,
		))
}

// UpdateCoreumToken updates XRPL token.
func (r *RunnerEnv) UpdateXRPLToken(
	ctx context.Context,
	t *testing.T,
	sender sdk.AccAddress,
	issuer, currency string,
	state *coreum.TokenState,
	sendingPrecision *int32,
	maxHoldingAmount *sdkmath.Int,
	bridgingFee *sdkmath.Int,
) {
	require.NoError(
		t,
		r.BridgeClient.UpdateXRPLToken(
			ctx,
			sender,
			issuer,
			currency,
			state,
			sendingPrecision,
			maxHoldingAmount,
			bridgingFee,
		))
}

func genCoreumRelayers(
	ctx context.Context,
	t *testing.T,
	coreumChain integrationtests.CoreumChain,
	relayersCount uint32,
) []sdk.AccAddress {
	t.Helper()

	addresses := make([]sdk.AccAddress, 0, relayersCount)
	for i := 0; i < int(relayersCount); i++ {
		relayerAddress := coreumChain.GenAccount()
		coreumChain.FundAccountWithOptions(ctx, t, relayerAddress, coreumintegration.BalancesOptions{
			Amount: sdkmath.NewIntFromUint64(1_000_000),
		})
		addresses = append(addresses, relayerAddress)
	}

	return addresses
}

func genBridgeXRPLAccountWithRelayers(
	ctx context.Context,
	t *testing.T,
	xrplChain integrationtests.XRPLChain,
	signersCount uint32,
) (rippledata.Account, []rippledata.Account, []rippledata.PublicKey) {
	t.Helper()
	// some fee to cover simple txs all extras must be allocated in the test
	bridgeXRPLAddress := xrplChain.GenAccount(ctx, t, 0.5)

	t.Logf("Bridge account is generated, address:%s", bridgeXRPLAddress.String())
	signerAccounts := make([]rippledata.Account, 0, signersCount)
	signerPubKeys := make([]rippledata.PublicKey, 0, signersCount)
	for i := 0; i < int(signersCount); i++ {
		signerAcc := xrplChain.GenAccount(ctx, t, 0)
		signerAccounts = append(signerAccounts, signerAcc)
		t.Logf("Signer %d is generated, address:%s", i+1, signerAcc.String())
		signerPubKeys = append(signerPubKeys, xrplChain.GetSignerPubKey(t, signerAcc))
	}
	// fund for the signers SignerListSet
	xrplChain.FundAccountForSignerListSet(ctx, t, bridgeXRPLAddress)
	return bridgeXRPLAddress, signerAccounts, signerPubKeys
}

func createDevRunner(
	ctx context.Context,
	t *testing.T,
	chains integrationtests.Chains,
	xrplRelayerAcc rippledata.Account,
	contractAddress sdk.AccAddress,
	relayerCoreumAddress sdk.AccAddress,
) (runner.Components, *runner.Runner) {
	t.Helper()

	encodingConfig := coreumconfig.NewEncodingConfig(coreumapp.ModuleBasics)
	xrplKeyring := keyring.NewInMemory(encodingConfig.Codec)
	coreumKeyring := keyring.NewInMemory(encodingConfig.Codec)

	relayerRunnerCfg := runner.DefaultConfig()
	relayerRunnerCfg.LoggingConfig.Level = "info"

	// reimport coreum key
	coreumKr := chains.Coreum.ClientContext.Keyring()
	keyInfo, err := coreumKr.KeyByAddress(relayerCoreumAddress)
	require.NoError(t, err)
	pass := uuid.NewString()
	armor, err := coreumKr.ExportPrivKeyArmor(keyInfo.Name, pass)
	require.NoError(t, err)
	require.NoError(t, coreumKeyring.ImportPrivKey(relayerRunnerCfg.Coreum.RelayerKeyName, armor, pass))

	// reimport XRPL key
	xrplKr := chains.XRPL.GetSignerKeyring()
	keyInfo, err = xrplKr.Key(xrplRelayerAcc.String())
	require.NoError(t, err)
	armor, err = xrplKr.ExportPrivKeyArmor(keyInfo.Name, pass)
	require.NoError(t, err)
	require.NoError(t, xrplKeyring.ImportPrivKey(relayerRunnerCfg.XRPL.MultiSignerKeyName, armor, pass))

	relayerRunnerCfg.XRPL.RPC.URL = chains.XRPL.Config().RPCAddress
	// make the scanner fast
	relayerRunnerCfg.XRPL.Scanner.RetryDelay = 500 * time.Millisecond

	relayerRunnerCfg.Coreum.GRPC.URL = chains.Coreum.Config().GRPCAddress
	relayerRunnerCfg.Coreum.Contract.ContractAddress = contractAddress.String()
	relayerRunnerCfg.Coreum.Network.ChainID = chains.Coreum.ChainSettings.ChainID
	// make operation fetcher fast
	relayerRunnerCfg.Processes.CoreumToXRPLProcess.RepeatDelay = 500 * time.Millisecond

	// exit on errors
	relayerRunnerCfg.Processes.ExitOnError = true

	// make the collector faster
	relayerRunnerCfg.Metrics.PeriodicCollector.RepeatDelay = 500 * time.Millisecond

	// re-init log to use correct `CallerSkip`
	log, err := logger.NewZapLogger(logger.DefaultZapLoggerConfig())
	require.NoError(t, err)

	xrplSDKClientCtx := chains.Coreum.ClientContext.WithKeyring(xrplKeyring).SDKContext()
	coreumSDKClientCtx := chains.Coreum.ClientContext.WithKeyring(coreumKeyring).SDKContext()
	components, err := runner.NewComponents(relayerRunnerCfg, xrplSDKClientCtx, coreumSDKClientCtx, log)
	require.NoError(t, err)

	relayerRunner, err := runner.NewRunner(ctx, components, relayerRunnerCfg)
	require.NoError(t, err)
	return components, relayerRunner
}
