//go:build integrationtests
// +build integrationtests

package xrpl_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

type Amount struct {
	Value    string
	Currency string
}

func NewAmount(value, currency string) Amount {
	return Amount{
		Value:    value,
		Currency: currency,
	}
}

type Offer struct {
	TakerPays Amount
	TakerGets Amount
	Account   rippledata.Account
}

func TestExchange(t *testing.T) {

	ctx, chains := integrationtests.NewTestingContext(t)

	acc1 := chains.XRPL.GenAccount(ctx, t, 100)
	acc2 := chains.XRPL.GenAccount(ctx, t, 100)
	acc3 := chains.XRPL.GenAccount(ctx, t, 100)

	// mapping to print balances with static names
	accNames := map[string]rippledata.Account{
		"acc1": acc1,
		"acc2": acc2,
		"acc3": acc3,
	}

	fooCurrency := "FOO"
	barCurrency := "BAR"

	tickSize := uint8(3)

	type testCase struct {
		name   string
		offers []Offer
	}
	tests := []testCase{
		{
			name: "maker_receives_more",
			offers: []Offer{
				{
					TakerPays: NewAmount("3", fooCurrency),
					TakerGets: NewAmount("7", barCurrency), // 3/7 ~= 0.4285714285
					Account:   acc1,
				},
				{
					TakerPays: NewAmount("42.86", barCurrency),
					TakerGets: NewAmount("100", fooCurrency), // 42.86/100 = 0.4286
					Account:   acc2,
				},
				// result
				// acc1:0.006993006993006BAR, 3FOO
				// acc2:97FOO, 6.993006993006994BAR

				// 3 / (7 - 0.006993006993006 )
				// (100 - 97) / 6.993006993006994
			},
		},
		{
			name: "maker_receives_more",
			offers: []Offer{
				{
					TakerPays: NewAmount("0.000000000000003", fooCurrency),
					TakerGets: NewAmount("0.000000000000007", barCurrency), // 0.000000000000003/0.000000000000007 ~= 0.4285714285
					Account:   acc1,
				},
				{
					TakerPays: NewAmount("0.000000000040286", barCurrency),
					TakerGets: NewAmount("0.000000000100000", fooCurrency), // 0.000000000040286/0.000000000100000 = 0.4286
					Account:   acc2,
				},
				// result
				// acc1:3FOO, 6993006993006e-30BAR, 3e-15FOO
				// acc2:97FOO, 99997e-15FOO, 6993006993006994e-30BAR
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// ------------------------------ EXECUTION ------------------------------

			issuer := chains.XRPL.GenAccount(ctx, t, 100)
			setUpIssuerAccount(ctx, t, chains, issuer, tickSize)

			currencies := make(map[rippledata.Currency]struct{})

			t.Logf("----------Offers----------")
			for _, offer := range tt.offers {
				for accName, acc := range accNames {
					if offer.Account.String() != acc.String() {
						continue
					}
					t.Logf("Acc:%s, TakerPays:%s%s, TakerGets:%s%s",
						accName,
						offer.TakerPays.Value, offer.TakerPays.Currency,
						offer.TakerGets.Value, offer.TakerGets.Currency,
					)
				}

				takerPaysCurrency, err := rippledata.NewCurrency(offer.TakerPays.Currency)
				require.NoError(t, err)
				currencies[takerPaysCurrency] = struct{}{}

				takerGetsCurrency, err := rippledata.NewCurrency(offer.TakerGets.Currency)
				require.NoError(t, err)
				currencies[takerGetsCurrency] = struct{}{}

				setMaxTrustSet(ctx, t, chains, issuer, takerPaysCurrency, offer.Account)
				setMaxTrustSet(ctx, t, chains, issuer, takerGetsCurrency, offer.Account)
				// offer creator pays TakerGets amount
				valueToFund, err := rippledata.NewValue(offer.TakerGets.Value, false)
				require.NoError(t, err)
				amountToFund := rippledata.Amount{
					Value:    valueToFund,
					Currency: takerGetsCurrency,
					Issuer:   issuer,
				}
				fundAccount(ctx, t, chains, offer.Account, amountToFund)
			}

			t.Log("----------Balances before offers creation----------")
			printBalances(ctx, t, chains, accNames, currencies, issuer)

			offerSequences := make(map[uint32]rippledata.Account, 0)

			for _, offer := range tt.offers {
				offerTakerPaysValue, err := rippledata.NewValue(offer.TakerPays.Value, false)
				require.NoError(t, err)
				offerTakerPaysCurrency, err := rippledata.NewCurrency(offer.TakerPays.Currency)
				require.NoError(t, err)
				offerTakerPaysAmount := rippledata.Amount{
					Value:    offerTakerPaysValue,
					Currency: offerTakerPaysCurrency,
					Issuer:   issuer,
				}

				offerTakerGetsValue, err := rippledata.NewValue(offer.TakerGets.Value, false)
				require.NoError(t, err)
				offerTakerGetsCurrency, err := rippledata.NewCurrency(offer.TakerGets.Currency)
				require.NoError(t, err)
				offerTakerGetsAmount := rippledata.Amount{
					Value:    offerTakerGetsValue,
					Currency: offerTakerGetsCurrency,
					Issuer:   issuer,
				}

				offerSeq := createOffer(ctx, t, chains, offer.Account, offerTakerPaysAmount, offerTakerGetsAmount)
				if offerSeq != nil {
					offerSequences[*offerSeq] = offer.Account
				}
			}

			//t.Log("----------Balances after offers creation----------")
			//printBalances(ctx, t, chains, accNames, currencies, issuer)

			// cancelling all offers
			for offerSequence, acc := range offerSequences {
				cancelOffer(ctx, t, chains, acc, offerSequence)
			}

			t.Log("----------Balances after offers cancelling----------")
			printBalances(ctx, t, chains, accNames, currencies, issuer)
		})
	}
}

func printBalances(
	ctx context.Context,
	t *testing.T,
	chains integrationtests.Chains,
	accNames map[string]rippledata.Account,
	currencies map[rippledata.Currency]struct{}, issuer rippledata.Account) {

	accKeys := lo.Keys(accNames)
	sort.Strings(accKeys)

	for _, name := range accKeys {
		acc := accNames[name]

		balances := chains.XRPL.GetAccountBalances(ctx, t, acc)
		results := make([]string, 0)
		for currency := range currencies {
			key := fmt.Sprintf("%s/%s", xrpl.ConvertCurrencyToString(currency), issuer.String())
			balance, ok := balances[key]
			if !ok || balance.Value.IsZero() {
				continue
			}
			results = append(results, fmt.Sprintf("%s%s", balance.Value.String(), currency))
		}
		if len(results) == 0 {
			continue
		}
		t.Logf("%s:%s", name, strings.Join(results, ", "))
	}
}

func createOffer(
	ctx context.Context,
	t *testing.T,
	chains integrationtests.Chains,
	sender rippledata.Account,
	takerPays rippledata.Amount,
	takerGets rippledata.Amount,
) *uint32 {
	offer1CreateTx := rippledata.OfferCreate{
		TakerPays: takerPays,
		TakerGets: takerGets,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.OFFER_CREATE,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &offer1CreateTx, sender))

	txRes, err := chains.XRPL.RPCClient().Tx(ctx, offer1CreateTx.Hash)
	require.NoError(t, err)

	var offer1Seq *uint32
	for _, an := range txRes.MetaData.AffectedNodes {
		createdNode := an.CreatedNode
		if createdNode == nil {
			continue
		}
		if createdNode.LedgerEntryType != rippledata.OFFER {
			continue
		}
		offer, ok := createdNode.NewFields.(*rippledata.Offer)
		if !ok {
			panic("failed to cast tp *rippledata.Offer")
		}
		offer1Seq = offer.Sequence
	}

	return offer1Seq
}

func setUpIssuerAccount(
	ctx context.Context,
	t *testing.T,
	chains integrationtests.Chains,
	issuer rippledata.Account,
	tickSize uint8,
) {
	tx := rippledata.AccountSet{
		SetFlag: lo.ToPtr(uint32(rippledata.TxDefaultRipple)),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.ACCOUNT_SET,
		},
		TickSize: lo.ToPtr(uint8(tickSize)),
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &tx, issuer))
}

func setMaxTrustSet(
	ctx context.Context,
	t *testing.T,
	chains integrationtests.Chains,
	issuer rippledata.Account,
	currency rippledata.Currency,
	accounts ...rippledata.Account,
) {
	currencyTrustSetValue, err := rippledata.NewValue("10e80", false)
	require.NoError(t, err)

	for _, acc := range accounts {
		tx := rippledata.TrustSet{
			LimitAmount: rippledata.Amount{
				Value:    currencyTrustSetValue,
				Currency: currency,
				Issuer:   issuer,
			},
			TxBase: rippledata.TxBase{
				TransactionType: rippledata.TRUST_SET,
				Flags:           lo.ToPtr(rippledata.TxSetNoRipple),
			},
		}
		require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &tx, acc))
	}
}

func fundAccount(
	ctx context.Context,
	t *testing.T,
	chains integrationtests.Chains,
	recipientAcc rippledata.Account,
	amount rippledata.Amount,
) {
	tx := rippledata.Payment{
		Destination: recipientAcc,
		Amount:      amount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &tx, amount.Issuer))
}

func cancelOffer(
	ctx context.Context,
	t *testing.T,
	chains integrationtests.Chains,
	sender rippledata.Account,
	offerSeq uint32,
) {
	offer1CancelTx := rippledata.OfferCancel{
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.OFFER_CANCEL,
		},
		OfferSequence: offerSeq,
	}
	require.NoError(t, chains.XRPL.AutoFillSignAndSubmitTx(ctx, t, &offer1CancelTx, sender))
}
