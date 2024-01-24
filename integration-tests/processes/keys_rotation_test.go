//go:build integrationtests
// +build integrationtests

package processes_test

import (
	"sort"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum/v4/pkg/client"
	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	assetfttypes "github.com/CoreumFoundation/coreum/v4/x/asset/ft/types"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	bridgeclient "github.com/CoreumFoundation/coreumbridge-xrpl/relayer/client"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
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

	registeredCoreumOriginatedToken := initialRunnerEnv.RegisterCoreumOriginatedToken(
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
	initialRunnerEnv.SendXRPLMaxTrustSetTx(ctx, t, xrplRecipientAddress, initialRunnerEnv.BridgeXRPLAddress, xrplCurrency)

	amountToSendToXRPL := sdkmath.NewInt(1000040)
	initialRunnerEnv.SendFromCoreumToXRPL(
		ctx,
		t,
		coreumSenderAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
		xrplRecipientAddress,
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

	newSigningThreshold := 3
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
	require.Equal(t, uint32(newSigningThreshold), *xrplBridgeAccountInfo.AccountData.SignerList[0].SignerQuorum)

	xrplBridgerAddressSignerEntries := xrplBridgeAccountInfo.AccountData.SignerList[0].SignerEntries
	sortSignerEntries(newSignerEntries)
	sortSignerEntries(xrplBridgerAddressSignerEntries)
	require.EqualValues(t, newSignerEntries, xrplBridgerAddressSignerEntries)

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
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSendToXRPL),
		xrplRecipientAddress,
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

func sortSignerEntries(signerEntries []rippledata.SignerEntry) {
	sort.Slice(signerEntries, func(i, j int) bool {
		return signerEntries[i].SignerEntry.Account.String() > signerEntries[j].SignerEntry.Account.String()
	})
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
