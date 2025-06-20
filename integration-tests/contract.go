package integrationtests

import (
	"context"
	"os"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v5/testutil/integration"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
)

// DeployInstantiateAndMigrateContract deploys and instantiates the mainnet version of the contract and applies
// migration.
func DeployInstantiateAndMigrateContract(
	ctx context.Context,
	t *testing.T,
	chains Chains,
	relayers []coreum.Relayer,
	evidenceThreshold uint32,
	usedTicketSequenceThreshold uint32,
	trustSetLimitAmount sdkmath.Int,
	bridgeXRPLAddress string,
	xrplBaseFee uint32,
) (sdk.AccAddress, *coreum.ContractClient) {
	t.Helper()

	owner, contractClient := DeployAndInstantiateContractV110(
		ctx,
		t,
		chains,
		relayers,
		evidenceThreshold,
		usedTicketSequenceThreshold,
		trustSetLimitAmount,
		bridgeXRPLAddress,
		xrplBaseFee,
	)

	MigrateContract(ctx, t, chains, contractClient, owner)

	return owner, contractClient
}

// MigrateContract migrates the contract to the compiled version.
func MigrateContract(
	ctx context.Context,
	t *testing.T,
	chains Chains,
	contractClient *coreum.ContractClient,
	owner sdk.AccAddress,
) {
	t.Helper()

	_, codeID, err := contractClient.DeployContract(ctx, owner, readBuiltContract(t, chains.Coreum.Config().ContractPath))
	require.NoError(t, err)
	_, err = contractClient.MigrateContract(ctx, owner, codeID)
	require.NoError(t, err)
}

// DeployAndInstantiateContractV110 deploys and instantiates the mainnet version of the contract.
func DeployAndInstantiateContractV110(
	ctx context.Context,
	t *testing.T,
	chains Chains,
	relayers []coreum.Relayer,
	evidenceThreshold uint32,
	usedTicketSequenceThreshold uint32,
	trustSetLimitAmount sdkmath.Int,
	bridgeXRPLAddress string,
	xrplBaseFee uint32,
) (sdk.AccAddress, *coreum.ContractClient) {
	t.Helper()

	t.Log("Deploying and instantiating contract")
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	owner := chains.Coreum.GenAccount()

	// fund with issuance fee and some coins to cover fees
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.AddRaw(2_000_000),
	})

	contactCfg := coreum.DefaultContractClientConfig(sdk.AccAddress(nil))

	// integration tests are running in parallel producing the high load on the chain, as a result, periodically,
	// with default gas adjustments, the tests might fail because of estimation delay and feemodel gas price change
	// the custom gas adjustment config prevents the failure
	contactCfg.GasAdjustment = 1.5
	contactCfg.GasPriceAdjustment = sdkmath.LegacyMustNewDecFromStr("1.5")

	contractClient := coreum.NewContractClient(
		contactCfg,
		chains.Log,
		chains.Coreum.ClientContext,
	)
	instantiationCfg := coreum.InstantiationConfig{
		Owner:                       owner,
		Admin:                       owner,
		Relayers:                    relayers,
		EvidenceThreshold:           evidenceThreshold,
		UsedTicketSequenceThreshold: usedTicketSequenceThreshold,
		TrustSetLimitAmount:         trustSetLimitAmount,
		BridgeXRPLAddress:           bridgeXRPLAddress,
		XRPLBaseFee:                 xrplBaseFee,
	}
	contractAddress, err := contractClient.DeployAndInstantiate(
		ctx, owner, readBuiltContract(t, chains.Coreum.Config().PreviousContractPath), instantiationCfg,
	)
	require.NoError(t, err)

	require.NoError(t, contractClient.SetContractAddress(contractAddress))

	return owner, contractClient
}

func readBuiltContract(t *testing.T, path string) []byte {
	t.Helper()

	body, err := os.ReadFile(path)
	require.NoError(t, err)

	return body
}
