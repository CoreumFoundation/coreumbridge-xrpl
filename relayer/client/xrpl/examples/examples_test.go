//go:build examples
// +build examples

package examples_test

import (
	"context"
	"fmt"
	"math/rand"
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

	seedPhrase1 = "sneZuwbLynqsZQtRuDa7yt6maJbuR" // 0 key : rwPpi6BnAxvvEu75m8GtGtFRDFvMAUuiG3
	seedPhrase2 = "ss9D9iMVnq78mKPGwhHGxc4x8wJqY" // 0 key : rhFXgxqXMChyath7CkCHc2J8jJxPu8JftS
	seedPhrase3 = "ssr6XnquehSA89CndWo98dGYJBLtK" // 0 key : rQBLm9DqSQS6Z3ARvGm8JDcuXmH3zrWBXC
	seedPhrase4 = "shVSrqJcPstHAzbSJJqZ2yuWuWH4Y" // 0 key : rfXoPtE851hbUaFCgLioXAecCLAJngbod2
)

// ********** Wallet **********

type Wallet struct {
	Key      ripplecrypto.Key
	Sequence *uint32
	Account  rippledata.Account
}

func NewWalletFromSeedPhrase(seedPhrase string) (Wallet, error) {
	seed, err := rippledata.NewSeedFromAddress(seedPhrase)
	if err != nil {
		return Wallet{}, err
	}

	key := seed.Key(ecdsaKeyType)
	seq := lo.ToPtr(uint32(0))
	account := seed.AccountId(ecdsaKeyType, seq)

	return Wallet{
		Key:      key,
		Sequence: seq,
		Account:  account,
	}, nil
}

func GenWallet() (Wallet, error) {
	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, 10)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}

	familySeed, err := ripplecrypto.GenerateFamilySeed(string(b))
	if err != nil {
		panic(err)
	}
	seed, err := rippledata.NewSeedFromAddress(familySeed.String())
	if err != nil {
		panic(err)
	}

	return NewWalletFromSeedPhrase(seed.String())
}

func (w Wallet) MultiSign(tx rippledata.MultiSignable) (rippledata.Signer, error) {
	if err := rippledata.MultiSign(tx, w.Key, w.Sequence, w.Account); err != nil {
		return rippledata.Signer{}, err
	}

	return rippledata.Signer{
		Signer: rippledata.SignerItem{
			Account:       w.Account,
			TxnSignature:  tx.GetSignature(),
			SigningPubKey: tx.GetPublicKey(),
		},
	}, nil
}

// ********** Tests **********

func TestXRPAndIssuedTokensPayment(t *testing.T) {
	remote, err := ripplewebsockets.NewRemote(testnetHost)
	require.NoError(t, err)
	defer remote.Close()

	issuerWallet, err := NewWalletFromSeedPhrase(seedPhrase1)
	require.NoError(t, err)
	t.Logf("Issuer account: %s", issuerWallet.Account)

	recipientWallet, err := NewWalletFromSeedPhrase(seedPhrase2)
	require.NoError(t, err)
	t.Logf("Recipient account: %s", recipientWallet.Account)

	// send XRP coins from issuer to recipient (if account is new you need to send 10 XRP to activate it)
	xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
	require.NoError(t, err)
	xrpPaymentTx := rippledata.Payment{
		Destination: recipientWallet.Account,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}

	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &xrpPaymentTx, issuerWallet))

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
			Issuer:   issuerWallet.Account,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TRUST_SET,
		},
	}
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &fooCurrencyTrustsetTx, recipientWallet))

	// send/issue the FOO token
	fooAmount, err := rippledata.NewValue("100000", false)
	require.NoError(t, err)
	fooPaymentTx := rippledata.Payment{
		Destination: recipientWallet.Account,
		Amount: rippledata.Amount{
			Value:    fooAmount,
			Currency: fooCurrency,
			Issuer:   issuerWallet.Account,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	t.Logf("Recipinet account balance before: %s", getAccountBalance(t, remote, recipientWallet.Account))
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &fooPaymentTx, issuerWallet))
	t.Logf("Recipinet account balance after: %s", getAccountBalance(t, remote, recipientWallet.Account))
}

func TestMultisigPayment(t *testing.T) {
	remote, err := ripplewebsockets.NewRemote(testnetHost)
	require.NoError(t, err)
	defer remote.Close()

	multisigWallet, err := NewWalletFromSeedPhrase(seedPhrase1)
	require.NoError(t, err)
	t.Logf("Multisig account: %s", multisigWallet.Account)

	wallet1, err := NewWalletFromSeedPhrase(seedPhrase2)
	require.NoError(t, err)
	t.Logf("Wallet1 account: %s", wallet1.Account)

	wallet2, err := NewWalletFromSeedPhrase(seedPhrase3)
	require.NoError(t, err)
	t.Logf("Wallet2 account: %s", wallet2.Account)

	wallet3, err := NewWalletFromSeedPhrase(seedPhrase4)
	require.NoError(t, err)
	t.Logf("Wallet3 account: %s", wallet3.Account)

	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum: 2, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &wallet1.Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &wallet2.Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &wallet3.Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &signerListSetTx, multisigWallet))
	t.Logf("The signers set is updated")

	xrplPaymentTx := buildXrpPaymentTxForMultiSigning(t, remote, multisigWallet.Account, wallet1.Account)
	signer1, err := wallet1.MultiSign(&xrplPaymentTx)
	require.NoError(t, err)

	xrplPaymentTx = buildXrpPaymentTxForMultiSigning(t, remote, multisigWallet.Account, wallet1.Account)
	signer2, err := wallet2.MultiSign(&xrplPaymentTx)
	require.NoError(t, err)

	xrplPaymentTx = buildXrpPaymentTxForMultiSigning(t, remote, multisigWallet.Account, wallet1.Account)
	signer3, err := wallet3.MultiSign(&xrplPaymentTx)
	require.NoError(t, err)

	xrpPaymentTxTwoSigners := buildXrpPaymentTxForMultiSigning(t, remote, multisigWallet.Account, wallet1.Account)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTxTwoSigners, []rippledata.Signer{
		signer1,
		signer2,
	}...))

	xrpPaymentTxThreeSigners := buildXrpPaymentTxForMultiSigning(t, remote, multisigWallet.Account, wallet1.Account)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTxThreeSigners, []rippledata.Signer{
		signer1,
		signer2,
		signer3,
	}...))

	// compare hashes
	t.Logf("TwoSignersHash/ThreeSignersHash: %s/%s", xrpPaymentTxTwoSigners.Hash, xrpPaymentTxThreeSigners.Hash)
	require.NotEqual(t, xrpPaymentTxTwoSigners.Hash.String(), xrpPaymentTxThreeSigners.Hash.String())

	t.Logf("Recipinet account balance before: %s", getAccountBalance(t, remote, xrpPaymentTxTwoSigners.Destination))
	require.NoError(t, submitTx(t, remote, &xrpPaymentTxTwoSigners))
	t.Logf("Recipinet account balance after: %s", getAccountBalance(t, remote, xrpPaymentTxTwoSigners.Destination))

	// try to submit with three signers (the transaction won't be accepted)
	require.ErrorContains(t, submitTx(t, remote, &xrpPaymentTxThreeSigners), "This sequence number has already passed")
}

func TestCreateAndUseTicketForPaymentAndTicketsCreation(t *testing.T) {
	remote, err := ripplewebsockets.NewRemote(testnetHost)
	require.NoError(t, err)
	defer remote.Close()

	senderWallet, err := NewWalletFromSeedPhrase(seedPhrase1)
	require.NoError(t, err)
	t.Logf("Sender account: %s", senderWallet.Account)

	recipientWallet, err := NewWalletFromSeedPhrase(seedPhrase2)
	require.NoError(t, err)
	t.Logf("Recipient account: %s", recipientWallet.Account)

	ticketsToCreate := 1
	createTicketsTx := rippledata.TicketCreate{
		TicketCount: lo.ToPtr(uint32(ticketsToCreate)),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TICKET_CREATE,
		},
	}
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &createTicketsTx, senderWallet))
	txRes, err := remote.Tx(*createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets := extractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, ticketsToCreate)

	// create tickets with ticket
	ticketsToCreate = 2
	createTicketsTx = rippledata.TicketCreate{
		TicketCount: lo.ToPtr(uint32(ticketsToCreate)),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TICKET_CREATE,
		},
	}
	autoFillTx(t, remote, &createTicketsTx, senderWallet.Account)
	// reset sequence and add ticket
	createTicketsTx.TxBase.Sequence = 0
	createTicketsTx.TicketSequence = createdTickets[0].TicketSequence
	require.NoError(t, signAndSubmitTx(t, remote, &createTicketsTx, senderWallet))

	txRes, err = remote.Tx(*createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets = extractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, ticketsToCreate)

	// send XRP coins from sender to recipient with ticket
	xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
	require.NoError(t, err)
	xrpPaymentTx := rippledata.Payment{
		Destination: recipientWallet.Account,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	autoFillTx(t, remote, &xrpPaymentTx, senderWallet.Account)
	// reset sequence and add ticket
	xrpPaymentTx.TxBase.Sequence = 0
	xrpPaymentTx.TicketSequence = createdTickets[0].TicketSequence

	t.Logf("Recipinet account balance before: %s", getAccountBalance(t, remote, recipientWallet.Account))
	require.NoError(t, signAndSubmitTx(t, remote, &xrpPaymentTx, senderWallet))
	t.Logf("Recipinet account balance after: %s", getAccountBalance(t, remote, recipientWallet.Account))

	// try to use tickets for the transactions without the trust-line
	const newFooCurrencyCode = "NFO"
	fooCurrency, err := rippledata.NewCurrency(newFooCurrencyCode)
	require.NoError(t, err)
	// send/issue the FOO token
	fooAmount, err := rippledata.NewValue("100000", false)
	require.NoError(t, err)
	ticketForFailingTx := createdTickets[1].TicketSequence
	fooPaymentTx := rippledata.Payment{
		Destination: recipientWallet.Account,
		Amount: rippledata.Amount{
			Value:    fooAmount,
			Currency: fooCurrency,
			Issuer:   senderWallet.Account,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	autoFillTx(t, remote, &fooPaymentTx, senderWallet.Account)
	// reset sequence and add ticket
	fooPaymentTx.TxBase.Sequence = 0
	fooPaymentTx.TicketSequence = ticketForFailingTx
	// there is no trust set so the tx should fail and use the ticket
	require.ErrorContains(t, signAndSubmitTx(t, remote, &fooPaymentTx, senderWallet), "Path could not send partial amount")

	// try to reuse the ticket for the success tx
	xrpPaymentTx = rippledata.Payment{
		Destination: recipientWallet.Account,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	autoFillTx(t, remote, &xrpPaymentTx, senderWallet.Account)
	// reset sequence and add ticket
	xrpPaymentTx.TxBase.Sequence = 0
	xrpPaymentTx.TicketSequence = ticketForFailingTx
	// the ticket is used in prev failed transaction so can't be used here
	require.ErrorContains(t, signAndSubmitTx(t, remote, &fooPaymentTx, senderWallet), "Ticket is not in ledger")
}

func TestCreateAndUseTicketForTicketsCreationWithMultisigning(t *testing.T) {
	remote, err := ripplewebsockets.NewRemote(testnetHost)
	require.NoError(t, err)
	defer remote.Close()

	multisigWallet, err := NewWalletFromSeedPhrase(seedPhrase1)
	require.NoError(t, err)
	t.Logf("Multisig account: %s", multisigWallet.Account)

	wallet1, err := NewWalletFromSeedPhrase(seedPhrase2)
	require.NoError(t, err)
	t.Logf("Wallet1 account: %s", wallet1.Account)

	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum: 1, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &wallet1.Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &signerListSetTx, multisigWallet))
	t.Logf("The signers set is updated")

	ticketsToCreate := uint32(1)
	createTicketsTx := buildCreateTicketsTxForMultiSigning(t, remote, ticketsToCreate, nil, multisigWallet.Account)
	signer1, err := wallet1.MultiSign(&createTicketsTx)
	require.NoError(t, err)

	createTicketsTx = buildCreateTicketsTxForMultiSigning(t, remote, ticketsToCreate, nil, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&createTicketsTx, []rippledata.Signer{
		signer1,
	}...))

	require.NoError(t, submitTx(t, remote, &createTicketsTx))

	txRes, err := remote.Tx(*createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets := extractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, int(ticketsToCreate))

	createTicketsTx = buildCreateTicketsTxForMultiSigning(t, remote, ticketsToCreate, createdTickets[0].TicketSequence, multisigWallet.Account)
	signer1, err = wallet1.MultiSign(&createTicketsTx)
	require.NoError(t, err)

	createTicketsTx = buildCreateTicketsTxForMultiSigning(t, remote, ticketsToCreate, createdTickets[0].TicketSequence, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&createTicketsTx, []rippledata.Signer{
		signer1,
	}...))

	require.NoError(t, submitTx(t, remote, &createTicketsTx))

	txRes, err = remote.Tx(*createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets = extractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, int(ticketsToCreate))
}

func TestCreateAndUseTicketForMultisigningKeysRotation(t *testing.T) {
	remote, err := ripplewebsockets.NewRemote(testnetHost)
	require.NoError(t, err)
	defer remote.Close()

	multisigWallet, err := NewWalletFromSeedPhrase(seedPhrase1)
	require.NoError(t, err)
	t.Logf("Multisig account: %s", multisigWallet.Account)

	wallet1, err := NewWalletFromSeedPhrase(seedPhrase2)
	require.NoError(t, err)
	t.Logf("Wallet1 account: %s", wallet1.Account)

	wallet2, err := NewWalletFromSeedPhrase(seedPhrase3)
	require.NoError(t, err)
	t.Logf("Wallet2 account: %s", wallet2.Account)

	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum: 1, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &wallet1.Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &signerListSetTx, multisigWallet))

	ticketsToCreate := uint32(2)

	createTicketsTx := buildCreateTicketsTxForMultiSigning(t, remote, ticketsToCreate, nil, multisigWallet.Account)
	signer1, err := wallet1.MultiSign(&createTicketsTx)
	require.NoError(t, err)

	createTicketsTx = buildCreateTicketsTxForMultiSigning(t, remote, ticketsToCreate, nil, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&createTicketsTx, []rippledata.Signer{
		signer1,
	}...))
	require.NoError(t, submitTx(t, remote, &createTicketsTx))

	txRes, err := remote.Tx(*createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets := extractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, int(ticketsToCreate))

	updateSignerListSetTx := buildUpdateSignerListSetTxForMultiSigning(t, remote, wallet2.Account, createdTickets[0].TicketSequence, multisigWallet.Account)
	signer1, err = wallet1.MultiSign(&updateSignerListSetTx)
	require.NoError(t, err)

	updateSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(t, remote, wallet2.Account, createdTickets[0].TicketSequence, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&updateSignerListSetTx, []rippledata.Signer{
		signer1,
	}...))
	require.NoError(t, submitTx(t, remote, &updateSignerListSetTx))

	// try to sign and send with previous signer
	restoreSignerListSetTx := buildUpdateSignerListSetTxForMultiSigning(t, remote, wallet1.Account, createdTickets[1].TicketSequence, multisigWallet.Account)
	signer1, err = wallet1.MultiSign(&restoreSignerListSetTx)
	require.NoError(t, err)

	restoreSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(t, remote, wallet1.Account, createdTickets[1].TicketSequence, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&restoreSignerListSetTx, []rippledata.Signer{
		signer1,
	}...))
	require.ErrorContains(t, submitTx(t, remote, &restoreSignerListSetTx), "A signature is provided for a non-signer")

	// build and send with correct signer
	restoreSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(t, remote, wallet1.Account, createdTickets[1].TicketSequence, multisigWallet.Account)
	signer2, err := wallet2.MultiSign(&restoreSignerListSetTx)
	require.NoError(t, err)

	restoreSignerListSetTx = buildUpdateSignerListSetTxForMultiSigning(t, remote, wallet1.Account, createdTickets[1].TicketSequence, multisigWallet.Account)
	require.NoError(t, rippledata.SetSigners(&restoreSignerListSetTx, []rippledata.Signer{
		signer2,
	}...))
	require.NoError(t, submitTx(t, remote, &restoreSignerListSetTx))
}

func TestMultisigWithMasterKeyRemoval(t *testing.T) {
	remote, err := ripplewebsockets.NewRemote(testnetHost)
	require.NoError(t, err)
	defer remote.Close()

	multisigWalletToDisable, err := GenWallet()
	require.NoError(t, err)
	t.Logf("Multisig account: %s", multisigWalletToDisable.Account)
	fundAccount(t, remote, multisigWalletToDisable.Account, "20000000")

	wallet1, err := NewWalletFromSeedPhrase(seedPhrase2)
	require.NoError(t, err)
	t.Logf("Wallet1 account: %s", wallet1.Account)

	wallet2, err := NewWalletFromSeedPhrase(seedPhrase3)
	require.NoError(t, err)
	t.Logf("Wallet2 account: %s", wallet2.Account)

	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum: 2, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &wallet1.Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &wallet2.Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &signerListSetTx, multisigWalletToDisable))
	t.Logf("The signers set is updated")

	// disable master key now to be able to use multi-signing only
	disableMasterKeyTx := rippledata.AccountSet{
		TxBase: rippledata.TxBase{
			Account:         multisigWalletToDisable.Account,
			TransactionType: rippledata.ACCOUNT_SET,
		},
		SetFlag: lo.ToPtr(uint32(4)),
	}
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &disableMasterKeyTx, multisigWalletToDisable))
	t.Logf("The master key is disabled")

	// try to update signers one more time
	require.ErrorContains(t, autoFillSignAndSubmitTx(t, remote, &signerListSetTx, multisigWalletToDisable), "Master key is disabled")

	// now use multi-signing for the account
	xrpPaymentTx := buildXrpPaymentTxForMultiSigning(t, remote, multisigWalletToDisable.Account, wallet1.Account)
	signer1, err := wallet1.MultiSign(&xrpPaymentTx)
	require.NoError(t, err)

	xrpPaymentTx = buildXrpPaymentTxForMultiSigning(t, remote, multisigWalletToDisable.Account, wallet1.Account)
	signer2, err := wallet2.MultiSign(&xrpPaymentTx)
	require.NoError(t, err)

	xrpPaymentTx = buildXrpPaymentTxForMultiSigning(t, remote, multisigWalletToDisable.Account, wallet1.Account)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTx, []rippledata.Signer{
		signer1,
		signer2,
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

func autoFillSignAndSubmitTx(
	t *testing.T,
	remote *ripplewebsockets.Remote,
	tx rippledata.Transaction,
	wallet Wallet,
) error {
	t.Helper()

	autoFillTx(t, remote, tx, wallet.Account)
	return signAndSubmitTx(t, remote, tx, wallet)
}

func signAndSubmitTx(
	t *testing.T,
	remote *ripplewebsockets.Remote,
	tx rippledata.Transaction,
	wallet Wallet,
) error {
	t.Helper()

	require.NoError(t, rippledata.Sign(tx, wallet.Key, wallet.Sequence))
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

	t.Logf("Submitting transaction, hash:%s", tx.GetHash())
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

func extractTicketsFromMeta(txRes *ripplewebsockets.TxResult) []*rippledata.Ticket {
	createdTickets := make([]*rippledata.Ticket, 0)
	for _, node := range txRes.MetaData.AffectedNodes {
		createdNode := node.CreatedNode
		if createdNode == nil {
			continue
		}
		newFields := createdNode.NewFields
		if newFields == nil {
			continue
		}
		if rippledata.TICKET.String() != newFields.GetType() {
			continue
		}
		ticket, ok := newFields.(*rippledata.Ticket)
		if !ok {
			continue
		}
		createdTickets = append(createdTickets, ticket)
	}

	return createdTickets
}

func fundAccount(t *testing.T, remote *ripplewebsockets.Remote, acc rippledata.Account, amount string) {
	t.Helper()

	xrpAmount, err := rippledata.NewAmount(amount)
	require.NoError(t, err)
	fundXrpTx := rippledata.Payment{
		Destination: acc,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}

	wallet, err := NewWalletFromSeedPhrase(seedPhrase1)
	require.NoError(t, err)
	t.Logf("Funding account: %s", wallet.Account)
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &fundXrpTx, wallet))
	t.Logf("The account %s is funded", acc)
}

func buildXrpPaymentTxForMultiSigning(
	t *testing.T,
	remote *ripplewebsockets.Remote,
	from, to rippledata.Account,
) rippledata.Payment {
	t.Helper()

	xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
	require.NoError(t, err)

	tx := rippledata.Payment{
		Destination: to,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	autoFillTx(t, remote, &tx, from)
	// important for the multi-signing
	tx.TxBase.SigningPubKey = &rippledata.PublicKey{}

	return tx
}

func buildCreateTicketsTxForMultiSigning(
	t *testing.T,
	remote *ripplewebsockets.Remote,
	ticketsToCreate uint32,
	ticketSeq *uint32,
	from rippledata.Account,
) rippledata.TicketCreate {
	tx := rippledata.TicketCreate{
		TicketCount: lo.ToPtr(uint32(ticketsToCreate)),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TICKET_CREATE,
		},
	}
	autoFillTx(t, remote, &tx, from)

	if ticketSeq != nil {
		tx.Sequence = 0
		tx.TicketSequence = ticketSeq
	}
	// important for the multi-signing
	tx.TxBase.SigningPubKey = &rippledata.PublicKey{}

	return tx
}

func buildUpdateSignerListSetTxForMultiSigning(
	t *testing.T,
	remote *ripplewebsockets.Remote,
	signerAcc rippledata.Account,
	ticketSeq *uint32,
	from rippledata.Account,
) rippledata.SignerListSet {
	tx := rippledata.SignerListSet{
		SignerQuorum: 1, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &signerAcc,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	autoFillTx(t, remote, &tx, from)
	// important for the multi-signing
	tx.TxBase.SigningPubKey = &rippledata.PublicKey{}

	if ticketSeq != nil {
		tx.Sequence = 0
		tx.TicketSequence = ticketSeq
	}

	return tx
}
