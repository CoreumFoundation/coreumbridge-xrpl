//go:build integrationtests
// +build integrationtests

package contract_test

import (
	"context"
	"strconv"
	"testing"

	sdkmath "cosmossdk.io/math"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmoserrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum/v5/pkg/client"
	"github.com/CoreumFoundation/coreum/v5/testutil/event"
	coreumintegration "github.com/CoreumFoundation/coreum/v5/testutil/integration"
	assetfttypes "github.com/CoreumFoundation/coreum/v5/x/asset/ft/types"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestSendFromXRPLToCoreumXRPLOriginatedToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumRecipient := chains.Coreum.GenAccount()
	randomAddress := chains.Coreum.GenAccount()
	relayers := genRelayers(ctx, t, chains, 2)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	chains.Coreum.FundAccountWithOptions(ctx, t, randomAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))
	sendingPrecision := int32(15)
	maxHoldingAmount := sdkmath.NewInt(10000)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	// register from the owner
	bridgingFee := sdkmath.NewInt(3)
	_, err := contractClient.RegisterXRPLToken(
		ctx,
		owner,
		issuer,
		currency,
		sendingPrecision,
		maxHoldingAmount,
		bridgingFee,
	)
	require.NoError(t, err)

	xrplTokens, err := contractClient.GetXRPLTokens(ctx)
	require.NoError(t, err)
	// find registered token
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

	// activate token
	activateXRPLToken(ctx, t, contractClient, relayers, issuer, currency)

	// create an evidence of transfer tokens from XRPL to Coreum
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    integrationtests.GenXRPLTxHash(t),
		Issuer:    issuerAcc.String(),
		Currency:  currency,
		Amount:    sdkmath.NewInt(10),
		Recipient: coreumRecipient,
	}

	// try to call from not relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, randomAddress, xrplToCoreumTransferEvidence)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// try use not registered token
	wrongXRPLToCoreumTransferEvidence := xrplToCoreumTransferEvidence
	wrongXRPLToCoreumTransferEvidence.Currency = "NEZ"
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		wrongXRPLToCoreumTransferEvidence,
	)
	require.True(t, coreum.IsTokenNotRegisteredError(err), err)

	// try to provide the evidence with prohibited address
	xrplToCoreumTransferEvidenceWithProhibitedAddress := xrplToCoreumTransferEvidence
	xrplToCoreumTransferEvidenceWithProhibitedAddress.Recipient = contractClient.GetContractAddress()
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidenceWithProhibitedAddress,
	)
	require.True(t, coreum.IsProhibitedAddressError(err), err)

	// call from first relayer
	txRes, err := contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)

	transactionEvidences, err := contractClient.GetTransactionEvidences(ctx)
	require.NoError(t, err)
	require.Len(t, transactionEvidences, 1)
	require.Len(t, transactionEvidences[0].RelayerAddresses, 1)
	require.Equal(t, transactionEvidences[0].RelayerAddresses[0].String(), relayers[0].CoreumAddress.String())

	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.True(t, recipientBalanceRes.Balance.IsZero())
	thresholdReached, err := event.FindStringEventAttribute(
		txRes.Events,
		wasmtypes.ModuleName,
		eventAttributeThresholdReached,
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(false), thresholdReached)

	// call from first relayer one more time
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.True(t, coreum.IsEvidenceAlreadyProvidedError(err), err)

	// call from second relayer
	txRes, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[1].CoreumAddress, xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)
	recipientBalanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredToken.CoreumDenom,
	})
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(
		txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(true), thresholdReached)

	transactionEvidences, err = contractClient.GetTransactionEvidences(ctx)
	require.NoError(t, err)
	require.Empty(t, transactionEvidences)

	require.NoError(t, err)
	// expect new token on the recipient balance
	// the amount is sent_amount - bridge_fee
	require.Equal(t,
		xrplToCoreumTransferEvidence.Amount.Sub(bridgingFee).String(),
		recipientBalanceRes.Balance.Amount.String())

	// try to push the same evidence
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence,
	)
	require.True(t, coreum.IsOperationAlreadyExecutedError(err), err)

	// assert fees are calculated correctly
	claimFeesAndMakeAssertions(
		ctx,
		t,
		contractClient,
		bankClient,
		relayers,
		bridgingFee,
		sdkmath.ZeroInt(),
		registeredToken.CoreumDenom,
	)
}

func TestSendFromXRPLToCoreumXRPLOriginatedTokenWithMaxAmount(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumRecipient := chains.Coreum.GenAccount()
	randomAddress := chains.Coreum.GenAccount()
	relayers := genRelayers(ctx, t, chains, 2)

	chains.Coreum.FundAccountWithOptions(ctx, t, randomAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))
	sendingPrecision := int32(15)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	_, err := contractClient.RegisterXRPLToken(
		ctx,
		owner,
		issuer,
		currency,
		sendingPrecision,
		coreum.MaxContractAmount,
		sdkmath.ZeroInt(),
	)
	require.NoError(t, err)

	// activate token
	activateXRPLToken(ctx, t, contractClient, relayers, issuer, currency)

	xrplToCoreumTransferEvidenceWithHightAmount := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    integrationtests.GenXRPLTxHash(t),
		Issuer:    issuerAcc.String(),
		Currency:  currency,
		Amount:    coreum.MaxContractAmount.AddRaw(1),
		Recipient: coreumRecipient,
	}
	// try to send the amount which is greater than max
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidenceWithHightAmount,
	)
	require.ErrorContains(t, err, "invalid Uint128")
	// send max amount
	xrplToCoreumTransferEvidence := xrplToCoreumTransferEvidenceWithHightAmount
	xrplToCoreumTransferEvidence.Amount = coreum.MaxContractAmount
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence)
	require.NoError(t, err)
}

func TestSendFromXRPLToCoreumXRPLOriginatedTokenTooLowAmounts(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumRecipient := chains.Coreum.GenAccount()
	randomAddress := chains.Coreum.GenAccount()
	relayers := genRelayers(ctx, t, chains, 2)

	chains.Coreum.FundAccountWithOptions(ctx, t, randomAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	_, err := contractClient.RegisterXRPLToken(
		ctx,
		owner,
		issuer,
		currency,
		int32(2),
		coreum.MaxContractAmount,
		sdkmath.NewInt(10),
	)
	require.NoError(t, err)

	// activate token
	activateXRPLToken(ctx, t, contractClient, relayers, issuer, currency)

	xrplToCoreumTransferEvidenceWithAmountZeroAfterTruncation := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    integrationtests.GenXRPLTxHash(t),
		Issuer:    issuerAcc.String(),
		Currency:  currency,
		Amount:    sdkmath.NewInt(100),
		Recipient: coreumRecipient,
	}

	// try to send the amount which is zero after truncation
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidenceWithAmountZeroAfterTruncation,
	)
	require.True(t, coreum.IsAmountSentIsZeroAfterTruncationError(err), err)

	xrplToCoreumTransferEvidenceWithAmountNotEnoughToCoverBridgingFee := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    integrationtests.GenXRPLTxHash(t),
		Issuer:    issuerAcc.String(),
		Currency:  currency,
		Amount:    sdkmath.NewInt(5),
		Recipient: coreumRecipient,
	}

	// try to send the amount which is zero after truncation
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidenceWithAmountNotEnoughToCoverBridgingFee,
	)
	require.True(t, coreum.IsCannotCoverBridgingFeesError(err), err)
}

func TestSendFromXRPLToCoreumModuleAccount(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	moduleAccountRecipient := authtypes.NewModuleAddress(govtypes.ModuleName)
	relayers := genRelayers(ctx, t, chains, 2)

	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	// fund owner to cover issuance fees
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))
	sendingPrecision := int32(15)
	maxHoldingAmount := sdkmath.NewIntFromUint64(10000)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	// register from the owner
	_, err := contractClient.RegisterXRPLToken(
		ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)

	xrplTokens, err := contractClient.GetXRPLTokens(ctx)
	require.NoError(t, err)
	// find registered token
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

	// activate token
	activateXRPLToken(ctx, t, contractClient, relayers, issuer, currency)

	// create an evidence of transfer tokens from XRPL to Coreum
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    integrationtests.GenXRPLTxHash(t),
		Issuer:    issuerAcc.String(),
		Currency:  currency,
		Amount:    sdkmath.NewInt(10),
		Recipient: moduleAccountRecipient,
	}

	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)

	// sending to module account is blocked.
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[1].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.True(t, coreum.IsRecipientBlockedError(err), err)
}

//nolint:tparallel // the test is parallel, but test cases are not
func TestSendFromXRPLToCoreumXRPLOriginatedTokenWithDifferentSendingPrecision(t *testing.T) {
	t.Parallel()

	var (
		tokenDecimals        = int64(15)
		highMaxHoldingAmount = integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)
	)

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 2)
	coreumRecipient := chains.Coreum.GenAccount()

	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	tests := []struct {
		name                                       string
		sendingPrecision                           int32
		sendingAmount                              sdkmath.Int
		maxHoldingAmount                           sdkmath.Int
		wantReceivedAmount                         sdkmath.Int
		wantIsAmountSentIsZeroAfterTruncationError bool
		wantIsMaximumBridgedAmountReachedError     bool
	}{
		{
			name:               "positive_precision_no_truncation",
			sendingPrecision:   2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", tokenDecimals),
		},
		{
			name:               "positive_precision_with_truncation",
			sendingPrecision:   2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15567", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15", tokenDecimals),
		},
		{
			name:             "positive_precision_low_amount",
			sendingPrecision: 2,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.009999", tokenDecimals),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "zero_precision_no_truncation",
			sendingPrecision:   0,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", tokenDecimals),
		},
		{
			name:               "zero_precision_with_truncation",
			sendingPrecision:   0,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1.15567", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", tokenDecimals),
		},
		{
			name:             "zero_precision_low_amount",
			sendingPrecision: 0,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.9999", tokenDecimals),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "negative_precision_no_truncation",
			sendingPrecision:   -2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", tokenDecimals),
		},
		{
			name:               "negative_precision_with_truncation",
			sendingPrecision:   -2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999.15567", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9900", tokenDecimals),
		},
		{
			name:             "negative_precision_low_amount",
			sendingPrecision: -2,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "99.9999", tokenDecimals),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:             "reached_max_holding_amount",
			sendingPrecision: 2,
			maxHoldingAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999", tokenDecimals),
			sendingAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(
				t, "9999.01", tokenDecimals,
			),
			wantIsMaximumBridgedAmountReachedError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// fund owner to cover registration fee
			chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
				Amount: issueFee.Amount,
			})

			issuerAcc := xrpl.GenPrivKeyTxSigner().Account()
			issuer := issuerAcc.String()
			currency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))

			// register from the owner
			_, err := contractClient.RegisterXRPLToken(
				ctx, owner, issuer, currency, tt.sendingPrecision, tt.maxHoldingAmount, sdkmath.ZeroInt(),
			)
			require.NoError(t, err)
			registeredXRPLToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
			require.NoError(t, err)

			// activate token
			activateXRPLToken(ctx, t, contractClient, relayers, issuerAcc.String(), currency)

			// create an evidence
			xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
				TxHash:    integrationtests.GenXRPLTxHash(t),
				Issuer:    issuerAcc.String(),
				Currency:  currency,
				Amount:    tt.sendingAmount,
				Recipient: coreumRecipient,
			}

			// call from all relayers
			for _, relayer := range relayers {
				_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence)
				if tt.wantIsAmountSentIsZeroAfterTruncationError {
					require.True(t, coreum.IsAmountSentIsZeroAfterTruncationError(err), err)
					return
				}
				if tt.wantIsMaximumBridgedAmountReachedError {
					require.True(t, coreum.IsMaximumBridgedAmountReachedError(err), err)
					return
				}
				require.NoError(t, err)
			}

			balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: coreumRecipient.String(),
				Denom:   registeredXRPLToken.CoreumDenom,
			})
			require.NoError(t, err)
			require.Equal(t, tt.wantReceivedAmount.String(), balanceRes.Balance.Amount.String())
		})
	}
}

func TestSendFromXRPLToCoreumXRPToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumRecipient := chains.Coreum.GenAccount()
	randomAddress := chains.Coreum.GenAccount()
	relayers := genRelayers(ctx, t, chains, 2)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	chains.Coreum.FundAccountWithOptions(ctx, t, randomAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	_, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	registeredXRPToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx,
		xrpl.XRPTokenIssuer.String(),
		xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
	)
	require.NoError(t, err)

	require.Equal(t, coreum.XRPLToken{
		Issuer:           xrpl.XRPTokenIssuer.String(),
		Currency:         xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
		CoreumDenom:      assetfttypes.BuildDenom("drop", contractClient.GetContractAddress()),
		SendingPrecision: 6,
		MaxHoldingAmount: sdkmath.NewIntWithDecimal(1, 16),
		State:            coreum.TokenStateEnabled,
		BridgingFee:      sdkmath.ZeroInt(),
	}, registeredXRPToken)

	// create an evidence of transfer tokens from XRPL to Coreum
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    integrationtests.GenXRPLTxHash(t),
		Issuer:    rippledata.Account{}.String(),
		Currency:  rippledata.Currency{}.String(),
		Amount:    sdkmath.NewInt(10),
		Recipient: coreumRecipient,
	}

	// try to call from not relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, randomAddress, xrplToCoreumTransferEvidence)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// call from first relayer
	txRes, err := contractClient.SendXRPLToCoreumTransferEvidence(
		ctx,
		relayers[0].CoreumAddress,
		xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)
	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredXRPToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.True(t, recipientBalanceRes.Balance.IsZero())
	thresholdReached, err := event.FindStringEventAttribute(
		txRes.Events,
		wasmtypes.ModuleName,
		eventAttributeThresholdReached,
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(false), thresholdReached)

	// call from first relayer one more time
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence,
	)
	require.True(t, coreum.IsEvidenceAlreadyProvidedError(err), err)

	// call from second relayer
	txRes, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[1].CoreumAddress, xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)
	recipientBalanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredXRPToken.CoreumDenom,
	})
	require.NoError(t, err)
	thresholdReached, err = event.FindStringEventAttribute(
		txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(true), thresholdReached)

	require.NoError(t, err)
	// expect new token on the recipient balance
	require.Equal(t, xrplToCoreumTransferEvidence.Amount.String(), recipientBalanceRes.Balance.Amount.String())

	// try to push the same evidence
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence)
	require.True(t, coreum.IsOperationAlreadyExecutedError(err), err)
}

func TestSendFromXRPLToCoreumCoreumOriginatedToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumSender := chains.Coreum.GenAccount()
	coreumRecipient := chains.Coreum.GenAccount()
	xrplRecipient := chains.XRPL.GenAccount(ctx, t, 1)

	relayers := genRelayers(ctx, t, chains, 2)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.MulRaw(2).Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 10)

	// issue asset ft and register it
	sendingPrecision := int32(5)
	tokenDecimals := uint32(5)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 11)
	initialAmount := sdkmath.NewIntWithDecimal(1, 11)
	registeredCoreumOriginatedToken := issueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		contractClient,
		chains.Coreum,
		coreumSender,
		owner,
		tokenDecimals,
		initialAmount,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)

	coinToSend := sdk.NewCoin(registeredCoreumOriginatedToken.Denom, sdkmath.NewInt(10))
	sendFromCoreumToXRPL(ctx, t, contractClient, relayers, coreumSender, coinToSend, xrplRecipient)

	contractBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: contractClient.GetContractAddress().String(),
		Denom:   registeredCoreumOriginatedToken.Denom,
	})
	require.NoError(t, err)
	require.Equal(t, coinToSend.String(), contractBalanceRes.Balance.String())

	// create an evidence of transfer tokens from XRPL to Coreum
	// account has 100_000_000_000 in XRPL after conversion
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:   integrationtests.GenXRPLTxHash(t),
		Issuer:   bridgeXRPLAddress,
		Currency: registeredCoreumOriginatedToken.XRPLCurrency,
		// Equivalent of sending 4 tokens back
		Amount:    sdkmath.NewInt(40_000_000_000),
		Recipient: coreumRecipient,
	}

	xrplToCoreumTransferEvidenceForNotRegisteredToken := xrplToCoreumTransferEvidence
	xrplToCoreumTransferEvidenceForNotRegisteredToken.Currency = "XZA"
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidenceForNotRegisteredToken,
	)
	require.True(t, coreum.IsTokenNotRegisteredError(err), err)

	// call from first relayer
	_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence)
	require.NoError(t, err)

	// call from second relayer
	txRes, err := contractClient.SendXRPLToCoreumTransferEvidence(
		ctx, relayers[1].CoreumAddress, xrplToCoreumTransferEvidence,
	)
	require.NoError(t, err)
	thresholdReached, err := event.FindStringEventAttribute(
		txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
	)
	require.NoError(t, err)
	require.Equal(t, strconv.FormatBool(true), thresholdReached)

	// check recipient balance
	recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredCoreumOriginatedToken.Denom,
	})
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(4).String(), recipientBalanceRes.Balance.Amount.String())

	// check contract balance
	contractBalanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: contractClient.GetContractAddress().String(),
		Denom:   registeredCoreumOriginatedToken.Denom,
	})
	require.NoError(t, err)
	require.Equal(t, coinToSend.Amount.Sub(sdkmath.NewInt(4)).String(), contractBalanceRes.Balance.Amount.String())
}

//nolint:tparallel // the test is parallel, but test cases are not
func TestSendFromXRPLToCoreumCoreumOriginatedTokenWithFreezingAndWhitelisting(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumRecipient := chains.Coreum.GenAccount()
	xrplRecipient := chains.XRPL.GenAccount(ctx, t, 1)

	relayers := genRelayers(ctx, t, chains, 2)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	amountToSend := sdkmath.NewInt(10_000)

	tests := []struct {
		name              string
		features          []assetfttypes.Feature
		beforeSendToXRPL  func(t *testing.T, issuer sdk.AccAddress, denom string)
		afterSendToXRPL   func(t *testing.T, issuer sdk.AccAddress, denom string)
		checkAssetFTError func(t *testing.T, err error)
	}{
		{
			name: "freezing_of_the_contract_account",
			features: []assetfttypes.Feature{
				assetfttypes.Feature_freezing,
			},
			afterSendToXRPL: func(t *testing.T, issuer sdk.AccAddress, denom string) {
				msg := &assetfttypes.MsgFreeze{
					Sender:  issuer.String(),
					Account: contractClient.GetContractAddress().String(),
					Coin:    sdk.NewCoin(denom, amountToSend),
				}
				_, err := client.BroadcastTx(
					ctx,
					chains.Coreum.ClientContext.WithFromAddress(issuer),
					chains.Coreum.TxFactory().WithSimulateAndExecute(true),
					msg,
				)
				require.NoError(t, err)
			},
			checkAssetFTError: func(t *testing.T, err error) {
				require.True(t, coreum.IsAssetFTFreezingError(err), err)
			},
		},
		{
			name: "global_freezing",
			features: []assetfttypes.Feature{
				assetfttypes.Feature_freezing,
			},
			afterSendToXRPL: func(t *testing.T, issuer sdk.AccAddress, denom string) {
				msg := &assetfttypes.MsgGloballyFreeze{
					Sender: issuer.String(),
					Denom:  denom,
				}
				_, err := client.BroadcastTx(
					ctx,
					chains.Coreum.ClientContext.WithFromAddress(issuer),
					chains.Coreum.TxFactory().WithSimulateAndExecute(true),
					msg,
				)
				require.NoError(t, err)
			},
			checkAssetFTError: func(t *testing.T, err error) {
				require.True(t, coreum.IsAssetFTGlobalFreezingError(err), err)
			},
		},
		{
			name: "whitelisting",
			features: []assetfttypes.Feature{
				assetfttypes.Feature_whitelisting,
			},
			beforeSendToXRPL: func(t *testing.T, issuer sdk.AccAddress, denom string) {
				msg := &assetfttypes.MsgSetWhitelistedLimit{
					Sender:  issuer.String(),
					Account: contractClient.GetContractAddress().String(),
					Coin:    sdk.NewCoin(denom, amountToSend),
				}
				_, err := client.BroadcastTx(
					ctx,
					chains.Coreum.ClientContext.WithFromAddress(issuer),
					chains.Coreum.TxFactory().WithSimulateAndExecute(true),
					msg,
				)
				require.NoError(t, err)
			},
			checkAssetFTError: func(t *testing.T, err error) {
				require.True(t, coreum.IsAssetFTWhitelistedLimitExceededError(err), err)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coreumSender := chains.Coreum.GenAccount()
			issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
			chains.Coreum.FundAccountWithOptions(ctx, t, coreumSender, coreumintegration.BalancesOptions{
				Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 6)),
			})

			sendingPrecision := int32(5)
			tokenDecimals := uint32(5)
			maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 11)
			issueMsg := &assetfttypes.MsgIssue{
				Issuer:        coreumSender.String(),
				Symbol:        "symbol",
				Subunit:       "subunit",
				Precision:     tokenDecimals,
				InitialAmount: maxHoldingAmount,
				Features:      tt.features,
			}
			_, err := client.BroadcastTx(
				ctx,
				chains.Coreum.ClientContext.WithFromAddress(coreumSender),
				chains.Coreum.TxFactory().WithSimulateAndExecute(true),
				issueMsg,
			)
			require.NoError(t, err)
			denom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSender)
			_, err = contractClient.RegisterCoreumToken(
				ctx, owner, denom, tokenDecimals, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
			)
			require.NoError(t, err)
			registeredCoreumToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
			require.NoError(t, err)

			if tt.beforeSendToXRPL != nil {
				tt.beforeSendToXRPL(t, coreumSender, denom)
			}

			coinToSend := sdk.NewCoin(denom, amountToSend)
			sendFromCoreumToXRPL(ctx, t, contractClient, relayers, coreumSender, coinToSend, xrplRecipient)

			if tt.afterSendToXRPL != nil {
				tt.afterSendToXRPL(t, coreumSender, denom)
			}

			contractBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: contractClient.GetContractAddress().String(),
				Denom:   denom,
			})
			require.NoError(t, err)
			require.Equal(t, coinToSend.String(), contractBalanceRes.Balance.String())

			// The amount is the converted amount that was sent in XRPL and is the equivalent of 10_000 in Coreum
			amountToSendBack := sdkmath.NewIntWithDecimal(1, 15)

			// create an evidence of transfer tokens from XRPL to Coreum
			xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
				TxHash:    integrationtests.GenXRPLTxHash(t),
				Issuer:    bridgeXRPLAddress,
				Currency:  registeredCoreumToken.XRPLCurrency,
				Amount:    amountToSendBack,
				Recipient: coreumRecipient,
			}

			// call from first relayer
			_, err = contractClient.SendXRPLToCoreumTransferEvidence(
				ctx, relayers[0].CoreumAddress, xrplToCoreumTransferEvidence,
			)
			require.NoError(t, err)

			// call from second relayer
			txRes, err := contractClient.SendXRPLToCoreumTransferEvidence(
				ctx, relayers[1].CoreumAddress, xrplToCoreumTransferEvidence,
			)
			if tt.checkAssetFTError != nil {
				require.True(t, coreum.IsAssetFTStateError(err), err)
				tt.checkAssetFTError(t, err)
				return
			}

			require.NoError(t, err)
			thresholdReached, err := event.FindStringEventAttribute(
				txRes.Events, wasmtypes.ModuleName, eventAttributeThresholdReached,
			)
			require.NoError(t, err)
			require.Equal(t, strconv.FormatBool(true), thresholdReached)

			// check recipient balance
			recipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: coreumRecipient.String(),
				Denom:   registeredCoreumToken.Denom,
			})
			require.NoError(t, err)
			require.Equal(
				t,
				xrplToCoreumTransferEvidence.Amount.String(),
				recipientBalanceRes.Balance.Amount.String(),
			)

			// check contract balance
			contractBalanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: contractClient.GetContractAddress().String(),
				Denom:   denom,
			})
			require.NoError(t, err)
			require.Equal(
				t,
				coinToSend.Amount.Sub(xrplToCoreumTransferEvidence.Amount).String(),
				contractBalanceRes.Balance.Amount.String(),
			)
		})
	}
}

//nolint:tparallel // the test is parallel, but test cases are not
func TestSendFromXRPLToCoreumCoreumOriginatedTokenWithDifferentSendingPrecision(t *testing.T) {
	t.Parallel()

	highMaxHoldingAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 2)
	coreumRecipient := chains.Coreum.GenAccount()

	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	tests := []struct {
		name                                       string
		sendingPrecision                           int32
		decimals                                   uint32
		sendingAmount                              sdkmath.Int
		maxHoldingAmount                           sdkmath.Int
		wantReceivedAmount                         sdkmath.Int
		wantIsAmountSentIsZeroAfterTruncationError bool
		xrplSendingAmount                          sdkmath.Int
	}{
		{
			name:               "positive_precision_no_truncation",
			sendingPrecision:   2,
			decimals:           6,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", 6),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", 6),
			xrplSendingAmount:  sdkmath.NewIntWithDecimal(999999999915, 13),
		},
		{
			name:               "positive_precision_with_truncation",
			sendingPrecision:   2,
			decimals:           20,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15567", 20),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15", 20),
			xrplSendingAmount:  sdkmath.NewIntWithDecimal(15, 13),
		},
		{
			name:              "positive_precision_low_amount",
			sendingPrecision:  2,
			decimals:          13,
			maxHoldingAmount:  highMaxHoldingAmount,
			sendingAmount:     integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.009999", 13),
			xrplSendingAmount: sdkmath.NewIntWithDecimal(9999, 8),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "zero_precision_no_truncation",
			sendingPrecision:   0,
			decimals:           11,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", 11),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", 11),
			xrplSendingAmount:  sdkmath.NewIntWithDecimal(9999999999, 15),
		},
		{
			name:               "zero_precision_with_truncation",
			sendingPrecision:   0,
			decimals:           1,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1.15567", 1),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 1),
			xrplSendingAmount:  sdkmath.NewIntWithDecimal(1, 15),
		},
		{
			name:              "zero_precision_low_amount",
			sendingPrecision:  0,
			decimals:          2,
			maxHoldingAmount:  highMaxHoldingAmount,
			sendingAmount:     integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.9999", 2),
			xrplSendingAmount: sdkmath.NewIntWithDecimal(9999, 11),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "negative_precision_no_truncation",
			sendingPrecision:   -2,
			decimals:           3,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", 3),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", 3),
			xrplSendingAmount:  sdkmath.NewIntWithDecimal(9999999900, 15),
		},
		{
			name:               "negative_precision_with_truncation",
			sendingPrecision:   -2,
			decimals:           20,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999.15567", 20),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9900", 20),
			xrplSendingAmount:  sdkmath.NewIntWithDecimal(9900, 15),
		},
		{
			name:              "negative_precision_low_amount",
			sendingPrecision:  -2,
			decimals:          6,
			maxHoldingAmount:  highMaxHoldingAmount,
			sendingAmount:     integrationtests.ConvertStringWithDecimalsToSDKInt(t, "99.9999", 6),
			xrplSendingAmount: sdkmath.NewIntWithDecimal(999999, 11),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// fund sender to cover registration fee and some coins on top for the contract calls
			coreumSenderAddress := chains.Coreum.GenAccount()
			chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
				Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 6)),
			})

			registeredCoreumOriginatedToken := issueAndRegisterCoreumOriginatedToken(
				ctx,
				t,
				contractClient,
				chains.Coreum,
				coreumSenderAddress,
				owner,
				tt.decimals,
				tt.maxHoldingAmount,
				tt.sendingPrecision,
				tt.maxHoldingAmount,
				sdkmath.ZeroInt(),
			)
			// if we expect an error the amount is invalid, so it won't be accepted from the coreum to XRPL
			if !tt.wantIsAmountSentIsZeroAfterTruncationError {
				coinToSend := sdk.NewCoin(registeredCoreumOriginatedToken.Denom, tt.sendingAmount)
				sendFromCoreumToXRPL(
					ctx, t, contractClient, relayers, coreumSenderAddress, coinToSend, xrpl.GenPrivKeyTxSigner().Account(),
				)
			}

			// create an evidence
			xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
				TxHash:    integrationtests.GenXRPLTxHash(t),
				Issuer:    bridgeXRPLAddress,
				Currency:  registeredCoreumOriginatedToken.XRPLCurrency,
				Amount:    tt.xrplSendingAmount,
				Recipient: coreumRecipient,
			}

			// call from all relayers
			for _, relayer := range relayers {
				_, err := contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence)
				if tt.wantIsAmountSentIsZeroAfterTruncationError {
					require.True(t, coreum.IsAmountSentIsZeroAfterTruncationError(err), err)
					return
				}
				require.NoError(t, err)
			}

			balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: coreumRecipient.String(),
				Denom:   registeredCoreumOriginatedToken.Denom,
			})
			require.NoError(t, err)
			require.Equal(t, tt.wantReceivedAmount.String(), balanceRes.Balance.Amount.String())
		})
	}
}

func TestSendFromCoreumToXRPLXRPLOriginatedToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	relayers := genRelayers(ctx, t, chains, 2)
	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	issuerAcc := chains.XRPL.GenAccount(ctx, t, 0)
	issuer := issuerAcc.String()
	currency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))
	sendingPrecision := int32(15)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 9)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 5)

	// register new token
	_, err := contractClient.RegisterXRPLToken(
		ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	// activate token
	registeredXRPLOriginatedToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	activateXRPLToken(ctx, t, contractClient, relayers, issuer, currency)

	amountToSendFromXRPLToCoreum := sdkmath.NewInt(1_000_100)
	sendFromXRPLToCoreum(
		ctx, t, contractClient, relayers, issuer, currency, amountToSendFromXRPLToCoreum, coreumSenderAddress,
	)
	// validate that the amount is received
	balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(t, amountToSendFromXRPLToCoreum.String(), balanceRes.Balance.Amount.String())

	amountToSend := sdkmath.NewIntWithDecimal(1, 6)

	// try to send more than account has
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSendFromXRPLToCoreum.AddRaw(1)),
		nil,
	)
	require.ErrorContains(t, err, cosmoserrors.ErrInsufficientFunds.Error())

	// try to send with invalid recipient
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		"invalid",
		sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSend),
		nil,
	)
	require.True(t, coreum.IsInvalidXRPLAddressError(err), err)

	// try to send with not registered token
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(chains.Coreum.ChainSettings.Denom, sdkmath.NewInt(1)),
		nil,
	)
	require.True(t, coreum.IsTokenNotRegisteredError(err), err)

	// send valid amount and validate the state
	coreumSenderBalanceBeforeRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSend),
		nil,
	)
	require.NoError(t, err)
	// check the remaining balance
	coreumSenderBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(
		t,
		coreumSenderBalanceBeforeRes.Balance.Amount.Sub(amountToSend).String(),
		coreumSenderBalanceAfterRes.Balance.Amount.String(),
	)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	require.Equal(t, operationType.Issuer, registeredXRPLOriginatedToken.Issuer)
	require.Equal(t, operationType.Currency, registeredXRPLOriginatedToken.Currency)
	require.Equal(t, operationType.Amount, amountToSend)
	require.Equal(t, operationType.Recipient, xrplRecipientAddress.String())

	acceptedTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            integrationtests.GenXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}

	// send from first relayer
	_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
		ctx,
		relayers[0].CoreumAddress,
		acceptedTxEvidence,
	)
	require.NoError(t, err)

	// send from second relayer
	_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
		ctx,
		relayers[1].CoreumAddress,
		acceptedTxEvidence,
	)
	require.NoError(t, err)

	// check pending operations
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// use all available tickets
	tickets, err := contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	for range len(tickets) - 1 {
		_, err = contractClient.SendToXRPL(
			ctx,
			coreumSenderAddress,
			xrplRecipientAddress.String(),
			sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, sdkmath.NewInt(1)),
			nil,
		)
		require.NoError(t, err)
	}

	// try to use last (protected) ticket
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, sdkmath.NewInt(1)),
		nil,
	)
	require.True(t, coreum.IsLastTicketReservedError(err))
}

//nolint:tparallel // the test is parallel, but test cases are not
func TestSendFromCoreumToXRPLXRPLOriginatedTokenWithDifferentSendingPrecision(t *testing.T) {
	t.Parallel()

	var (
		tokenDecimals        = int64(15)
		highMaxHoldingAmount = integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)
	)

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 2)
	xrplRecipient := xrpl.GenPrivKeyTxSigner().Account()

	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		50,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	tests := []struct {
		name                                       string
		sendingPrecision                           int32
		sendingAmount                              sdkmath.Int
		maxHoldingAmount                           sdkmath.Int
		wantReceivedAmount                         sdkmath.Int
		wantIsAmountSentIsZeroAfterTruncationError bool
	}{
		{
			name:               "positive_precision_no_truncation",
			sendingPrecision:   2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", tokenDecimals),
		},
		{
			name:               "positive_precision_with_truncation",
			sendingPrecision:   2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15567", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15", tokenDecimals),
		},
		{
			name:             "positive_precision_low_amount",
			sendingPrecision: 2,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.009999", tokenDecimals),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "zero_precision_no_truncation",
			sendingPrecision:   0,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", tokenDecimals),
		},
		{
			name:               "zero_precision_with_truncation",
			sendingPrecision:   0,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1.15567", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", tokenDecimals),
		},
		{
			name:             "zero_precision_low_amount",
			sendingPrecision: 0,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.9999", tokenDecimals),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "negative_precision_no_truncation",
			sendingPrecision:   -2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", tokenDecimals),
		},
		{
			name:               "negative_precision_with_truncation",
			sendingPrecision:   -2,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999.15567", tokenDecimals),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9900", tokenDecimals),
		},
		{
			name:             "negative_precision_low_amount",
			sendingPrecision: -2,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "99.9999", tokenDecimals),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// fund owner to cover registration fee
			chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
				Amount: issueFee.Amount,
			})

			issuerAcc := xrpl.GenPrivKeyTxSigner().Account()
			issuer := issuerAcc.String()
			currency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))

			// register from the owner
			_, err := contractClient.RegisterXRPLToken(
				ctx, owner, issuer, currency, tt.sendingPrecision, tt.maxHoldingAmount, sdkmath.ZeroInt(),
			)
			require.NoError(t, err)
			registeredXRPLToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
			require.NoError(t, err)

			// activate token
			activateXRPLToken(ctx, t, contractClient, relayers, issuerAcc.String(), currency)

			coreumSenderAddress := chains.Coreum.GenAccount()
			// fund coreum sender address to cover fee
			chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
				Amount: sdkmath.NewIntWithDecimal(1, 6),
			})
			sendFromXRPLToCoreum(ctx, t, contractClient, relayers, issuer, currency, tt.maxHoldingAmount, coreumSenderAddress)
			coreumSenderBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: coreumSenderAddress.String(),
				Denom:   registeredXRPLToken.CoreumDenom,
			})
			require.NoError(t, err)
			require.Equal(t, tt.maxHoldingAmount.String(), coreumSenderBalanceRes.Balance.Amount.String())

			_, err = contractClient.SendToXRPL(
				ctx,
				coreumSenderAddress,
				xrplRecipient.String(),
				sdk.NewCoin(registeredXRPLToken.CoreumDenom, tt.sendingAmount),
				nil,
			)
			if tt.wantIsAmountSentIsZeroAfterTruncationError {
				require.True(t, coreum.IsAmountSentIsZeroAfterTruncationError(err), err)
				return
			}
			require.NoError(t, err)

			pendingOperations, err := contractClient.GetPendingOperations(ctx)
			require.NoError(t, err)
			found := false
			for _, operation := range pendingOperations {
				operationType := operation.OperationType.CoreumToXRPLTransfer
				if operationType != nil && operationType.Issuer == issuer && operationType.Currency == currency {
					found = true
					require.Equal(t, tt.wantReceivedAmount.String(), operationType.Amount.String())
				}
			}
			require.True(t, found)
		})
	}
}

func TestSendFromCoreumToXRPLXRPToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 2)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	registeredXRPToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(
		ctx, xrpl.XRPTokenIssuer.String(), xrpl.ConvertCurrencyToString(xrpl.XRPTokenCurrency),
	)
	require.NoError(t, err)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 5)

	amountToSendFromXRPLToCoreum := sdkmath.NewInt(1_000_100)
	sendFromXRPLToCoreum(
		ctx,
		t,
		contractClient,
		relayers,
		registeredXRPToken.Issuer,
		registeredXRPToken.Currency,
		amountToSendFromXRPLToCoreum,
		coreumSenderAddress,
	)
	// validate that the amount is received
	balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(t, amountToSendFromXRPLToCoreum.String(), balanceRes.Balance.Amount.String())

	amountToSend := sdkmath.NewIntWithDecimal(1, 6)

	// send valid amount and validate the state
	coreumSenderBalanceBeforeRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPToken.CoreumDenom,
	})
	require.NoError(t, err)
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPToken.CoreumDenom, amountToSend),
		nil,
	)
	require.NoError(t, err)
	// check the remaining balance
	coreumSenderBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(
		t,
		coreumSenderBalanceBeforeRes.Balance.Amount.Sub(amountToSend).String(),
		coreumSenderBalanceAfterRes.Balance.Amount.String(),
	)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	require.Equal(t, operationType.Issuer, registeredXRPToken.Issuer)
	require.Equal(t, operationType.Currency, registeredXRPToken.Currency)
	require.Equal(t, operationType.Amount, amountToSend)
	require.Equal(t, operationType.Recipient, xrplRecipientAddress.String())

	acceptedTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            integrationtests.GenXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}

	// send from first relayer
	_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
		ctx, relayers[0].CoreumAddress, acceptedTxEvidence,
	)
	require.NoError(t, err)

	// send from second relayer
	_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
		ctx, relayers[1].CoreumAddress, acceptedTxEvidence,
	)
	require.NoError(t, err)

	// check pending operations
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)
}

func TestSendFromCoreumXRPLOriginatedTokenWithDeliverAmount(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)
	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	relayers := genRelayers(ctx, t, chains, 2)
	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	sendingPrecision := int32(3)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 30)
	issuer := chains.XRPL.GenAccount(ctx, t, 0).String()
	currency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))
	bridgingFee := sdkmath.NewInt(10)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 15)

	// register new XRPL token and test sending

	_, err := contractClient.RegisterXRPLToken(
		ctx, owner, issuer, currency, sendingPrecision, maxHoldingAmount, bridgingFee,
	)
	require.NoError(t, err)

	registeredXRPLOriginatedToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, currency)
	require.NoError(t, err)
	activateXRPLToken(ctx, t, contractClient, relayers, issuer, currency)

	xrplTokenAmountToSendFromXRPLToCoreum := sdkmath.NewIntWithDecimal(1, 15).Add(bridgingFee)
	sendFromXRPLToCoreum(
		ctx, t, contractClient, relayers, issuer, currency, xrplTokenAmountToSendFromXRPLToCoreum, coreumSenderAddress,
	)
	// validate that the XRPL token amount is received
	coreumSenderBalance, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(
		t, xrplTokenAmountToSendFromXRPLToCoreum.Sub(bridgingFee).String(), coreumSenderBalance.Balance.Amount.String(),
	)

	amountToSendBack := sdkmath.NewIntWithDecimal(1, 14)

	// try to send amount greater than max amount
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSendBack),
		lo.ToPtr(amountToSendBack.AddRaw(1000)),
	)
	require.True(t, coreum.IsInvalidDeliverAmountError(err), err)

	coreumSenderBalanceBeforeRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)

	// send amount equal to max amount
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSendBack.Add(bridgingFee)),
		lo.ToPtr(amountToSendBack),
	)
	require.NoError(t, err)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	require.Equal(t, coreum.OperationTypeCoreumToXRPLTransfer{
		Issuer:   issuer,
		Currency: currency,
		// the amounts are truncated
		Amount:    amountToSendBack,
		MaxAmount: &amountToSendBack,
		Recipient: xrplRecipientAddress.String(),
	}, *operationType)

	// reject the operation
	rejectedTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            integrationtests.GenXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultRejected,
		},
	}
	for _, relayer := range relayers {
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
			ctx, relayer.CoreumAddress, rejectedTxEvidence,
		)
		require.NoError(t, err)
	}

	// claim refunds
	pendingRefunds, err := contractClient.GetPendingRefunds(ctx, coreumSenderAddress)
	require.NoError(t, err)
	require.Len(t, pendingRefunds, 1)
	require.Equal(t, pendingRefunds[0].XRPLTxHash, rejectedTxEvidence.TxHash)
	_, err = contractClient.ClaimRefund(ctx, coreumSenderAddress, pendingRefunds[0].ID)
	require.NoError(t, err)

	coreumSenderBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)

	// check that only the bridging fee is charged
	require.Equal(
		t,
		bridgingFee.String(),
		coreumSenderBalanceBeforeRes.Balance.Amount.Sub(coreumSenderBalanceAfterRes.Balance.Amount).String(),
	)

	coreumSenderBalanceBeforeRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)

	// send amount with low deliver amount
	deliverAmount := amountToSendBack.QuoRaw(2)
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredXRPLOriginatedToken.CoreumDenom, amountToSendBack.Add(bridgingFee)),
		&deliverAmount,
	)
	require.NoError(t, err)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation = pendingOperations[0]
	operationType = operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	require.Equal(t, coreum.OperationTypeCoreumToXRPLTransfer{
		Issuer:   issuer,
		Currency: currency,
		// the amounts are truncated
		Amount:    deliverAmount,
		MaxAmount: &amountToSendBack,
		Recipient: xrplRecipientAddress.String(),
	}, *operationType)

	// accep the operation
	acceptedTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            integrationtests.GenXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}
	for _, relayer := range relayers {
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
			ctx, relayer.CoreumAddress, acceptedTxEvidence,
		)
		require.NoError(t, err)
	}

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	coreumSenderBalanceAfterRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredXRPLOriginatedToken.CoreumDenom,
	})
	require.NoError(t, err)

	// check that changed full amount plus bridging fee
	require.Equal(
		t,
		coreumSenderBalanceBeforeRes.Balance.Amount.Sub(amountToSendBack).Sub(bridgingFee).String(),
		coreumSenderBalanceAfterRes.Balance.Amount.String(),
	)
}

func TestSendFromCoreumCoreumOriginatedTokenWithDeliverAmount(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 6)),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)
	relayers := genRelayers(ctx, t, chains, 2)
	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		xrpl.GenPrivKeyTxSigner().Account().String(),
		10,
	)

	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 15)

	tokenDecimals := uint32(6)
	sendingPrecision := int32(3)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 9)
	initialAmount := sdkmath.NewIntWithDecimal(1, 9)
	registeredCoreumOriginatedToken := issueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		contractClient,
		chains.Coreum,
		coreumSenderAddress,
		owner,
		tokenDecimals,
		initialAmount,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)

	amountToSend := sdkmath.NewInt(1000)
	// try to send with deliverAmount
	_, err := contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, amountToSend),
		lo.ToPtr(amountToSend),
	)
	require.True(t, coreum.IsDeliverAmountIsProhibitedError(err), err)
}

func TestSendFromCoreumToXRPLCoreumOriginatedToken(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.MulRaw(2).Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	relayers := genRelayers(ctx, t, chains, 2)
	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 10)

	// issue asset ft and register it
	sendingPrecision1 := int32(5)
	tokenDecimals1 := uint32(5)
	maxHoldingAmount1 := sdkmath.NewIntWithDecimal(1, 11)
	initialAmount := sdkmath.NewIntWithDecimal(1, 11)
	registeredCoreumOriginatedToken1 := issueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		contractClient,
		chains.Coreum,
		coreumSenderAddress,
		owner,
		tokenDecimals1,
		initialAmount,
		sendingPrecision1,
		maxHoldingAmount1,
		sdkmath.ZeroInt(),
	)

	// register coreum (udevcore) denom
	denom2 := chains.Coreum.ChainSettings.Denom
	sendingPrecision2 := int32(6)
	tokenDecimals2 := uint32(6)
	maxHoldingAmount2 := sdkmath.NewIntWithDecimal(1, 9)
	_, err := contractClient.RegisterCoreumToken(
		ctx, owner, denom2, tokenDecimals2, sendingPrecision2, maxHoldingAmount2, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	registeredCoreumOriginatedToken2, err := contractClient.GetCoreumTokenByDenom(ctx, denom2)
	require.NoError(t, err)

	// issue asset ft but not register it
	issueMsg := &assetfttypes.MsgIssue{
		Issuer:        coreumSenderAddress.String(),
		Symbol:        "notreg",
		Subunit:       "notreg",
		Precision:     uint32(16), // token decimals in terms of the contract
		InitialAmount: sdkmath.NewIntWithDecimal(1, 11),
	}
	_, err = client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumSenderAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		issueMsg,
	)
	require.NoError(t, err)
	notRegisteredTokenDenom := assetfttypes.BuildDenom(issueMsg.Subunit, coreumSenderAddress)

	// try to send with not registered token
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(notRegisteredTokenDenom, sdkmath.NewInt(1)),
		nil,
	)
	require.True(t, coreum.IsTokenNotRegisteredError(err), err)

	// ********** test token1 (assetft) **********

	amountToSendOfToken1 := sdkmath.NewInt(1_001_001)

	// send valid amount and validate the state
	coreumSenderBalanceBeforeRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredCoreumOriginatedToken1.Denom,
	})
	require.NoError(t, err)
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredCoreumOriginatedToken1.Denom, amountToSendOfToken1),
		nil,
	)
	require.NoError(t, err)
	// check the remaining balance
	coreumSenderBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredCoreumOriginatedToken1.Denom,
	})
	require.NoError(t, err)
	require.Equal(
		t,
		coreumSenderBalanceBeforeRes.Balance.Amount.Sub(amountToSendOfToken1).String(),
		coreumSenderBalanceAfterRes.Balance.Amount.String(),
	)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	require.Equal(t, operationType.Issuer, bridgeXRPLAddress)
	require.Equal(t, operationType.Currency, registeredCoreumOriginatedToken1.XRPLCurrency)
	// XRPL DECIMALS (15) - TOKEN DECIMALS (5) = 10
	require.Equal(t, operationType.Amount, amountToSendOfToken1.Mul(sdkmath.NewInt(10_000_000_000)))
	require.Equal(t, operationType.Recipient, xrplRecipientAddress.String())

	acceptedTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            integrationtests.GenXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}
	// send from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
			ctx, relayer.CoreumAddress, acceptedTxEvidence,
		)
		require.NoError(t, err)
	}

	// check pending operations
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// ********** test token2 (udevcore) **********

	amountToSendOfToken2 := sdkmath.NewInt(1_002_001)

	// send valid amount and validate the state
	coreumSenderBalanceBeforeRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredCoreumOriginatedToken2.Denom,
	})
	require.NoError(t, err)
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredCoreumOriginatedToken2.Denom, amountToSendOfToken2),
		nil,
	)
	require.NoError(t, err)
	// check the remaining balance
	coreumSenderBalanceAfterRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredCoreumOriginatedToken2.Denom,
	})
	require.NoError(t, err)
	require.True(t, coreumSenderBalanceBeforeRes.Balance.Amount.
		Sub(amountToSendOfToken2).
		GT(coreumSenderBalanceAfterRes.Balance.Amount),
	)

	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation = pendingOperations[0]
	operationType = operation.OperationType.CoreumToXRPLTransfer
	require.NotNil(t, operationType)
	require.Equal(t, operationType.Issuer, bridgeXRPLAddress)
	require.Equal(t, operationType.Currency, registeredCoreumOriginatedToken2.XRPLCurrency)
	// XRPL DECIMALS (15) - TOKEN DECIMALS (6) = 9
	require.Equal(t, operationType.Amount, amountToSendOfToken2.Mul(sdkmath.NewIntWithDecimal(1, 9)))
	require.Equal(t, operationType.Recipient, xrplRecipientAddress.String())

	acceptedTxEvidence = coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            integrationtests.GenXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}

	// send from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
			ctx, relayer.CoreumAddress, acceptedTxEvidence,
		)
		require.NoError(t, err)
	}

	// check pending operations
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// ********** use all available tickets **********

	tickets, err := contractClient.GetAvailableTickets(ctx)
	require.NoError(t, err)
	for range len(tickets) - 1 {
		_, err = contractClient.SendToXRPL(
			ctx,
			coreumSenderAddress,
			xrplRecipientAddress.String(),
			sdk.NewCoin(registeredCoreumOriginatedToken1.Denom, sdkmath.NewInt(1)),
			nil,
		)
		require.NoError(t, err)
	}

	// try to use last (protected) ticket
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredCoreumOriginatedToken1.Denom, sdkmath.NewInt(1)),
		nil,
	)
	require.True(t, coreum.IsLastTicketReservedError(err))
}

func TestSendFromCoreumToXRPLProhibitedAddresses(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	coreumSenderAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.MulRaw(2).Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	xrplRecipientAddress := chains.XRPL.GenAccount(ctx, t, 0)

	relayers := genRelayers(ctx, t, chains, 2)
	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 10)

	prohibitedXRPLAddresses, err := contractClient.GetProhibitedXRPLAddresses(ctx)
	require.NoError(t, err)

	initialProhibitedAddresses := []string{
		"rrrrrrrrrrrrrrrrrrrrrhoLvTp",
		"rrrrrrrrrrrrrrrrrrrrBZbvji",
		"rHb9CJAWyB4rj91VRWn96DkukG4bwdtyTh",
		"rrrrrrrrrrrrrrrrrNAMEtxvNvQ",
		"rrrrrrrrrrrrrrrrrrrn5RM1rHd",
	}
	initialProhibitedAddresses = append(initialProhibitedAddresses, bridgeXRPLAddress)
	require.ElementsMatch(t, initialProhibitedAddresses, prohibitedXRPLAddresses)

	tokenDecimals := uint32(6)
	sendingPrecision := int32(6)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 9)
	initialAmount := sdkmath.NewIntWithDecimal(1, 9)
	registeredCoreumOriginatedToken := issueAndRegisterCoreumOriginatedToken(
		ctx,
		t,
		contractClient,
		chains.Coreum,
		coreumSenderAddress,
		owner,
		tokenDecimals,
		initialAmount,
		sendingPrecision,
		maxHoldingAmount,
		sdkmath.ZeroInt(),
	)

	for _, recipient := range prohibitedXRPLAddresses {
		_, err = contractClient.SendToXRPL(
			ctx,
			coreumSenderAddress,
			recipient,
			sdk.NewCoin(registeredCoreumOriginatedToken.Denom, sdkmath.NewInt(1)),
			nil,
		)
		require.True(t, coreum.IsProhibitedAddressError(err), err)
	}

	contractCfg, err := contractClient.GetContractConfig(ctx)
	require.NoError(t, err)

	// try to send to XRPL bridge account address
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		contractCfg.BridgeXRPLAddress,
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, sdkmath.NewInt(1)),
		nil,
	)
	require.True(t, coreum.IsProhibitedAddressError(err), err)

	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredCoreumOriginatedToken.Denom, sdkmath.NewInt(1)),
		nil,
	)
	require.NoError(t, err)
	//nolint:gocritic // append new item to old list for the assertion
	newProhibitedAddresses := append(initialProhibitedAddresses, xrplRecipientAddress.String())

	// try to update the prohibited addresses list from not owner
	_, err = contractClient.UpdateProhibitedXRPLAddresses(ctx, relayers[0].CoreumAddress, newProhibitedAddresses)
	require.True(t, coreum.IsUnauthorizedSenderError(err), err)

	// update form owner
	_, err = contractClient.UpdateProhibitedXRPLAddresses(ctx, owner, newProhibitedAddresses)
	require.NoError(t, err)

	prohibitedXRPLAddresses, err = contractClient.GetProhibitedXRPLAddresses(ctx)
	require.NoError(t, err)

	require.ElementsMatch(t, newProhibitedAddresses, prohibitedXRPLAddresses)
}

//nolint:tparallel // the test is parallel, but test cases are not
func TestSendFromCoreumToXRPLCoreumOriginatedTokenWithDifferentSendingPrecisionAndDecimals(t *testing.T) {
	t.Parallel()

	highMaxHoldingAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)
	ctx, chains := integrationtests.NewTestingContext(t)

	relayers := genRelayers(ctx, t, chains, 2)
	xrplRecipient := xrpl.GenPrivKeyTxSigner().Account()

	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		50,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	tests := []struct {
		name                                       string
		sendingPrecision                           int32
		decimals                                   uint32
		sendingAmount                              sdkmath.Int
		maxHoldingAmount                           sdkmath.Int
		wantReceivedAmount                         sdkmath.Int
		wantIsAmountSentIsZeroAfterTruncationError bool
		wantIsMaximumBridgedAmountReachedError     bool
	}{
		{
			name:               "positive_precision_no_truncation",
			sendingPrecision:   2,
			decimals:           6,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", 6),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999.15", 15),
		},
		{
			name:               "positive_precision_with_truncation",
			sendingPrecision:   2,
			decimals:           20,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15567", 20),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.15", 15),
		},
		{
			name:             "positive_precision_low_amount",
			sendingPrecision: 2,
			decimals:         13,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.009999", 13),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "zero_precision_no_truncation",
			sendingPrecision:   0,
			decimals:           11,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", 11),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999999", 15),
		},
		{
			name:               "zero_precision_with_truncation",
			sendingPrecision:   0,
			decimals:           1,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1.15567", 1),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 15),
		},
		{
			name:             "zero_precision_low_amount",
			sendingPrecision: 0,
			decimals:         2,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.9999", 2),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:               "negative_precision_no_truncation",
			sendingPrecision:   -2,
			decimals:           3,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", 3),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999999900", 15),
		},
		{
			name:               "negative_precision_with_truncation",
			sendingPrecision:   -2,
			decimals:           20,
			maxHoldingAmount:   highMaxHoldingAmount,
			sendingAmount:      integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999.15567", 20),
			wantReceivedAmount: integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9900", 15),
		},
		{
			name:             "negative_precision_low_amount",
			sendingPrecision: -2,
			decimals:         6,
			maxHoldingAmount: highMaxHoldingAmount,
			sendingAmount:    integrationtests.ConvertStringWithDecimalsToSDKInt(t, "99.9999", 6),
			wantIsAmountSentIsZeroAfterTruncationError: true,
		},
		{
			name:                                   "reached_max_holding_amount",
			sendingPrecision:                       2,
			decimals:                               8,
			maxHoldingAmount:                       integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999", 8),
			sendingAmount:                          integrationtests.ConvertStringWithDecimalsToSDKInt(t, "9999.01", 8),
			wantIsMaximumBridgedAmountReachedError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// fund sender to cover registration fee and some coins on top for the contract calls
			coreumSenderAddress := chains.Coreum.GenAccount()
			chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
				Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 6)),
			})

			registeredCoreumOriginatedToken := issueAndRegisterCoreumOriginatedToken(
				ctx,
				t,
				contractClient,
				chains.Coreum,
				coreumSenderAddress,
				owner,
				tt.decimals,
				tt.maxHoldingAmount.MulRaw(2), // twice more to be able to send more than max
				tt.sendingPrecision,
				tt.maxHoldingAmount,
				sdkmath.ZeroInt(),
			)

			_, err := contractClient.SendToXRPL(
				ctx,
				coreumSenderAddress,
				xrplRecipient.String(),
				sdk.NewCoin(registeredCoreumOriginatedToken.Denom, tt.sendingAmount),
				nil,
			)
			if tt.wantIsAmountSentIsZeroAfterTruncationError {
				require.True(t, coreum.IsAmountSentIsZeroAfterTruncationError(err), err)
				return
			}
			if tt.wantIsMaximumBridgedAmountReachedError {
				require.True(t, coreum.IsMaximumBridgedAmountReachedError(err), err)
				return
			}
			require.NoError(t, err)

			pendingOperations, err := contractClient.GetPendingOperations(ctx)
			require.NoError(t, err)
			found := false
			for _, operation := range pendingOperations {
				operationType := operation.OperationType.CoreumToXRPLTransfer
				if operationType != nil &&
					operationType.Issuer == bridgeXRPLAddress &&
					operationType.Currency == registeredCoreumOriginatedToken.XRPLCurrency {
					found = true
					require.Equal(t, tt.wantReceivedAmount.String(), operationType.Amount.String())
				}
			}
			require.True(t, found)
		})
	}
}

func TestSendCoreumOriginatedTokenWithBurningRateAndSendingCommissionFromCoreumToXRPLAndBack(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	coreumIssuerAddress := chains.Coreum.GenAccount()
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumIssuerAddress, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 7)),
	})

	coreumSenderAddress := chains.Coreum.GenAccount()
	chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
		Amount: sdkmath.NewIntWithDecimal(1, 6),
	})

	coreumRecipient := chains.Coreum.GenAccount()

	xrplRecipientAddress := xrpl.GenPrivKeyTxSigner().Account()
	relayers := genRelayers(ctx, t, chains, 2)
	bridgeXRPLAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		3,
		defaultTrustSetLimitAmount,
		bridgeXRPLAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 10)

	// issue asset ft and register it
	sendingPrecision := int32(5)
	tokenDecimals := uint32(5)
	maxHoldingAmount := sdkmath.NewIntWithDecimal(1, 11)
	msgIssue := &assetfttypes.MsgIssue{
		Issuer:             coreumIssuerAddress.String(),
		Symbol:             "symbol",
		Subunit:            "subunit",
		Precision:          tokenDecimals,
		InitialAmount:      maxHoldingAmount,
		BurnRate:           sdkmath.LegacyMustNewDecFromStr("0.1"),
		SendCommissionRate: sdkmath.LegacyMustNewDecFromStr("0.2"),
	}
	_, err := client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumIssuerAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		msgIssue,
	)
	require.NoError(t, err)
	denom := assetfttypes.BuildDenom(msgIssue.Subunit, coreumIssuerAddress)
	_, err = contractClient.RegisterCoreumToken(
		ctx, owner, denom, tokenDecimals, sendingPrecision, maxHoldingAmount, sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	registeredToken, err := contractClient.GetCoreumTokenByDenom(ctx, denom)
	require.NoError(t, err)

	// send coins to sender to test the commission

	msgSend := &banktypes.MsgSend{
		FromAddress: coreumIssuerAddress.String(),
		ToAddress:   coreumSenderAddress.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(denom, 10_000_000)),
	}
	_, err = client.BroadcastTx(
		ctx,
		chains.Coreum.ClientContext.WithFromAddress(coreumIssuerAddress),
		chains.Coreum.TxFactory().WithSimulateAndExecute(true),
		msgSend,
	)
	require.NoError(t, err)

	// ********** Coreum to XRPL **********

	amountToSend := sdkmath.NewIntWithDecimal(1, 6)

	// send the amount and validate the state
	coreumSenderBalanceBeforeRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredToken.Denom,
	})
	require.NoError(t, err)
	_, err = contractClient.SendToXRPL(
		ctx,
		coreumSenderAddress,
		xrplRecipientAddress.String(),
		sdk.NewCoin(registeredToken.Denom, amountToSend),
		nil,
	)
	require.NoError(t, err)
	// check the remaining balance
	coreumSenderBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumSenderAddress.String(),
		Denom:   registeredToken.Denom,
	})
	require.NoError(t, err)
	// amountToSend + burning rate + sending commission
	spentAmount := amountToSend.
		Add(amountToSend.ToLegacyDec().Mul(msgIssue.BurnRate.Add(msgIssue.SendCommissionRate)).TruncateInt())
	require.Equal(
		t,
		coreumSenderBalanceBeforeRes.Balance.Amount.Sub(spentAmount).String(),
		coreumSenderBalanceAfterRes.Balance.Amount.String(),
	)

	pendingOperations, err := contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Len(t, pendingOperations, 1)
	operation := pendingOperations[0]
	operationType := operation.OperationType.CoreumToXRPLTransfer
	// XRPL DECIMALS (15) - TOKEN DECIMALS (5) = 10
	require.Equal(t, operationType.Amount, amountToSend.Mul(sdkmath.NewInt(10_000_000_000)))
	require.Equal(t, operationType.Recipient, xrplRecipientAddress.String())

	acceptedTxEvidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            integrationtests.GenXRPLTxHash(t),
			TicketSequence:    &operation.TicketSequence,
			TransactionResult: coreum.TransactionResultAccepted,
		},
	}
	// send from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
			ctx, relayer.CoreumAddress, acceptedTxEvidence,
		)
		require.NoError(t, err)
	}

	// check pending operations
	pendingOperations, err = contractClient.GetPendingOperations(ctx)
	require.NoError(t, err)
	require.Empty(t, pendingOperations)

	// ********** XRPL to Coreum **********

	// XRPL DECIMALS (15) - TOKEN DECIMALS (5) = 10
	amountToSendBack := amountToSend.Mul(sdkmath.NewIntWithDecimal(1, 10))

	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    integrationtests.GenXRPLTxHash(t),
		Issuer:    bridgeXRPLAddress,
		Currency:  registeredToken.XRPLCurrency,
		Amount:    amountToSendBack,
		Recipient: coreumRecipient,
	}

	bridgeContractBalanceBeforeRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: contractClient.GetContractAddress().String(),
		Denom:   registeredToken.Denom,
	})
	require.NoError(t, err)

	// send from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SendXRPLToCoreumTransferEvidence(
			ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence,
		)
		require.NoError(t, err)
	}

	// check the remaining balance
	bridgeContractBalanceAfterRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: contractClient.GetContractAddress().String(),
		Denom:   registeredToken.Denom,
	})
	require.NoError(t, err)
	require.Equal(
		t,
		bridgeContractBalanceBeforeRes.Balance.Amount.Sub(amountToSend).String(),
		bridgeContractBalanceAfterRes.Balance.Amount.String(),
	)
	require.Equal(t, sdkmath.ZeroInt().String(), bridgeContractBalanceAfterRes.Balance.Amount.String())

	coreumRecipientBalanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredToken.Denom,
	})
	require.NoError(t, err)
	require.Equal(
		t,
		coreumRecipientBalanceRes.Balance.Amount.String(),
		bridgeContractBalanceBeforeRes.Balance.Amount.String(),
	)
}

// TestBridgingFeeForCoreumOriginatedTokens tests that corrects fees are calculated, deducted and
// are collected by relayers.
//
//nolint:tparallel // the test is parallel, but test cases are not
func TestBridgingFeeForCoreumOriginatedTokens(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	maxHoldingAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)
	tokenDecimals := uint32(15)
	relayers := genRelayers(ctx, t, chains, 2)
	bridgeAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		bridgeAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	type testCase struct {
		name             string
		bridgingFee      string
		sendingPrecision int32
		sendingAmount    string
		receivedAmount   string
		collectedFee     string
	}

	tests := []testCase{
		{
			name:             "zero_bridging_fee",
			sendingPrecision: 2,
			bridgingFee:      "0",
			sendingAmount:    "9999999999.15",
			receivedAmount:   "9999999999.15",
			collectedFee:     "0",
		},
		{
			name:             "4_bridging_fee",
			sendingPrecision: 2,
			bridgingFee:      "4",
			sendingAmount:    "1004",
			receivedAmount:   "1000",
			collectedFee:     "4",
		},
		{
			name:             "bridge_fee_with_precision",
			sendingPrecision: 2,
			bridgingFee:      "0.04",
			sendingAmount:    "999.96",
			receivedAmount:   "999.92",
			collectedFee:     "0.04",
		},
		{
			name:             "bridge_fee_with_precision_and_truncation",
			sendingPrecision: 2,
			bridgingFee:      "0.04",
			sendingAmount:    "1000.127",
			receivedAmount:   "1000.08",
			collectedFee:     "0.047",
		},
		{
			name:             "bridge_fee_less_than_sending_precision",
			sendingPrecision: 1,
			bridgingFee:      "0.00001",
			sendingAmount:    "999.9",
			receivedAmount:   "999.8",
			collectedFee:     "0.1",
		},
	}

	stringToSDKInt := func(stringValue string) sdkmath.Int {
		return integrationtests.ConvertStringWithDecimalsToSDKInt(t, stringValue, int64(tokenDecimals))
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// fund sender to cover registration fee and some coins on top for the contract calls
			coreumSenderAddress := chains.Coreum.GenAccount()
			chains.Coreum.FundAccountWithOptions(ctx, t, coreumSenderAddress, coreumintegration.BalancesOptions{
				Amount: issueFee.Amount.Add(sdkmath.NewIntWithDecimal(1, 6)),
			})

			registeredCoreumOriginatedToken := issueAndRegisterCoreumOriginatedToken(
				ctx,
				t,
				contractClient,
				chains.Coreum,
				coreumSenderAddress,
				owner,
				tokenDecimals,
				maxHoldingAmount.MulRaw(2), // twice more to be able to send more than max,
				tt.sendingPrecision,
				maxHoldingAmount,
				stringToSDKInt(tt.bridgingFee),
			)

			xrplRecipient := xrpl.GenPrivKeyTxSigner().Account()
			_, err := contractClient.SendToXRPL(
				ctx,
				coreumSenderAddress,
				xrplRecipient.String(),
				sdk.NewCoin(registeredCoreumOriginatedToken.Denom, stringToSDKInt(tt.sendingAmount)),
				nil,
			)
			require.NoError(t, err)

			pendingOperations, err := contractClient.GetPendingOperations(ctx)
			require.NoError(t, err)
			found := false
			for _, operation := range pendingOperations {
				operationType := operation.OperationType.CoreumToXRPLTransfer
				if operationType != nil &&
					operationType.Issuer == bridgeAddress &&
					operationType.Currency == registeredCoreumOriginatedToken.XRPLCurrency {
					found = true
					require.Equal(t, stringToSDKInt(tt.receivedAmount).String(), operationType.Amount.String())
				}
			}
			require.True(t, found)

			// assert fee collection
			claimFeesAndMakeAssertions(
				ctx,
				t,
				contractClient,
				bankClient,
				relayers,
				stringToSDKInt(tt.collectedFee),
				sdkmath.ZeroInt(),
				registeredCoreumOriginatedToken.Denom,
			)
		})
	}
}

// TestFeeCalculations_MultipleAssetsAndPartialClaim tests that corrects fees are calculated, deducted and
// are collected by relayers.
func TestFeeCalculations_MultipleAssetsAndPartialClaim(t *testing.T) {
	t.Parallel()

	var (
		sendingAmount    = sdkmath.NewIntWithDecimal(1, 6)
		maxHoldingAmount = sdkmath.NewIntWithDecimal(1, 9)
	)

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 3)
	bridgeAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		bridgeAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	assets := []struct {
		name            string
		bridgingFee     sdkmath.Int
		registeredToken coreum.XRPLToken
	}{
		{
			name:        "asset 1",
			bridgingFee: sdkmath.NewInt(4000),
		},
		{
			name:        "asset 2",
			bridgingFee: sdkmath.NewInt(4500),
		},
		{
			name:        "asset 3",
			bridgingFee: sdkmath.NewInt(2221),
		},
	}

	for index, asset := range assets {
		// fund owner to cover registration fee
		chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
			Amount: issueFee.Amount,
		})

		issuerAcc := xrpl.GenPrivKeyTxSigner().Account()
		issuer := issuerAcc.String()
		xrplCurrency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))

		// register from the owner
		_, err := contractClient.RegisterXRPLToken(
			ctx,
			owner,
			issuer,
			xrplCurrency,
			15,
			maxHoldingAmount,
			asset.bridgingFee,
		)
		require.NoError(t, err)
		registeredXRPLToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, xrplCurrency)
		require.NoError(t, err)
		assets[index].registeredToken = registeredXRPLToken

		// activate token
		activateXRPLToken(ctx, t, contractClient, relayers, issuerAcc.String(), xrplCurrency)

		// create an evidence
		coreumRecipient := chains.Coreum.GenAccount()
		xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
			TxHash:    integrationtests.GenXRPLTxHash(t),
			Issuer:    issuerAcc.String(),
			Currency:  xrplCurrency,
			Amount:    sendingAmount,
			Recipient: coreumRecipient,
		}

		// call from all relayers
		for _, relayer := range relayers {
			_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence)
			require.NoError(t, err)
		}

		balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
			Address: coreumRecipient.String(),
			Denom:   registeredXRPLToken.CoreumDenom,
		})
		require.NoError(t, err)
		require.Equal(t, sendingAmount.Sub(asset.bridgingFee).String(), balanceRes.Balance.Amount.String())
	}

	// assert fees are calculated correctly
	for _, relayer := range relayers {
		fees, err := contractClient.GetFeesCollected(ctx, relayer.CoreumAddress)
		require.NoError(t, err)
		require.Len(t, fees, len(assets))
		for _, fee := range fees {
			found := false
			for _, asset := range assets {
				if asset.registeredToken.CoreumDenom == fee.Denom {
					found = true
					assert.EqualValues(t, fee.Amount.String(), asset.bridgingFee.Quo(sdkmath.NewInt(int64(len(relayers)))).String())
					break
				}
			}

			require.True(t, found)
		}
	}

	// partial fee claiming
	for _, relayer := range relayers {
		initialFees, err := contractClient.GetFeesCollected(ctx, relayer.CoreumAddress)
		require.NoError(t, err)

		// claim one third of the fees
		oneThirdOfFees := initialFees.QuoInt(sdkmath.NewInt(3))
		_, err = contractClient.ClaimRelayerFees(ctx, relayer.CoreumAddress, oneThirdOfFees)
		require.NoError(t, err)
		allBalances, err := bankClient.AllBalances(ctx, &banktypes.QueryAllBalancesRequest{
			Address: relayer.CoreumAddress.String(),
		})
		require.NoError(t, err)
		for _, coin := range oneThirdOfFees {
			require.Equal(t, coin.Amount.String(), allBalances.Balances.AmountOf(coin.Denom).String())
		}

		// assert remainder is correct
		remainderFees := initialFees.Sub(oneThirdOfFees...)
		fees, err := contractClient.GetFeesCollected(ctx, relayer.CoreumAddress)
		require.NoError(t, err)
		require.EqualValues(t, remainderFees.String(), fees.String())

		// claim remainder of fees
		_, err = contractClient.ClaimRelayerFees(ctx, relayer.CoreumAddress, remainderFees)
		require.NoError(t, err)

		allBalances, err = bankClient.AllBalances(ctx, &banktypes.QueryAllBalancesRequest{
			Address: relayer.CoreumAddress.String(),
		})
		require.NoError(t, err)
		for _, coin := range initialFees {
			require.Equal(t, coin.Amount.String(), allBalances.Balances.AmountOf(coin.Denom).String())
		}

		// assert no fees are remaining
		fees, err = contractClient.GetFeesCollected(ctx, relayer.CoreumAddress)
		require.NoError(t, err)
		require.Empty(t, fees)
	}
}

// TestFeeCalculations_MultipleAssetsAndPartialClaim tests that corrects remainder fees are calculated, deducted and
// are collected by relayers.
func TestFeeCalculations_FeeRemainder(t *testing.T) {
	t.Parallel()

	maxHoldingAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 2)
	bridgeAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		bridgeAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	// fund owner to cover registration fee
	chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
		Amount: issueFee.Amount,
	})

	issuerAcc := xrpl.GenPrivKeyTxSigner().Account()
	issuer := issuerAcc.String()
	xrplCurrency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))

	tokenDecimals := int64(15)
	// register from the owner
	bridgingFee := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.3", tokenDecimals)
	_, err := contractClient.RegisterXRPLToken(
		ctx,
		owner,
		issuer,
		xrplCurrency,
		1,
		maxHoldingAmount,
		bridgingFee,
	)
	require.NoError(t, err)
	registeredXRPLToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, xrplCurrency)
	require.NoError(t, err)

	// activate token
	activateXRPLToken(ctx, t, contractClient, relayers, issuerAcc.String(), xrplCurrency)

	// create an evidence
	sendingAmount := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1.36", tokenDecimals)
	remainder := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.06", tokenDecimals)
	coreumRecipient := chains.Coreum.GenAccount()
	xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    integrationtests.GenXRPLTxHash(t),
		Issuer:    issuerAcc.String(),
		Currency:  xrplCurrency,
		Amount:    sendingAmount,
		Recipient: coreumRecipient,
	}

	// call from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence)
		require.NoError(t, err)
	}

	balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredXRPLToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(t, sendingAmount.Sub(bridgingFee).Sub(remainder).String(), balanceRes.Balance.Amount.String())

	// assert fees are calculated correctly
	claimFeesAndMakeAssertions(
		ctx,
		t,
		contractClient,
		bankClient,
		relayers,
		bridgingFee,
		remainder,
		registeredXRPLToken.CoreumDenom,
	)

	// send the amount again
	xrplToCoreumTransferEvidence.TxHash = integrationtests.GenXRPLTxHash(t)
	xrplToCoreumTransferEvidence.Recipient = coreum.GenAccount()

	// call from all relayers
	for _, relayer := range relayers {
		_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence)
		require.NoError(t, err)
	}

	balanceRes, err = bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: coreumRecipient.String(),
		Denom:   registeredXRPLToken.CoreumDenom,
	})
	require.NoError(t, err)
	require.Equal(t, sendingAmount.Sub(bridgingFee).Sub(remainder).String(), balanceRes.Balance.Amount.String())

	// assert fees are calculated correctly
	bridgingFeeWithRemainder := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.4", tokenDecimals)
	newRemainder := integrationtests.ConvertStringWithDecimalsToSDKInt(t, "0.02", tokenDecimals)
	claimFeesAndMakeAssertions(
		ctx,
		t,
		contractClient,
		bankClient,
		relayers,
		bridgingFeeWithRemainder,
		newRemainder,
		registeredXRPLToken.CoreumDenom,
	)
}

// TestBridgingFeeForXRPLOrginatedTokens tests that corrects fees are calculated, deducted and
// are collected by relayers.
//
//nolint:tparallel // the test is parallel, but test cases are not
func TestBridgingFeeForXRPLOrginatedTokens(t *testing.T) {
	t.Parallel()

	var (
		tokenDecimals        = int64(15)
		highMaxHoldingAmount = integrationtests.ConvertStringWithDecimalsToSDKInt(t, "1", 30)
	)

	ctx, chains := integrationtests.NewTestingContext(t)
	bankClient := banktypes.NewQueryClient(chains.Coreum.ClientContext)

	relayers := genRelayers(ctx, t, chains, 2)
	bridgeAddress := xrpl.GenPrivKeyTxSigner().Account().String()
	owner, contractClient := integrationtests.DeployInstantiateAndMigrateContract(
		ctx,
		t,
		chains,
		relayers,
		uint32(len(relayers)),
		10,
		defaultTrustSetLimitAmount,
		bridgeAddress,
		10,
	)
	// recover tickets to be able to create operations from coreum to XRPL
	recoverTickets(ctx, t, contractClient, owner, relayers, 100)

	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	type testCase struct {
		name                    string
		bridgingFee             string
		sendingPrecision        int32
		sendingAmountFromXRPL   string
		receivedOnCoreum        string
		collectedFeeFromXRPL    string
		sendingAmountFromCoreum string
		receivedOnXRPL          string
		collectedFeeFromCoreum  string
		expectErrorXRPL         bool
		expectErrorSendToCoreum bool
	}

	tests := []testCase{
		{
			name:                    "zero_bridge_fee",
			sendingPrecision:        2,
			bridgingFee:             "0",
			sendingAmountFromXRPL:   "9999999999.15",
			receivedOnCoreum:        "9999999999.15",
			collectedFeeFromXRPL:    "0",
			sendingAmountFromCoreum: "9999999999.15",
			receivedOnXRPL:          "9999999999.15",
			collectedFeeFromCoreum:  "0",
		},
		{
			name:                    "4_bridge_fee",
			sendingPrecision:        2,
			bridgingFee:             "4",
			sendingAmountFromXRPL:   "1008",
			receivedOnCoreum:        "1004",
			collectedFeeFromXRPL:    "4",
			sendingAmountFromCoreum: "1004",
			receivedOnXRPL:          "1000",
			collectedFeeFromCoreum:  "4",
		},
		{
			name:                    "bridge_fee_with_precision",
			sendingPrecision:        2,
			bridgingFee:             "0.04",
			sendingAmountFromXRPL:   "1000",
			receivedOnCoreum:        "999.96",
			collectedFeeFromXRPL:    "0.04",
			sendingAmountFromCoreum: "999.96",
			receivedOnXRPL:          "999.92",
			collectedFeeFromCoreum:  "0.04",
		},
		{
			name:                    "bridge_fee_with_precision_and_truncation",
			sendingPrecision:        2,
			bridgingFee:             "0.04",
			sendingAmountFromXRPL:   "1000.222",
			receivedOnCoreum:        "1000.18",
			collectedFeeFromXRPL:    "0.042",
			sendingAmountFromCoreum: "1000.127",
			receivedOnXRPL:          "1000.08",
			collectedFeeFromCoreum:  "0.047",
		},
		{
			name:                    "bridge_fee_less_than_sending_precision",
			sendingPrecision:        1,
			bridgingFee:             "0.00001",
			sendingAmountFromXRPL:   "1000",
			receivedOnCoreum:        "999.9",
			collectedFeeFromXRPL:    "0.1",
			sendingAmountFromCoreum: "999.9",
			receivedOnXRPL:          "999.8",
			collectedFeeFromCoreum:  "0.1",
		},
		{
			name:                    "low_amount_send_from_xrpl",
			sendingPrecision:        2,
			bridgingFee:             "0.001",
			sendingAmountFromXRPL:   "0.01",
			expectErrorSendToCoreum: true,
		},
		{
			name:                    "low_amount_send_from_coreum",
			sendingPrecision:        2,
			bridgingFee:             "0.001",
			sendingAmountFromXRPL:   "1",
			receivedOnCoreum:        "0.99",
			collectedFeeFromXRPL:    "0.01",
			sendingAmountFromCoreum: "0.01",
			expectErrorXRPL:         true,
		},
	}

	stringToSDKInt := func(stringValue string) sdkmath.Int {
		return integrationtests.ConvertStringWithDecimalsToSDKInt(t, stringValue, tokenDecimals)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// fund owner to cover registration fee
			chains.Coreum.FundAccountWithOptions(ctx, t, owner, coreumintegration.BalancesOptions{
				Amount: issueFee.Amount,
			})

			issuerAcc := xrpl.GenPrivKeyTxSigner().Account()
			issuer := issuerAcc.String()
			xrplCurrency := xrpl.ConvertCurrencyToString(integrationtests.GenerateXRPLCurrency(t))

			// register from the owner
			_, err := contractClient.RegisterXRPLToken(
				ctx,
				owner,
				issuer,
				xrplCurrency,
				tt.sendingPrecision,
				highMaxHoldingAmount,
				stringToSDKInt(tt.bridgingFee),
			)
			require.NoError(t, err)
			registeredXRPLToken, err := contractClient.GetXRPLTokenByIssuerAndCurrency(ctx, issuer, xrplCurrency)
			require.NoError(t, err)

			// activate token
			activateXRPLToken(ctx, t, contractClient, relayers, issuerAcc.String(), xrplCurrency)

			// create an evidence
			coreumRecipient := chains.Coreum.GenAccount()
			xrplToCoreumTransferEvidence := coreum.XRPLToCoreumTransferEvidence{
				TxHash:    integrationtests.GenXRPLTxHash(t),
				Issuer:    issuerAcc.String(),
				Currency:  xrplCurrency,
				Amount:    stringToSDKInt(tt.sendingAmountFromXRPL),
				Recipient: coreumRecipient,
			}

			// call from all relayers
			for _, relayer := range relayers {
				_, err = contractClient.SendXRPLToCoreumTransferEvidence(ctx, relayer.CoreumAddress, xrplToCoreumTransferEvidence)
				if tt.expectErrorSendToCoreum {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
			}

			balanceRes, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: coreumRecipient.String(),
				Denom:   registeredXRPLToken.CoreumDenom,
			})
			require.NoError(t, err)
			require.Equal(t, stringToSDKInt(tt.receivedOnCoreum).String(), balanceRes.Balance.Amount.String())

			// assert fee collection
			claimFeesAndMakeAssertions(
				ctx,
				t,
				contractClient,
				bankClient,
				relayers,
				stringToSDKInt(tt.collectedFeeFromXRPL),
				sdkmath.ZeroInt(),
				registeredXRPLToken.CoreumDenom,
			)

			// send back to xrpl
			chains.Coreum.FundAccountWithOptions(ctx, t, coreumRecipient, coreumintegration.BalancesOptions{
				Amount: sdkmath.NewInt(1_000_000),
			})
			xrplRecipient := xrpl.GenPrivKeyTxSigner().Account()
			_, err = contractClient.SendToXRPL(
				ctx,
				coreumRecipient,
				xrplRecipient.String(),
				sdk.NewCoin(
					registeredXRPLToken.CoreumDenom,
					stringToSDKInt(tt.sendingAmountFromCoreum),
				),
				nil,
			)
			if tt.expectErrorXRPL {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			pendingOperations, err := contractClient.GetPendingOperations(ctx)
			require.NoError(t, err)
			found := false
			for _, operation := range pendingOperations {
				operationType := operation.OperationType.CoreumToXRPLTransfer
				if operationType != nil &&
					operationType.Issuer == issuerAcc.String() &&
					operationType.Currency == xrplCurrency {
					found = true
					require.Equal(t, stringToSDKInt(tt.receivedOnXRPL).String(), operationType.Amount.String())
				}
			}
			require.True(t, found)

			// assert fees again for bridging back
			claimFeesAndMakeAssertions(
				ctx,
				t,
				contractClient,
				bankClient,
				relayers,
				stringToSDKInt(tt.collectedFeeFromCoreum),
				sdkmath.ZeroInt(),
				registeredXRPLToken.CoreumDenom,
			)
		})
	}
}

func claimFeesAndMakeAssertions(
	ctx context.Context,
	t *testing.T,
	contractClient *coreum.ContractClient,
	bankClient banktypes.QueryClient,
	relayers []coreum.Relayer,
	bridgingFeeAmount sdkmath.Int,
	remainderAmount sdkmath.Int,
	denom string,
) {
	expectedFeeAmount := bridgingFeeAmount.Quo(sdkmath.NewInt(int64(len(relayers))))
	expectedFee := sdk.NewCoins(sdk.NewCoin(denom, expectedFeeAmount))
	for _, relayer := range relayers {
		// assert fees are calculated correctly
		fees, err := contractClient.GetFeesCollected(ctx, relayer.CoreumAddress)
		require.NoError(t, err)
		if bridgingFeeAmount.IsZero() {
			require.Empty(t, fees)
			continue
		}
		require.Len(t, fees, 1)

		// collect fees
		relayerBalanceBeforeClaim, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
			Address: relayer.CoreumAddress.String(),
			Denom:   denom,
		})
		require.NoError(t, err)
		_, err = contractClient.ClaimRelayerFees(ctx, relayer.CoreumAddress, expectedFee)
		require.NoError(t, err)
		relayerBalanceAfterClaim, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
			Address: relayer.CoreumAddress.String(),
			Denom:   denom,
		})
		require.NoError(t, err)
		balanceChange := relayerBalanceAfterClaim.Balance.
			Sub(*relayerBalanceBeforeClaim.Balance)
		require.EqualValues(t, expectedFeeAmount.String(), balanceChange.Amount.String())

		// assert fees are now collected
		fees, err = contractClient.GetFeesCollected(ctx, relayer.CoreumAddress)
		require.NoError(t, err)
		if remainderAmount.IsZero() {
			require.Empty(t, fees, 0)
		} else {
			expectedRemainder := remainderAmount.QuoRaw(int64(len(relayers)))
			require.EqualValues(t, fees[0].Amount.String(), expectedRemainder.String())
		}
	}
}
