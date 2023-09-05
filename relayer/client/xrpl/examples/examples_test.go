//go:build examples
// +build examples

package examples_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/CoreumFoundation/coreum-tools/pkg/retry"
	"github.com/pkg/errors"
	ripplecrypto "github.com/rubblelabs/ripple/crypto"
	rippledata "github.com/rubblelabs/ripple/data"
	ripplewebsockets "github.com/rubblelabs/ripple/websockets"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

var (
	xrpCurrency = "XRP"

	testnetHost  = "wss://s.altnet.rippletest.net:51233/"
	ecdsaKeyType = rippledata.ECDSA

	seedPhrase1 = "ssWU9edn2TGCByJAa6CXbAAsCkNzQ" // 0 key : rwPpi6BnAxvvEu75m8GtGtFRDFvMAUuiG3
	seedPhrase2 = "ss9D9iMVnq78mKPGwhHGxc4x8wJqY" // 0 key : rhFXgxqXMChyath7CkCHc2J8jJxPu8JftS
	seedPhrase3 = "ssr6XnquehSA89CndWo98dGYJBLtK" // 0 key : rQBLm9DqSQS6Z3ARvGm8JDcuXmH3zrWBXC
	seedPhrase4 = "shVSrqJcPstHAzbSJJqZ2yuWuWH4Y" // 0 key : rfXoPtE851hbUaFCgLioXAecCLAJngbod2
)

func TestXRPAndIssuedTokensPayment(t *testing.T) {
	remote, err := ripplewebsockets.NewRemote(testnetHost)
	defer remote.Close()

	issuerSeed, err := rippledata.NewSeedFromAddress(seedPhrase1)
	require.NoError(t, err)
	issuerKey := issuerSeed.Key(ecdsaKeyType)
	issuerKeySeq := lo.ToPtr(uint32(0))
	issuerAccount := issuerSeed.AccountId(ecdsaKeyType, issuerKeySeq)
	t.Logf("Issuer account: %s", issuerAccount)

	recipientSeed, err := rippledata.NewSeedFromAddress(seedPhrase2)
	require.NoError(t, err)
	recipientKey := recipientSeed.Key(ecdsaKeyType)
	recipientKeySeq := lo.ToPtr(uint32(0))
	recipientAccount := recipientSeed.AccountId(ecdsaKeyType, recipientKeySeq)
	t.Logf("Recipient account: %s", recipientAccount)

	// send XRP coins from issuer to recipient (if account is new you need to send 10 XRP to active it)
	xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
	require.NoError(t, err)
	xrpPaymentTx := rippledata.Payment{
		Destination: recipientAccount,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}

	require.NoError(t, signAndSubmitTx(t, remote, &xrpPaymentTx, issuerAccount, issuerKey, issuerKeySeq))

	// allow the FOO coin issued by the issuer to be received by the recipient
	const fooCurrencyCode = "FOO"
	fooCurrency, err := rippledata.NewCurrency(fooCurrencyCode)
	require.NoError(t, err)
	fooCurrencyTrustsetValue, err := rippledata.NewValue("10000000000000000", false)
	require.NoError(t, err)
	fooCurrencyTrustsetTx := rippledata.TrustSet{
		LimitAmount: rippledata.Amount{
			Value:    fooCurrencyTrustsetValue,
			Currency: fooCurrency,
			Issuer:   issuerAccount,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TRUST_SET,
		},
	}
	require.NoError(t, signAndSubmitTx(t, remote, &fooCurrencyTrustsetTx, recipientAccount, recipientKey, recipientKeySeq))

	// send/issue the FOO token
	fooAmount, err := rippledata.NewValue("100000", false)
	require.NoError(t, err)
	fooPaymentTx := rippledata.Payment{
		Destination: recipientAccount,
		Amount: rippledata.Amount{
			Value:    fooAmount,
			Currency: fooCurrency,
			Issuer:   issuerAccount,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	t.Logf("Recipinet account balance before: %s", getAccountBalance(t, remote, recipientAccount))
	require.NoError(t, signAndSubmitTx(t, remote, &fooPaymentTx, issuerAccount, issuerKey, issuerKeySeq))
	t.Logf("Recipinet account balance after: %s", getAccountBalance(t, remote, recipientAccount))
}

func TestMultisigPayment(t *testing.T) {
	remote, err := ripplewebsockets.NewRemote(testnetHost)
	defer remote.Close()

	multisigSeed, err := rippledata.NewSeedFromAddress(seedPhrase1)
	require.NoError(t, err)
	multisigKey := multisigSeed.Key(ecdsaKeyType)
	multisigKeySeq := lo.ToPtr(uint32(0))
	multisigAccount := multisigSeed.AccountId(ecdsaKeyType, multisigKeySeq)
	t.Logf("Multisig account: %s", multisigAccount)

	signer1Seed, err := rippledata.NewSeedFromAddress(seedPhrase2)
	require.NoError(t, err)
	signer1Key := signer1Seed.Key(ecdsaKeyType)
	signer1KeySeq := lo.ToPtr(uint32(0))
	signer1Account := signer1Seed.AccountId(ecdsaKeyType, signer1KeySeq)
	t.Logf("Signer1 account: %s", signer1Account)

	signer2Seed, err := rippledata.NewSeedFromAddress(seedPhrase3)
	require.NoError(t, err)
	signer2Key := signer2Seed.Key(ecdsaKeyType)
	signer2KeySeq := lo.ToPtr(uint32(0))
	signer2Account := signer2Seed.AccountId(ecdsaKeyType, signer2KeySeq)
	t.Logf("Signer2 account: %s", signer2Account)

	signer3Seed, err := rippledata.NewSeedFromAddress(seedPhrase4)
	require.NoError(t, err)
	signer3KeySeq := lo.ToPtr(uint32(0))
	signer3Account := signer3Seed.AccountId(ecdsaKeyType, signer3KeySeq)
	t.Logf("Signer3 account: %s", signer3Account)

	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum: 2, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &signer1Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &signer2Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &signer3Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, signAndSubmitTx(t, remote, &signerListSetTx, multisigAccount, multisigKey, multisigKeySeq))
	t.Logf("The signers set is updated")

	// prepare transaction to be signed
	xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
	require.NoError(t, err)

	// build payment tx using function to prevent signing function mutations
	buildXrpPaymentTx := func() rippledata.Payment {
		xrpPaymentTx := rippledata.Payment{
			Destination: signer1Account,
			Amount:      *xrpAmount,
			TxBase: rippledata.TxBase{
				TransactionType: rippledata.PAYMENT,
			},
		}
		autoFillTx(t, remote, &xrpPaymentTx, multisigAccount)
		// important for the multi-signing
		xrpPaymentTx.TxBase.SigningPubKey = &rippledata.PublicKey{}

		return xrpPaymentTx
	}

	signedXrpPaymentTx1 := buildXrpPaymentTx()
	require.NoError(t, rippledata.MultiSign(&signedXrpPaymentTx1, signer1Key, signer1KeySeq, signer1Account))

	signedXrpPaymentTx2 := buildXrpPaymentTx()
	require.NoError(t, rippledata.MultiSign(&signedXrpPaymentTx2, signer2Key, signer2KeySeq, signer2Account))

	xrpPaymentTx := buildXrpPaymentTx()
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTx, []rippledata.Signer{
		{
			Signer: rippledata.SignerItem{
				Account:       signer1Account,
				TxnSignature:  signedXrpPaymentTx1.TxnSignature,
				SigningPubKey: signedXrpPaymentTx1.SigningPubKey,
			},
		},
		{
			Signer: rippledata.SignerItem{
				Account:       signer2Account,
				TxnSignature:  signedXrpPaymentTx2.TxnSignature,
				SigningPubKey: signedXrpPaymentTx2.SigningPubKey,
			},
		},
	}...))

	t.Logf("Recipinet account balance before: %s", getAccountBalance(t, remote, xrpPaymentTx.Destination))
	require.NoError(t, submitTx(t, remote, &xrpPaymentTx))
	t.Logf("Recipinet account balance after: %s", getAccountBalance(t, remote, xrpPaymentTx.Destination))
}

func getAccountBalance(t *testing.T, remote *ripplewebsockets.Remote, acc rippledata.Account) map[string]rippledata.Amount {
	amounts := make(map[string]rippledata.Amount, 0)

	accInfo, err := remote.AccountInfo(acc)
	require.NoError(t, err)
	amounts[xrpCurrency] = rippledata.Amount{
		Value: accInfo.AccountData.Balance,
	}
	// none xrp amounts
	accLines, err := remote.AccountLines(acc, "closed")
	require.NoError(t, err)

	for _, line := range accLines.Lines {
		amounts[fmt.Sprintf("%s/%s", line.Currency.String(), line.Account.String())] = rippledata.Amount{
			Value:    &line.Balance.Value,
			Currency: line.Currency,
			Issuer:   line.Account,
		}
	}

	return amounts
}

func signAndSubmitTx(
	t *testing.T,
	remote *ripplewebsockets.Remote,
	tx rippledata.Transaction,
	sender rippledata.Account,
	key ripplecrypto.Key,
	keySeq *uint32,
) error {
	t.Helper()

	autoFillTx(t, remote, tx, sender)
	require.NoError(t, rippledata.Sign(tx, key, keySeq))

	return submitTx(t, remote, tx)
}

func autoFillTx(t *testing.T, remote *ripplewebsockets.Remote, tx rippledata.Transaction, sender rippledata.Account) {
	t.Helper()

	accInfo, err := remote.AccountInfo(sender)
	require.NoError(t, err)
	// update base settings
	base := tx.GetBase()
	fee, err := rippledata.NewValue("100", true)
	require.NoError(t, err)
	base.Fee = *fee
	base.Account = sender
	base.Sequence = *accInfo.AccountData.Sequence
}

func submitTx(t *testing.T, remote *ripplewebsockets.Remote, tx rippledata.Transaction) error {
	t.Helper()

	// submit the transaction
	res, err := remote.Submit(tx)
	if err != nil {
		return err
	}
	if !res.EngineResult.Success() {
		return errors.Errorf("the tx submition is failed, %+v", res)
	}

	retryCtx, retryCtxCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer retryCtxCancel()

	t.Logf("Transaction is submitted waitig for hash:%s", tx.GetHash())
	return retry.Do(retryCtx, 250*time.Millisecond, func() error {
		txRes, err := remote.Tx(*tx.GetHash())
		if err != nil {
			return retry.Retryable(err)
		}

		if !txRes.Validated {
			return retry.Retryable(errors.Errorf("transaction is not validated"))
		}

		return nil
	})
}
