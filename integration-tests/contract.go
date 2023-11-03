package integrationtests

import (
	"context"
	"os"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v3/testutil/integration"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
)

const compiledContractFilePath = "../../build/coreumbridge_xrpl.wasm"

// DeployAndInstantiateContract deploys and instantiates the contract.
func DeployAndInstantiateContract(
	ctx context.Context,
	t *testing.T,
	chains Chains,
	relayers []coreum.Relayer,
	evidenceThreshold int,
	usedTicketsThreshold int,
	trustSetLimitAmount string,
) (sdk.AccAddress, *coreum.ContractClient) {
	t.Helper()

	t.Log("Deploying and instantiating contract")
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	owner := chains.Coreum.GenAccount()

	// fund with issuance fee and some coins to cover fees
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.AddRaw(1_000_000),
	})

	contractClient := coreum.NewContractClient(coreum.DefaultContractClientConfig(sdk.AccAddress(nil)), chains.Log, chains.Coreum.ClientContext)
	instantiationCfg := coreum.InstantiationConfig{
		Owner:                owner,
		Admin:                owner,
		Relayers:             relayers,
		EvidenceThreshold:    evidenceThreshold,
		UsedTicketsThreshold: usedTicketsThreshold,
		TrustSetLimitAmount:  trustSetLimitAmount,
	}
	contractAddress, err := contractClient.DeployAndInstantiate(ctx, owner, readBuiltContract(t), instantiationCfg)
	require.NoError(t, err)

	require.NoError(t, contractClient.SetContractAddress(contractAddress))

	return owner, contractClient
}

func readBuiltContract(t *testing.T) []byte {
	t.Helper()

	body, err := os.ReadFile(compiledContractFilePath)
	require.NoError(t, err)

	return body
}
