//go:build integrationtests
// +build integrationtests

package coreum_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	coreumintegration "github.com/CoreumFoundation/coreum/v3/testutil/integration"
	assetfttypes "github.com/CoreumFoundation/coreum/v3/x/asset/ft/types"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

const (
	compiledContractFilePath = "../../contract/artifacts/coreumbridge_xrpl.wasm"
	xrp                      = "XRP"
	drop                     = "drop"
	xrplPrecision            = 15
	xrpIssuer                = "rrrrrrrrrrrrrrrrrrrrrho"
	xrpCurrency              = "XRP"
)

func TestDeployAndInstantiateContract(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	assetftClient := assetfttypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := []sdk.AccAddress{
		chains.Coreum.GenAccount(),
	}
	maxAllowedUsedTickets := 10
	owner, contractClient := deployAndInstantiateContract(ctx, t, chains, relayers, len(relayers), maxAllowedUsedTickets)

	contractCfg, err := contractClient.GetContractConfig(ctx)
	require.NoError(t, err)

	require.Equal(t, coreum.ContractConfig{
		Relayers:              relayers,
		EvidenceThreshold:     len(relayers),
		MaxAllowedUsedTickets: maxAllowedUsedTickets,
	}, contractCfg)

	contractOwnership, err := contractClient.GetContractOwnership(ctx)
	require.NoError(t, err)

	require.Equal(t, coreum.ContractOwnership{
		Owner:        owner,
		PendingOwner: sdk.AccAddress{},
	}, contractOwnership)

	contractAddress := contractClient.GetContractAddress()
	tokensRes, err := assetftClient.Tokens(ctx, &assetfttypes.QueryTokensRequest{
		Issuer: contractAddress.String(),
	})
	require.NoError(t, err)
	require.Len(t, tokensRes.Tokens, 1)

	coreumDenom := fmt.Sprintf("%s-%s", drop, contractAddress.String())
	require.Equal(t, assetfttypes.Token{
		Denom:              coreumDenom,
		Issuer:             contractAddress.String(),
		Symbol:             xrp,
		Subunit:            drop,
		Precision:          6,
		Description:        "",
		GloballyFrozen:     false,
		Features:           []assetfttypes.Feature{assetfttypes.Feature_minting, assetfttypes.Feature_burning, assetfttypes.Feature_ibc},
		BurnRate:           sdk.ZeroDec(),
		SendCommissionRate: sdk.ZeroDec(),
		Version:            assetfttypes.CurrentTokenVersion,
	}, tokensRes.Tokens[0])

	// query all tokens
	xrplTokens, err := contractClient.GetXRPLTokens(ctx)
	require.NoError(t, err)

	require.Len(t, xrplTokens, 1)
	require.Equal(t, coreum.XRPLToken{
		Issuer:      xrpIssuer,
		Currency:    xrpCurrency,
		CoreumDenom: coreumDenom,
	}, xrplTokens[0])
}

func TestChangeContractOwnership(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := []sdk.AccAddress{
		chains.Coreum.GenAccount(),
	}
	maxAllowedUsedTickets := 10

	owner, contractClient := deployAndInstantiateContract(ctx, t, chains, relayers, len(relayers), maxAllowedUsedTickets)
	contractOwnership, err := contractClient.GetContractOwnership(ctx)
	require.NoError(t, err)
	require.Equal(t, owner.String(), contractOwnership.Owner.String())

	newOwner := chains.Coreum.GenAccount()
	// fund to cover fees
	chains.Coreum.FundAccountWithOptions(ctx, t, newOwner, coreumintegration.BalancesOptions{
		Amount: sdk.NewIntFromUint64(1_000_000),
	})

	// transfer ownership
	_, err = contractClient.TransferOwnership(ctx, owner, newOwner)
	require.NoError(t, err)
	contractOwnership, err = contractClient.GetContractOwnership(ctx)
	require.NoError(t, err)
	// the owner is still old until new owner accepts the ownership
	require.Equal(t, owner.String(), contractOwnership.Owner.String())
	require.Equal(t, newOwner.String(), contractOwnership.PendingOwner.String())

	// accept the ownership
	_, err = contractClient.AcceptOwnership(ctx, newOwner)
	require.NoError(t, err)
	contractOwnership, err = contractClient.GetContractOwnership(ctx)
	require.NoError(t, err)
	// the owner is still old until new owner accepts the ownership
	require.Equal(t, newOwner.String(), contractOwnership.Owner.String())

	// try to update the ownership one more time (from old owner)
	_, err = contractClient.TransferOwnership(ctx, owner, newOwner)
	require.True(t, coreum.IsNotOwnerError(err))
}

func TestRegisterCoreumToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	relayers := []sdk.AccAddress{
		chains.Coreum.GenAccount(),
	}
	maxAllowedUsedTickets := 10

	notOwner := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, notOwner, coreumintegration.BalancesOptions{
		Amount: sdk.NewInt(1_000_000),
	})

	owner, contractClient := deployAndInstantiateContract(ctx, t, chains, relayers, len(relayers), maxAllowedUsedTickets)

	denom1 := "denom1"
	denom1Decimals := uint32(17)

	// try to register from not owner
	_, err := contractClient.RegisterCoreumToken(ctx, notOwner, denom1, denom1Decimals)
	require.True(t, coreum.IsNotOwnerError(err))

	// register from the owner
	_, err = contractClient.RegisterCoreumToken(ctx, owner, denom1, denom1Decimals)
	require.NoError(t, err)

	// try to register the same denom one more time
	_, err = contractClient.RegisterCoreumToken(ctx, owner, denom1, denom1Decimals)
	require.True(t, coreum.IsCoreumTokenAlreadyRegisteredError(err))

	coreumTokens, err := contractClient.GetCoreumTokens(ctx)
	require.NoError(t, err)
	require.Len(t, coreumTokens, 1)

	registeredToken := coreumTokens[0]
	require.Equal(t, denom1, registeredToken.Denom)
	require.Equal(t, denom1Decimals, registeredToken.Decimals)
	require.NotEmpty(t, registeredToken.XRPLCurrency)

	// try to use the registered denom with new XRPL currency on the XRPL chain
	issuerAcc := chains.XRPL.GenAccount(ctx, t, 10)
	recipientAcc := chains.XRPL.GenAccount(ctx, t, 10)

	// allow to receive the currency
	currency, err := xrpl.StringToHexXRPLCurrency(registeredToken.XRPLCurrency)
	require.NoError(t, err)
	amountToSend, err := rippledata.NewValue("10000000000000000", false)
	require.NoError(t, err)
	trustSetTx := rippledata.TrustSet{
		LimitAmount: rippledata.Amount{
			Value:    amountToSend,
			Currency: currency,
			Issuer:   issuerAcc,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TRUST_SET,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &trustSetTx, recipientAcc))

	paymentTx := rippledata.Payment{
		Destination: recipientAcc,
		Amount: rippledata.Amount{
			Value:    amountToSend,
			Currency: currency,
			Issuer:   issuerAcc,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}

	balancesBeforeSend := chains.XRPL.GetAccountBalances(ctx, t, recipientAcc)
	t.Logf("Recipinet account balances before send: %s", balancesBeforeSend)
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &paymentTx, issuerAcc))
	balancesAfterSend := chains.XRPL.GetAccountBalances(ctx, t, recipientAcc)
	t.Logf("Recipinet account balances after send: %s", balancesAfterSend)
	receiveAmount, ok := balancesAfterSend[fmt.Sprintf("%s/%s", currency.String(), issuerAcc.String())]
	require.True(t, ok)
	require.Equal(t, amountToSend.String(), receiveAmount.Value.String())
}

func TestRegisterXRPLToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	assetftClient := assetfttypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := []sdk.AccAddress{
		chains.Coreum.GenAccount(),
	}
	maxAllowedUsedTickets := 10

	notOwner := chains.Coreum.GenAccount()

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	// fund with issuance fee and some coins on to cover fees
	chains.Coreum.FundAccountWithOptions(ctx, t, notOwner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.AddRaw(1_000_000),
	})

	owner, contractClient := deployAndInstantiateContract(ctx, t, chains, relayers, len(relayers), maxAllowedUsedTickets)
	// fund owner to cover registration fees twice
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Mul(sdk.NewIntFromUint64(2)),
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := "CRR"

	// try to register from not owner
	_, err := contractClient.RegisterXRPLToken(ctx, notOwner, issuer, currency)
	require.True(t, coreum.IsNotOwnerError(err))

	// register from the owner
	_, err = contractClient.RegisterXRPLToken(ctx, owner, issuer, currency)
	require.NoError(t, err)

	// try to register the same denom one more time
	_, err = contractClient.RegisterXRPLToken(ctx, owner, issuer, currency)
	require.True(t, coreum.IsXRPLTokenAlreadyRegisteredError(err))

	xrplTokens, err := contractClient.GetXRPLTokens(ctx)
	require.NoError(t, err)
	// one XRP token and registered
	require.Len(t, xrplTokens, 2)

	var registeredToken coreum.XRPLToken
	for _, token := range xrplTokens {
		if token.Issuer == issuer && token.Currency == currency {
			registeredToken = token
			break
		}
	}
	require.Equal(t, issuer, registeredToken.Issuer)
	require.Equal(t, currency, registeredToken.Currency)
	require.NotEmpty(t, registeredToken.CoreumDenom)

	// check that corresponding token is issued
	contractAddress := contractClient.GetContractAddress()

	tokenRes, err := assetftClient.Token(ctx, &assetfttypes.QueryTokenRequest{
		Denom: registeredToken.CoreumDenom,
	})
	require.NoError(t, err)

	// deconstruct the denom to get prefix used for the symbol and subunit
	prefix, _, err := assetfttypes.DeconstructDenom(registeredToken.CoreumDenom)
	require.NoError(t, err)

	require.Equal(t, assetfttypes.Token{
		Denom:              registeredToken.CoreumDenom,
		Issuer:             contractAddress.String(),
		Symbol:             strings.ToUpper(prefix),
		Subunit:            prefix,
		Precision:          xrplPrecision,
		Description:        "",
		GloballyFrozen:     false,
		Features:           []assetfttypes.Feature{assetfttypes.Feature_minting, assetfttypes.Feature_burning, assetfttypes.Feature_ibc},
		BurnRate:           sdk.ZeroDec(),
		SendCommissionRate: sdk.ZeroDec(),
		Version:            assetfttypes.CurrentTokenVersion,
	}, tokenRes.Token)
}

func deployAndInstantiateContract(
	ctx context.Context,
	t *testing.T,
	chains integrationtests.Chains,
	relayers []sdk.AccAddress,
	evidenceThreshold int,
	maxAllowedUsedTickets int,
) (sdk.AccAddress, *coreum.ContractClient) {
	t.Helper()

	t.Log("Deploying and instantiating contract")
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	owner := chains.Coreum.GenAccount()

	// fund with issuance fee and some coins on to cover fees
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.AddRaw(10_000_000),
	})

	contractClient := coreum.NewContractClient(coreum.DefaultContractClientConfig(sdk.AccAddress(nil)), chains.Log, chains.Coreum.ClientContext)
	instantiationCfg := coreum.InstantiationConfig{
		Owner:                 owner,
		Admin:                 owner,
		Relayers:              relayers,
		EvidenceThreshold:     evidenceThreshold,
		MaxAllowedUsedTickets: maxAllowedUsedTickets,
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
