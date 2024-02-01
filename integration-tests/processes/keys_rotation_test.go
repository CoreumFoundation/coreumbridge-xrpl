//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"context"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestKeysRotation(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	initialRunnerEnvCfg := DefaultRunnerEnvConfig()
	initialRunnerEnvCfg.SigningThreshold = 2
	initialRunnerEnvCfg.RelayersCount = 2
	// expect the UnauthorizedSender since after the rotation one sender will become unauthorized
	initialRunnerEnvCfg.CustomErrorHandler = coreum.IsUnauthorizedSenderError

	initialRunnerEnv := NewRunnerEnv(ctx, t, initialRunnerEnvCfg, chains)
	initialRunnerEnv.StartAllRunnerProcesses()
	initialRunnerEnv.AllocateTickets(ctx, t, 200)

	// register a token and transfer from Coreum to XRPL
	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewInt(1_000_000)),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	// issue asset ft and register it
	initialAmount := sdkmath.NewIntWithDecimal(1, 30)
	sendingPrecision := int32(6)
	tokenDecimals := uint32(6)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 30)
	bridgingFee := sdkmath.NewInt(40)
	registeredCoreumOriginatedToken := initialRunnerEnv.IssueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		coreumSenderAddress,
		tokenDecimals,
		initialAmount,
		sendingPrecision,
		maxHoldingAmount,
		bridgingFee,
	)

	// send TrustSet to be able to receive coins from the bridge
	xrplCurrency, err := rippledata.NewCurrency(registeredCoreumOriginatedToken.XRPLCurrency)
	require.NoError(t, err)
	initialRunnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, initialRunnerEnv.BridgeXRPLAddress, xrplCurrency)

	amountToSendToXRPL := sdkmath.NewInt(1000040)
	initialRunnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSenderAddress,
		xrplRecipientAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
		nil,
	)

	initialRunnerEnv.AwaitNoPendingOperations(ctx, t)

	balance := initialRunnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, initialRunnerEnv.BridgeXRPLAddress, xrplCurrency,
	)
	require.Equal(t, "1", balance.Value.String())

	newRunnerEnvCfg := DefaultRunnerEnvConfig()
	newRunnerEnvCfg.RelayersCount = 3 // gen 3 new relayers
	newRunnerEnvCfg.CustomBridgeXRPLAddress = &initialRunnerEnv.BridgeXRPLAddress
	newRunnerEnvCfg.CustomContractAddress = lo.ToPtr(initialRunnerEnv.ContractClient.GetContractAddress())
	newRunnerEnvCfg.CustomContractOwner = &initialRunnerEnv.ContractOwner

	newRunnerEnv := NewRunnerEnv(ctx, t, newRunnerEnvCfg, chains)

	newSigningThreshold := uint32(3)
	// take 4 relayers (1 from prev and 3 from new set)
	updatedRelayers := []bridgeclient.RelayerConfig{
		initialRunnerEnv.BootstrappingConfig.Relayers[0],
		newRunnerEnv.BootstrappingConfig.Relayers[0],
		newRunnerEnv.BootstrappingConfig.Relayers[1],
		newRunnerEnv.BootstrappingConfig.Relayers[2],
	}

	contractCfgBeforeRotationAcceptance, err := initialRunnerEnv.ContractClient.GetContractConfig(ctx)
	require.NoError(t, err)

	require.NoError(t, initialRunnerEnv.BridgeClient.RotateKeys(
		ctx,
		initialRunnerEnv.ContractOwner,
		bridgeclient.KeysRotationConfig{
			Relayers:          updatedRelayers,
			EvidenceThreshold: newSigningThreshold,
		},
	))

	initialRunnerEnv.AwaitNoPendingOperations(ctx, t)

	assertSignersAreUpdated(ctx, t, initialRunnerEnv, updatedRelayers, newSigningThreshold)

	contractCfgAfterRotationAcceptance, err := initialRunnerEnv.ContractClient.GetContractConfig(ctx)
	require.NoError(t, err)

	expectedContractCfgAfterRotationAcceptance := contractCfgBeforeRotationAcceptance
	expectedContractCfgAfterRotationAcceptance.EvidenceThreshold = newSigningThreshold
	expectedContractCfgAfterRotationAcceptance.Relayers = convertBridgeClientRelayersToContactRelayers(t, updatedRelayers)
	expectedContractCfgAfterRotationAcceptance.BridgeState = string(coreum.BridgeStateHalted)
	require.Equal(t, expectedContractCfgAfterRotationAcceptance, contractCfgAfterRotationAcceptance)

	// activate the bridge to let it work with the new relays
	require.NoError(t, initialRunnerEnv.BridgeClient.ResumeBridge(ctx, initialRunnerEnv.ContractOwner))
	// start all new relayers together with some initial
	newRunnerEnv.StartAllRunnerProcesses()

	initialRunnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSenderAddress,
		xrplRecipientAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
		nil,
	)

	initialRunnerEnv.AwaitNoPendingOperations(ctx, t)

	balance = initialRunnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, initialRunnerEnv.BridgeXRPLAddress, xrplCurrency,
	)
	require.Equal(t, "2", balance.Value.String())

	// check the fee collected by old and new relayers
	relayerToFee := map[string]sdkmath.Int{
		// fees for the first and second tx (20 + 10)
		initialRunnerEnv.BootstrappingConfig.Relayers[0].CoreumAddress: sdkmath.NewInt(30),
		// fees for the first tx (20)
		initialRunnerEnv.BootstrappingConfig.Relayers[1].CoreumAddress: sdkmath.NewInt(20),
		// fees for the second tx (10)
		newRunnerEnv.BootstrappingConfig.Relayers[0].CoreumAddress: sdkmath.NewInt(10),
		// fees for the second tx (10)
		newRunnerEnv.BootstrappingConfig.Relayers[1].CoreumAddress: sdkmath.NewInt(10),
	}

	for relayer, expectedFee := range relayerToFee {
		relayerAddress, err := sdk.AccAddressFromBech32(relayer)
		require.NoError(t, err)
		fees, err := initialRunnerEnv.BridgeClient.GetFeesCollected(ctx, relayerAddress)
		require.NoError(t, err)
		require.Equal(t, expectedFee.String(), fees.AmountOf(registeredCoreumOriginatedToken.Denom).String())
	}

	// claim the fee from the relayer which is not active now in order not to produce account seq mismatch error
	relayerBalanceBefore, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: initialRunnerEnv.BootstrappingConfig.Relayers[1].CoreumAddress,
		Denom:   registeredCoreumOriginatedToken.Denom,
	})
	require.NoError(t, err)

	relayerAddress, err := sdk.AccAddressFromBech32(initialRunnerEnv.BootstrappingConfig.Relayers[1].CoreumAddress)
	require.NoError(t, err)
	amountToClaim := sdk.NewCoin(registeredCoreumOriginatedToken.Denom, relayerToFee[relayerAddress.String()])
	require.NoError(t, initialRunnerEnv.BridgeClient.ClaimFees(
		ctx, relayerAddress, sdk.NewCoins(amountToClaim)),
	)

	relayerBalanceAfter, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: initialRunnerEnv.BootstrappingConfig.Relayers[1].CoreumAddress,
		Denom:   registeredCoreumOriginatedToken.Denom,
	})
	require.NoError(t, err)
	require.False(t, relayerBalanceAfter.Balance.IsZero())
	require.Equal(
		t,
		relayerBalanceAfter.Balance.Amount.Sub(amountToClaim.Amount).String(),
		relayerBalanceBefore.Balance.Amount.String(),
	)
}

func TestKeysRotationWithMaxSignerCount(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewInt(1_000_000),
	})
	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	initialRunnerEnvCfg := DefaultRunnerEnvConfig()
	// expect the UnauthorizedSender since after the rotation senders will become unauthorized
	initialRunnerEnvCfg.CustomErrorHandler = coreum.IsUnauthorizedSenderError

	initialRunnerEnv := NewRunnerEnv(ctx, t, initialRunnerEnvCfg, chains)
	initialRunnerEnv.StartAllRunnerProcesses()
	initialRunnerEnv.AllocateTickets(ctx, t, 200)

	// rotate max allowed signers set
	newRunnerEnvCfg := DefaultRunnerEnvConfig()
	newRunnerEnvCfg.RelayersCount = xrpl.MaxAllowedXRPLSigners
	newRunnerEnvCfg.SigningThreshold = xrpl.MaxAllowedXRPLSigners
	newRunnerEnvCfg.CustomBridgeXRPLAddress = &initialRunnerEnv.BridgeXRPLAddress
	newRunnerEnvCfg.CustomContractAddress = lo.ToPtr(initialRunnerEnv.ContractClient.GetContractAddress())
	newRunnerEnvCfg.CustomContractOwner = &initialRunnerEnv.ContractOwner
	// replace all relayers
	newRunnerEnv := NewRunnerEnv(ctx, t, newRunnerEnvCfg, chains)
	newSigningThreshold := xrpl.MaxAllowedXRPLSigners
	require.NoError(t, initialRunnerEnv.BridgeClient.RotateKeys(
		ctx,
		initialRunnerEnv.ContractOwner,
		bridgeclient.KeysRotationConfig{
			Relayers:          newRunnerEnv.BootstrappingConfig.Relayers,
			EvidenceThreshold: newSigningThreshold,
		},
	))
	initialRunnerEnv.AwaitNoPendingOperations(ctx, t)
	assertSignersAreUpdated(ctx, t, initialRunnerEnv, newRunnerEnv.BootstrappingConfig.Relayers, newSigningThreshold)

	// activate the bridge and start new relayers
	require.NoError(t, initialRunnerEnv.BridgeClient.ResumeBridge(ctx, initialRunnerEnv.ContractOwner))
	newRunnerEnv.StartAllRunnerProcesses()

	// register coreum denom
	registeredCoreumOriginatedToken := initialRunnerEnv.RegisterCoreumOriginatedToken(
		ctx,
		t,
		// use Coreum denom
		chains.Coreum.ChainSettings.Denom,
		6,
		6,
		sdkmath.NewIntWithDecimal(1, 30),
		sdkmath.ZeroInt(),
	)

	// send TrustSet to be able to receive coins from the bridge
	xrplCurrency, err := rippledata.NewCurrency(registeredCoreumOriginatedToken.XRPLCurrency)
	require.NoError(t, err)
	initialRunnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, initialRunnerEnv.BridgeXRPLAddress, xrplCurrency)

	amountToSendToXRPL := sdkmath.NewInt(100)
	initialRunnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSenderAddress,
		xrplRecipientAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
		nil,
	)

	initialRunnerEnv.AwaitNoPendingOperations(ctx, t)

	balance := initialRunnerEnv.Chains.XRPL.GetAccountBalance(
		ctx, t, xrplRecipientAddress, initialRunnerEnv.BridgeXRPLAddress, xrplCurrency,
	)
	require.Equal(t, "0.0001", balance.Value.String())
}

func assertSignersAreUpdated(
	ctx context.Context,
	t *testing.T,
	initialRunnerEnv *RunnerEnv,
	updatedRelayers []bridgeclient.RelayerConfig,
	newSigningThreshold uint32,
) {
	xrplBridgeAccountInfo, err := initialRunnerEnv.Chains.XRPL.RPCClient().
		AccountInfo(ctx, initialRunnerEnv.BridgeXRPLAddress)
	require.NoError(t, err)

	newSignerEntries := make([]rippledata.SignerEntry, 0, len(updatedRelayers))
	for _, relayer := range updatedRelayers {
		xrplRelayerAccount, err := rippledata.NewAccountFromAddress(relayer.XRPLAddress)
		require.NoError(t, err)
		newSignerEntries = append(newSignerEntries, rippledata.SignerEntry{
			SignerEntry: rippledata.SignerEntryItem{
				Account:      xrplRelayerAccount,
				SignerWeight: lo.ToPtr(uint16(1)),
			},
		})
	}
	require.Len(t, xrplBridgeAccountInfo.AccountData.SignerList, 1)
	require.Equal(t, newSigningThreshold, *xrplBridgeAccountInfo.AccountData.SignerList[0].SignerQuorum)
	require.ElementsMatch(t, newSignerEntries, xrplBridgeAccountInfo.AccountData.SignerList[0].SignerEntries)
}

func convertBridgeClientRelayersToContactRelayers(
	t *testing.T,
	relayers []bridgeclient.RelayerConfig,
) []coreum.Relayer {
	contractRelayers := make([]coreum.Relayer, 0, len(relayers))
	for _, relayer := range relayers {
		relayerCoreumAddress, err := sdk.AccAddressFromBech32(relayer.CoreumAddress)
		require.NoError(t, err)
		contractRelayers = append(contractRelayers, coreum.Relayer{
			CoreumAddress: relayerCoreumAddress,
			XRPLAddress:   relayer.XRPLAddress,
			XRPLPubKey:    relayer.XRPLPubKey,
		})
	}

	return contractRelayers
}
