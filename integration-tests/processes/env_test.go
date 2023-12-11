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
	coreumapp "github.com/CoreumFoundation/coreum/v3/app"
	coreumconfig "github.com/CoreumFoundation/coreum/v3/pkg/config"
	coreumintegration "github.com/CoreumFoundation/coreum/v3/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

// RunnerEnvConfig is runner environment config.
type RunnerEnvConfig struct {
	AwaitTimeout                time.Duration
	SigningThreshold            int
	RelayersCount               int
	MaliciousRelayerNumber      int
	UsedTicketSequenceThreshold int
	TrustSetLimitAmount         sdkmath.Int
}

// DefaultRunnerEnvConfig returns default runner environment config.
func DefaultRunnerEnvConfig() RunnerEnvConfig {
	return RunnerEnvConfig{
		AwaitTimeout:                15 * time.Second,
		SigningThreshold:            2,
		RelayersCount:               3,
		MaliciousRelayerNumber:      0,
		UsedTicketSequenceThreshold: 150,
		TrustSetLimitAmount:         sdkmath.NewIntWithDecimal(1, 35),
	}
}

// RunnerEnv is runner environment used for the integration tests.
type RunnerEnv struct {
	Cfg                  RunnerEnvConfig
	bridgeXRPLAddress    rippledata.Account
	ContractClient       *coreum.ContractClient
	Chains               integrationtests.Chains
	ContractOwner        sdk.AccAddress
	BridgeClient         *bridgeclient.BridgeClient
	RunnersParallelGroup *parallel.Group
	Runners              []*runner.Runner
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

	contractOwner := chains.Coreum.GenAccount()
	// fund to cover the fees
	chains.Coreum.FundAccountWithOptions(ctx, t, contractOwner, coreumintegration.BalancesOptions{
		Amount: chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount.AddRaw(1_000_000),
	})

	contractClient := coreum.NewContractClient(coreum.DefaultContractClientConfig(sdk.AccAddress(nil)), chains.Log, chains.Coreum.ClientContext)
	xrplTxSigner := xrpl.NewKeyringTxSigner(chains.XRPL.GetSignerKeyring())
	bridgeClient := bridgeclient.NewBridgeClient(
		chains.Log,
		chains.Coreum.ClientContext,
		contractClient,
		chains.XRPL.RPCClient(),
		xrplTxSigner,
	)

	bootstrappingRelayers := make([]bridgeclient.RelayerBootstrappingConfig, 0)
	for i := 0; i < cfg.RelayersCount; i++ {
		relayerCoreumAddress := relayerCoreumAddresses[i]
		relayerXRPLAddress := relayerXRPLAddresses[i]
		relayerXRPLPubKey := relayerXRPLPubKeys[i]
		bootstrappingRelayers = append(bootstrappingRelayers, bridgeclient.RelayerBootstrappingConfig{
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
		ContractByteCodePath:        integrationtests.CompiledContractFilePath,
		SkipXRPLBalanceValidation:   true,
	}

	contractAddress, err := bridgeClient.Bootstrap(ctx, contractOwner, bridgeXRPLAddress.String(), bootstrappingCfg)
	require.NoError(t, err)
	require.NoError(t, contractClient.SetContractAddress(contractAddress))

	runners := make([]*runner.Runner, 0, cfg.RelayersCount)
	// add correct relayers
	for i := 0; i < cfg.RelayersCount-cfg.MaliciousRelayerNumber; i++ {
		runners = append(
			runners,
			createDevRunner(
				ctx,
				t,
				chains,
				relayerXRPLAddresses[i],
				contractClient.GetContractAddress(),
				relayerCoreumAddresses[i],
			),
		)
	}
	// add malicious relayers
	// we keep the relayer indexes to make all config valid apart from the XRPL signing
	for i := cfg.RelayersCount - cfg.MaliciousRelayerNumber; i < cfg.RelayersCount; i++ {
		maliciousXRPLAddress := chains.XRPL.GenAccount(ctx, t, 0)
		runners = append(
			runners,
			createDevRunner(
				ctx,
				t,
				chains,
				maliciousXRPLAddress,
				contractClient.GetContractAddress(),
				relayerCoreumAddresses[i],
			),
		)
	}

	runnerEnv := &RunnerEnv{
		Cfg:                  cfg,
		bridgeXRPLAddress:    bridgeXRPLAddress,
		ContractClient:       contractClient,
		Chains:               chains,
		ContractOwner:        contractOwner,
		BridgeClient:         bridgeClient,
		RunnersParallelGroup: parallel.NewGroup(ctx),
		Runners:              runners,
	}
	t.Cleanup(func() {
		// we can cancel the context now and wait for the runner to stop gracefully
		cancel()
		err := runnerEnv.RunnersParallelGroup.Wait()
		if err == nil || errors.Is(err, context.Canceled) {
			return
		}
		// the client replies with that error in if the context is canceled at the time of the request,
		// and the error is in the internal package, so we can't check the type
		if strings.Contains(err.Error(), "context canceled") {
			return
		}

		require.NoError(t, err, "Found unexpected runner process errors after the execution")
	})

	return runnerEnv
}

// StartAllRunnerProcesses starts all relayer processes.
func (r *RunnerEnv) StartAllRunnerProcesses() {
	for i := range r.Runners {
		relayerRunner := r.Runners[i]
		r.RunnersParallelGroup.Spawn(fmt.Sprintf("runner-%d", i), parallel.Exit, func(ctx context.Context) error {
			// disable restart on error to handler unexpected errors
			xrplTxObserverProcess := relayerRunner.Processes.XRPLTxObserver
			xrplTxObserverProcess.IsRestartableOnError = false
			xrplTxSubmitterProcess := relayerRunner.Processes.XRPLTxSubmitter
			xrplTxSubmitterProcess.IsRestartableOnError = false
			return relayerRunner.Processor.StartProcesses(ctx, xrplTxObserverProcess, xrplTxSubmitterProcess)
		})
	}
}

// AwaitNoPendingOperations waits for no pendoing contract transactions.
func (r *RunnerEnv) AwaitNoPendingOperations(ctx context.Context, t *testing.T) {
	t.Helper()

	r.AwaitState(ctx, t, func(t *testing.T) error {
		operations, err := r.ContractClient.GetPendingOperations(ctx)
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
	coreumChain integrationtests.CoreumChain,
	address sdk.AccAddress,
	expectedBalance sdk.Coin,
) {
	t.Helper()
	awaitContext, awaitContextCancel := context.WithTimeout(ctx, r.Cfg.AwaitTimeout)
	t.Cleanup(awaitContextCancel)
	require.NoError(t, coreumChain.AwaitForBalance(awaitContext, t, address, expectedBalance))
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
	bridgeXRPLAccountInfo, err := r.Chains.XRPL.RPCClient().AccountInfo(ctx, r.bridgeXRPLAddress)
	require.NoError(t, err)

	r.Chains.XRPL.FundAccountForTicketAllocation(ctx, t, r.bridgeXRPLAddress, numberOfTicketsToAllocate)
	_, err = r.ContractClient.RecoverTickets(
		ctx, r.ContractOwner, *bridgeXRPLAccountInfo.AccountData.Sequence, &numberOfTicketsToAllocate,
	)
	require.NoError(t, err)

	require.NoError(t, err)
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
) coreum.XRPLToken {
	r.Chains.Coreum.FundAccountWithOptions(ctx, t, r.ContractOwner, coreumintegration.BalancesOptions{
		Amount: r.Chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee.Amount,
	})
	_, err := r.ContractClient.RegisterXRPLToken(
		ctx,
		r.ContractOwner,
		issuer.String(),
		xrpl.ConvertCurrencyToString(currency),
		sendingPrecision,
		maxHoldingAmount,
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
	value, err := rippledata.NewValue("1e80", false)
	require.NoError(t, err)
	trustSetTx := rippledata.TrustSet{
		LimitAmount: rippledata.Amount{
			Value:    value,
			Currency: currency,
			Issuer:   issuer,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TRUST_SET,
		},
	}
	require.NoError(t, r.Chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &trustSetTx, account))
}

func genCoreumRelayers(
	ctx context.Context,
	t *testing.T,
	coreumChain integrationtests.CoreumChain,
	relayersCount int,
) []sdk.AccAddress {
	t.Helper()

	addresses := make([]sdk.AccAddress, 0, relayersCount)
	for i := 0; i < relayersCount; i++ {
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
	signersCount int,
) (rippledata.Account, []rippledata.Account, []rippledata.PublicKey) {
	t.Helper()
	// some fee to cover simple txs all extras must be allocated in the test
	bridgeXRPLAddress := xrplChain.GenAccount(ctx, t, 0.5)

	t.Logf("Bridge account is generated, address:%s", bridgeXRPLAddress.String())
	signerAccounts := make([]rippledata.Account, 0, signersCount)
	signerPubKeys := make([]rippledata.PublicKey, 0, signersCount)
	for i := 0; i < signersCount; i++ {
		signerAcc := xrplChain.GenAccount(ctx, t, 0)
		signerAccounts = append(signerAccounts, signerAcc)
		t.Logf("Signer %d is generated, address:%s", i+1, signerAcc.String())
		signerPubKeys = append(signerPubKeys, xrplChain.GetSignerPubKey(t, signerAcc))
	}
	// fund for the signers SignerListSet
	xrplChain.FundAccountForSignerListSet(ctx, t, bridgeXRPLAddress, signersCount)
	return bridgeXRPLAddress, signerAccounts, signerPubKeys
}

func createDevRunner(
	ctx context.Context,
	t *testing.T,
	chains integrationtests.Chains,
	xrplRelayerAcc rippledata.Account,
	contractAddress sdk.AccAddress,
	relayerCoreumAddress sdk.AccAddress,
) *runner.Runner {
	t.Helper()

	encodingConfig := coreumconfig.NewEncodingConfig(coreumapp.ModuleBasics)
	kr := keyring.NewInMemory(encodingConfig.Codec)

	relayerRunnerCfg := runner.DefaultConfig()
	relayerRunnerCfg.LoggingConfig.Level = "info"

	// reimport coreum key
	coreumKr := chains.Coreum.ClientContext.Keyring()
	keyInfo, err := coreumKr.KeyByAddress(relayerCoreumAddress)
	require.NoError(t, err)
	pass := uuid.NewString()
	armor, err := coreumKr.ExportPrivKeyArmor(keyInfo.Name, pass)
	require.NoError(t, err)
	require.NoError(t, kr.ImportPrivKey(relayerRunnerCfg.Coreum.RelayerKeyName, armor, pass))

	// reimport XRPL key
	xrplKr := chains.XRPL.GetSignerKeyring()
	keyInfo, err = xrplKr.Key(xrplRelayerAcc.String())
	require.NoError(t, err)
	armor, err = xrplKr.ExportPrivKeyArmor(keyInfo.Name, pass)
	require.NoError(t, err)
	require.NoError(t, kr.ImportPrivKey(relayerRunnerCfg.XRPL.MultiSignerKeyName, armor, pass))

	relayerRunnerCfg.XRPL.RPC.URL = chains.XRPL.Config().RPCAddress
	// make the scanner fast
	relayerRunnerCfg.XRPL.Scanner.RetryDelay = 500 * time.Millisecond

	relayerRunnerCfg.Coreum.GRPC.URL = chains.Coreum.Config().GRPCAddress
	relayerRunnerCfg.Coreum.Contract.ContractAddress = contractAddress.String()
	// We use high gas adjustment since our relayers might send transactions in one block.
	// They estimate gas based on the same state, but since transactions are executed one by one the next transaction uses
	// the state different from the one it used for the estimation as a result the out-of-gas error might appear.
	relayerRunnerCfg.Coreum.Contract.GasAdjustment = 2
	relayerRunnerCfg.Coreum.Network.ChainID = chains.Coreum.ChainSettings.ChainID
	// make operation fetcher fast
	relayerRunnerCfg.Processes.XRPLTxSubmitter.RepeatDelay = 500 * time.Millisecond

	relayerRunner, err := runner.NewRunner(ctx, relayerRunnerCfg, kr)
	require.NoError(t, err)
	return relayerRunner
}
