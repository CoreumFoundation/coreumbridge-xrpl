package integrationtests

import (
	"context"
	"os"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v4/testutil/integration"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
)

// DeployAndInstantiateContract deploys and instantiates the contract.
func DeployAndInstantiateContract(
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

	contractClient := coreum.NewContractClient(
		coreum.DefaultContractClientConfig(sdk.AccAddress(nil)),
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

	contractAddress, err := contractClient.DeployAndInstantiate(ctx, owner,
		readBuiltContract(t, chains.Coreum.Config().ContractPath), instantiationCfg)
	require.NoError(t, err)

	require.NoError(t, contractClient.SetContractAddress(contractAddress))

	return owner, contractClient
}

func readBuiltContract(t *testing.T, contractPath string) []byte {
	t.Helper()

	body, err := os.ReadFile(contractPath)
	require.NoError(t, err)

	return body
}
