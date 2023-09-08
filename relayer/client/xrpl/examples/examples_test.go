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

func TestXRPAndIssuedTokensPayment(t *testing.T) {
	remote, err := ripplewebsockets.NewRemote(testnetHost)
	require.NoError(t, err)
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

	// send XRP coins from issuer to recipient (if account is new you need to send 10 XRP to activate it)
	xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
	require.NoError(t, err)
	xrpPaymentTx := rippledata.Payment{
		Destination: recipientAccount,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}

	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &xrpPaymentTx, issuerAccount, issuerKey, issuerKeySeq))

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
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &fooCurrencyTrustsetTx, recipientAccount, recipientKey, recipientKeySeq))

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
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &fooPaymentTx, issuerAccount, issuerKey, issuerKeySeq))
	t.Logf("Recipinet account balance after: %s", getAccountBalance(t, remote, recipientAccount))
}

func TestMultisigPayment(t *testing.T) {
	remote, err := ripplewebsockets.NewRemote(testnetHost)
	require.NoError(t, err)
	defer remote.Close()

	multisigKey, multisigKeySeq, multisigAccount := getSignerSet(t, seedPhrase1)
	t.Logf("Multisig account: %s", multisigAccount)

	signer1Key, signer1KeySeq, signer1Account := getSignerSet(t, seedPhrase2)
	t.Logf("Signer1 account: %s", signer1Account)

	signer2Key, signer2KeySeq, signer2Account := getSignerSet(t, seedPhrase3)
	t.Logf("Signer2 account: %s", signer2Account)

	signer3Key, signer3KeySeq, signer3Account := getSignerSet(t, seedPhrase4)
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
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &signerListSetTx, multisigAccount, multisigKey, multisigKeySeq))
	t.Logf("The signers set is updated")

	signedXrpPaymentTx1 := buildXrpPaymentTxForMultiSigning(t, remote, multisigAccount, signer1Account)
	require.NoError(t, rippledata.MultiSign(&signedXrpPaymentTx1, signer1Key, signer1KeySeq, signer1Account))
	signer1 := rippledata.Signer{
		Signer: rippledata.SignerItem{
			Account:       signer1Account,
			TxnSignature:  signedXrpPaymentTx1.TxnSignature,
			SigningPubKey: signedXrpPaymentTx1.SigningPubKey,
		},
	}

	signedXrpPaymentTx2 := buildXrpPaymentTxForMultiSigning(t, remote, multisigAccount, signer1Account)
	require.NoError(t, rippledata.MultiSign(&signedXrpPaymentTx2, signer2Key, signer2KeySeq, signer2Account))
	signer2 := rippledata.Signer{
		Signer: rippledata.SignerItem{
			Account:       signer2Account,
			TxnSignature:  signedXrpPaymentTx2.TxnSignature,
			SigningPubKey: signedXrpPaymentTx2.SigningPubKey,
		},
	}

	signedXrpPaymentTx3 := buildXrpPaymentTxForMultiSigning(t, remote, multisigAccount, signer1Account)
	require.NoError(t, rippledata.MultiSign(&signedXrpPaymentTx3, signer3Key, signer3KeySeq, signer3Account))
	signer3 := rippledata.Signer{
		Signer: rippledata.SignerItem{
			Account:       signer3Account,
			TxnSignature:  signedXrpPaymentTx3.TxnSignature,
			SigningPubKey: signedXrpPaymentTx3.SigningPubKey,
		},
	}

	xrpPaymentTxTwoSigners := buildXrpPaymentTxForMultiSigning(t, remote, multisigAccount, signer1Account)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTxTwoSigners, []rippledata.Signer{
		signer1,
		signer2,
	}...))

	xrpPaymentTxThreeSigners := buildXrpPaymentTxForMultiSigning(t, remote, multisigAccount, signer1Account)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTxThreeSigners, []rippledata.Signer{
		signer1,
		signer2,
		signer3,
	}...))

	t.Logf("TwoSignersHash/ThreeSignersHash: %s/%s", xrpPaymentTxTwoSigners.Hash, xrpPaymentTxThreeSigners.Hash)

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

	senderKey, senderKeySeq, senderAccount := getSignerSet(t, seedPhrase1)
	t.Logf("Sender account: %s", senderAccount)

	_, _, recipientAccount := getSignerSet(t, seedPhrase2)
	t.Logf("Recipient account: %s", recipientAccount)

	ticketsToCreate := 1
	createTicketsTx := rippledata.TicketCreate{
		TicketCount: lo.ToPtr(uint32(ticketsToCreate)),
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.TICKET_CREATE,
		},
	}
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &createTicketsTx, senderAccount, senderKey, senderKeySeq))
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
	autoFillTx(t, remote, &createTicketsTx, senderAccount)
	// reset sequence and add ticket
	createTicketsTx.TxBase.Sequence = 0
	createTicketsTx.TicketSequence = createdTickets[0].TicketSequence
	require.NoError(t, signAndSubmitTx(t, remote, &createTicketsTx, senderKey, senderKeySeq))

	txRes, err = remote.Tx(*createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets = extractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, ticketsToCreate)

	// send XRP coins from sender to recipient with ticket
	xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
	require.NoError(t, err)
	xrpPaymentTx := rippledata.Payment{
		Destination: recipientAccount,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	autoFillTx(t, remote, &xrpPaymentTx, senderAccount)
	// reset sequence and add ticket
	xrpPaymentTx.TxBase.Sequence = 0
	xrpPaymentTx.TicketSequence = createdTickets[0].TicketSequence

	t.Logf("Recipinet account balance before: %s", getAccountBalance(t, remote, recipientAccount))
	require.NoError(t, signAndSubmitTx(t, remote, &xrpPaymentTx, senderKey, senderKeySeq))
	t.Logf("Recipinet account balance after: %s", getAccountBalance(t, remote, recipientAccount))

	// try to use tickets for the transactions without the trust-line
	const newFooCurrencyCode = "NFO"
	fooCurrency, err := rippledata.NewCurrency(newFooCurrencyCode)
	require.NoError(t, err)
	// send/issue the FOO token
	fooAmount, err := rippledata.NewValue("100000", false)
	require.NoError(t, err)
	ticketForFailingTx := createdTickets[1].TicketSequence
	fooPaymentTx := rippledata.Payment{
		Destination: recipientAccount,
		Amount: rippledata.Amount{
			Value:    fooAmount,
			Currency: fooCurrency,
			Issuer:   senderAccount,
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	autoFillTx(t, remote, &fooPaymentTx, senderAccount)
	// reset sequence and add ticket
	fooPaymentTx.TxBase.Sequence = 0
	fooPaymentTx.TicketSequence = ticketForFailingTx
	// there is no trust set so the tx should fail and use the ticket
	require.ErrorContains(t, signAndSubmitTx(t, remote, &fooPaymentTx, senderKey, senderKeySeq), "Path could not send partial amount")

	// try to reuse the ticket for the success tx
	xrpPaymentTx = rippledata.Payment{
		Destination: recipientAccount,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	autoFillTx(t, remote, &xrpPaymentTx, senderAccount)
	// reset sequence and add ticket
	xrpPaymentTx.TxBase.Sequence = 0
	xrpPaymentTx.TicketSequence = ticketForFailingTx
	// the ticket is used in prev failed transaction so can't be used here
	require.ErrorContains(t, signAndSubmitTx(t, remote, &fooPaymentTx, senderKey, senderKeySeq), "Ticket is not in ledger")
}

func TestCreateAndUseTicketForTicketsCreationWithMultisigning(t *testing.T) {
	remote, err := ripplewebsockets.NewRemote(testnetHost)
	require.NoError(t, err)
	defer remote.Close()

	multisigKey, multisigKeySeq, multisigAccount := getSignerSet(t, seedPhrase1)
	t.Logf("Multisig account: %s", multisigAccount)

	signer1Key, signer1KeySeq, signer1Account := getSignerSet(t, seedPhrase2)
	t.Logf("Signer1 account: %s", signer1Account)

	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum: 1, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &signer1Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &signerListSetTx, multisigAccount, multisigKey, multisigKeySeq))
	t.Logf("The signers set is updated")

	ticketsToCreate := 1
	buildCreateTicketsTx := func() rippledata.TicketCreate {
		tx := rippledata.TicketCreate{
			TicketCount: lo.ToPtr(uint32(ticketsToCreate)),
			TxBase: rippledata.TxBase{
				TransactionType: rippledata.TICKET_CREATE,
			},
		}
		autoFillTx(t, remote, &tx, multisigAccount)
		// important for the multi-signing
		tx.TxBase.SigningPubKey = &rippledata.PublicKey{}

		return tx
	}

	createTicketsTx1 := buildCreateTicketsTx()
	require.NoError(t, rippledata.MultiSign(&createTicketsTx1, signer1Key, signer1KeySeq, signer1Account))

	createTicketsTx := buildCreateTicketsTx()
	require.NoError(t, rippledata.SetSigners(&createTicketsTx, []rippledata.Signer{
		{
			Signer: rippledata.SignerItem{
				Account:       signer1Account,
				TxnSignature:  createTicketsTx1.TxnSignature,
				SigningPubKey: createTicketsTx1.SigningPubKey,
			},
		},
	}...))

	require.NoError(t, submitTx(t, remote, &createTicketsTx))

	txRes, err := remote.Tx(*createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets := extractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, ticketsToCreate)

	buildCreateTicketsWithTicketTx := func() rippledata.TicketCreate {
		tx := rippledata.TicketCreate{
			TicketCount: lo.ToPtr(uint32(ticketsToCreate)),
			TxBase: rippledata.TxBase{
				TransactionType: rippledata.TICKET_CREATE,
			},
		}
		autoFillTx(t, remote, &tx, multisigAccount)
		// important for the multi-signing
		tx.TxBase.SigningPubKey = &rippledata.PublicKey{}
		// reset sequence and add ticket
		tx.TxBase.Sequence = 0
		tx.TicketSequence = createdTickets[0].TicketSequence

		return tx
	}

	createTicketsWithTicketTx1 := buildCreateTicketsWithTicketTx()
	require.NoError(t, rippledata.MultiSign(&createTicketsWithTicketTx1, signer1Key, signer1KeySeq, signer1Account))

	createTicketsWithTicketTx := buildCreateTicketsWithTicketTx()
	require.NoError(t, rippledata.SetSigners(&createTicketsWithTicketTx, []rippledata.Signer{
		{
			Signer: rippledata.SignerItem{
				Account:       signer1Account,
				TxnSignature:  createTicketsWithTicketTx1.TxnSignature,
				SigningPubKey: createTicketsWithTicketTx1.SigningPubKey,
			},
		},
	}...))

	require.NoError(t, submitTx(t, remote, &createTicketsWithTicketTx))

	txRes, err = remote.Tx(*createTicketsWithTicketTx.GetHash())
	require.NoError(t, err)

	createdTickets = extractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, ticketsToCreate)
}

func TestCreateAndUseTicketForMultisigningKeysRotation(t *testing.T) {
	remote, err := ripplewebsockets.NewRemote(testnetHost)
	require.NoError(t, err)
	defer remote.Close()

	multisigKey, multisigKeySeq, multisigAccount := getSignerSet(t, seedPhrase1)
	t.Logf("Multisig account: %s", multisigAccount)

	signer1Key, signer1KeySeq, signer1Account := getSignerSet(t, seedPhrase2)
	t.Logf("Signer1 account: %s", signer1Account)

	signer2Key, signer2KeySeq, signer2Account := getSignerSet(t, seedPhrase3)
	t.Logf("Signer2 account: %s", signer2Account)

	signerListSetTx := rippledata.SignerListSet{
		SignerQuorum: 1, // weighted threshold
		SignerEntries: []rippledata.SignerEntry{
			{
				SignerEntry: rippledata.SignerEntryItem{
					Account:      &signer1Account,
					SignerWeight: lo.ToPtr(uint16(1)),
				},
			},
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &signerListSetTx, multisigAccount, multisigKey, multisigKeySeq))

	ticketsToCreate := 2
	buildCreateTicketsTx := func() rippledata.TicketCreate {
		tx := rippledata.TicketCreate{
			TicketCount: lo.ToPtr(uint32(ticketsToCreate)),
			TxBase: rippledata.TxBase{
				TransactionType: rippledata.TICKET_CREATE,
			},
		}
		autoFillTx(t, remote, &tx, multisigAccount)
		// important for the multi-signing
		tx.TxBase.SigningPubKey = &rippledata.PublicKey{}

		return tx
	}

	createTicketsTx1 := buildCreateTicketsTx()
	require.NoError(t, rippledata.MultiSign(&createTicketsTx1, signer1Key, signer1KeySeq, signer1Account))

	createTicketsTx := buildCreateTicketsTx()
	require.NoError(t, rippledata.SetSigners(&createTicketsTx, []rippledata.Signer{
		{
			Signer: rippledata.SignerItem{
				Account:       signer1Account,
				TxnSignature:  createTicketsTx1.TxnSignature,
				SigningPubKey: createTicketsTx1.SigningPubKey,
			},
		},
	}...))

	require.NoError(t, submitTx(t, remote, &createTicketsTx))

	txRes, err := remote.Tx(*createTicketsTx.GetHash())
	require.NoError(t, err)

	createdTickets := extractTicketsFromMeta(txRes)
	require.Len(t, createdTickets, ticketsToCreate)

	// use created tickets to rotate keys
	buildUpdateSignerListSetTx := func() rippledata.SignerListSet {
		tx := rippledata.SignerListSet{
			SignerQuorum: 1, // weighted threshold
			SignerEntries: []rippledata.SignerEntry{
				{
					SignerEntry: rippledata.SignerEntryItem{
						Account:      &signer2Account,
						SignerWeight: lo.ToPtr(uint16(1)),
					},
				},
			},
			TxBase: rippledata.TxBase{
				TransactionType: rippledata.SIGNER_LIST_SET,
			},
		}
		autoFillTx(t, remote, &tx, multisigAccount)
		// important for the multi-signing
		tx.TxBase.SigningPubKey = &rippledata.PublicKey{}
		// reset sequence and add ticket
		tx.TxBase.Sequence = 0
		tx.TicketSequence = createdTickets[0].TicketSequence

		return tx
	}

	updateSignerListSetTx1 := buildUpdateSignerListSetTx()
	require.NoError(t, rippledata.MultiSign(&updateSignerListSetTx1, signer1Key, signer1KeySeq, signer1Account))

	updateSignerListSetTx := buildUpdateSignerListSetTx()
	require.NoError(t, rippledata.SetSigners(&updateSignerListSetTx, []rippledata.Signer{
		{
			Signer: rippledata.SignerItem{
				Account:       signer1Account,
				TxnSignature:  updateSignerListSetTx1.TxnSignature,
				SigningPubKey: updateSignerListSetTx1.SigningPubKey,
			},
		},
	}...))

	require.NoError(t, submitTx(t, remote, &updateSignerListSetTx))

	// use created tickets to rotate keys
	buildRestoreSignerListSetTx := func() rippledata.SignerListSet {
		tx := rippledata.SignerListSet{
			SignerQuorum: 1, // weighted threshold
			SignerEntries: []rippledata.SignerEntry{
				{
					SignerEntry: rippledata.SignerEntryItem{
						Account:      &signer1Account,
						SignerWeight: lo.ToPtr(uint16(1)),
					},
				},
			},
			TxBase: rippledata.TxBase{
				TransactionType: rippledata.SIGNER_LIST_SET,
			},
		}
		autoFillTx(t, remote, &tx, multisigAccount)
		// important for the multi-signing
		tx.TxBase.SigningPubKey = &rippledata.PublicKey{}
		// reset sequence and add ticket
		tx.TxBase.Sequence = 0
		tx.TicketSequence = createdTickets[1].TicketSequence

		return tx
	}

	// try to sign and send with previous signer
	restoreSignerListSetTx1 := buildRestoreSignerListSetTx()
	require.NoError(t, rippledata.MultiSign(&restoreSignerListSetTx1, signer1Key, signer1KeySeq, signer1Account))

	restoreSignerListSetTx := buildRestoreSignerListSetTx()
	require.NoError(t, rippledata.SetSigners(&restoreSignerListSetTx, []rippledata.Signer{
		{
			Signer: rippledata.SignerItem{
				Account:       signer2Account,
				TxnSignature:  restoreSignerListSetTx1.TxnSignature,
				SigningPubKey: restoreSignerListSetTx1.SigningPubKey,
			},
		},
	}...))
	require.ErrorContains(t, submitTx(t, remote, &restoreSignerListSetTx), "Invalid signature on account")

	// build and send with correct signer
	restoreSignerListSetTx1 = buildRestoreSignerListSetTx()
	require.NoError(t, rippledata.MultiSign(&restoreSignerListSetTx1, signer2Key, signer2KeySeq, signer2Account))

	restoreSignerListSetTx = buildRestoreSignerListSetTx()
	require.NoError(t, rippledata.SetSigners(&restoreSignerListSetTx, []rippledata.Signer{
		{
			Signer: rippledata.SignerItem{
				Account:       signer2Account,
				TxnSignature:  restoreSignerListSetTx1.TxnSignature,
				SigningPubKey: restoreSignerListSetTx1.SigningPubKey,
			},
		},
	}...))

	require.NoError(t, submitTx(t, remote, &restoreSignerListSetTx))
}

func TestMultisigWithMasterKeyRemoval(t *testing.T) {
	remote, err := ripplewebsockets.NewRemote(testnetHost)
	require.NoError(t, err)
	defer remote.Close()

	multisigKeyToDisable, multisigKeySeq, multisigAccount := getSignerSet(t, genSeed().String())
	t.Logf("Multisig account: %s", multisigAccount)
	fundAccount(t, remote, multisigAccount, "20000000")

	signer1Key, signer1KeySeq, signer1Account := getSignerSet(t, seedPhrase2)
	t.Logf("Signer1 account: %s", signer1Account)

	signer2Key, signer2KeySeq, signer2Account := getSignerSet(t, seedPhrase3)
	t.Logf("Signer2 account: %s", signer2Account)

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
		},
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
	}
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &signerListSetTx, multisigAccount, multisigKeyToDisable, multisigKeySeq))
	t.Logf("The signers set is updated")

	// disable master key now to be able to use multi-signing only
	disableMasterKeyTx := rippledata.AccountSet{
		TxBase: rippledata.TxBase{
			Account:         multisigAccount,
			TransactionType: rippledata.ACCOUNT_SET,
		},
		SetFlag: lo.ToPtr(uint32(4)),
	}
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &disableMasterKeyTx, multisigAccount, multisigKeyToDisable, multisigKeySeq))
	t.Logf("The master key is disabled")

	// try to update signers one more time
	require.ErrorContains(t, autoFillSignAndSubmitTx(t, remote, &signerListSetTx, multisigAccount, multisigKeyToDisable, multisigKeySeq), "Master key is disabled")

	// now use multi-signing for the account
	signedXrpPaymentTx1 := buildXrpPaymentTxForMultiSigning(t, remote, multisigAccount, signer1Account)
	require.NoError(t, rippledata.MultiSign(&signedXrpPaymentTx1, signer1Key, signer1KeySeq, signer1Account))
	signer1 := rippledata.Signer{
		Signer: rippledata.SignerItem{
			Account:       signer1Account,
			TxnSignature:  signedXrpPaymentTx1.TxnSignature,
			SigningPubKey: signedXrpPaymentTx1.SigningPubKey,
		},
	}

	signedXrpPaymentTx2 := buildXrpPaymentTxForMultiSigning(t, remote, multisigAccount, signer1Account)
	require.NoError(t, rippledata.MultiSign(&signedXrpPaymentTx2, signer2Key, signer2KeySeq, signer2Account))
	signer2 := rippledata.Signer{
		Signer: rippledata.SignerItem{
			Account:       signer2Account,
			TxnSignature:  signedXrpPaymentTx2.TxnSignature,
			SigningPubKey: signedXrpPaymentTx2.SigningPubKey,
		},
	}

	xrpPaymentTx := buildXrpPaymentTxForMultiSigning(t, remote, multisigAccount, signer1Account)
	require.NoError(t, rippledata.SetSigners(&xrpPaymentTx, []rippledata.Signer{
		signer1,
		signer2,
	}...))

	t.Logf("Recipinet account balance before: %s", getAccountBalance(t, remote, xrpPaymentTx.Destination))
	require.NoError(t, submitTx(t, remote, &xrpPaymentTx))
	t.Logf("Recipinet account balance after: %s", getAccountBalance(t, remote, xrpPaymentTx.Destination))
}

func getSignerSet(t *testing.T, seedPhrase string) (ripplecrypto.Key, *uint32, rippledata.Account) {
	seed, err := rippledata.NewSeedFromAddress(seedPhrase)
	require.NoError(t, err)
	key := seed.Key(ecdsaKeyType)
	seq := lo.ToPtr(uint32(0))
	account := seed.AccountId(ecdsaKeyType, seq)

	return key, seq, account
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
	sender rippledata.Account,
	key ripplecrypto.Key,
	keySeq *uint32,
) error {
	t.Helper()

	autoFillTx(t, remote, tx, sender)
	return signAndSubmitTx(t, remote, tx, key, keySeq)
}

func signAndSubmitTx(
	t *testing.T,
	remote *ripplewebsockets.Remote,
	tx rippledata.Transaction,
	key ripplecrypto.Key,
	keySeq *uint32,
) error {
	t.Helper()

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

	faucetSeed, err := rippledata.NewSeedFromAddress(seedPhrase1)
	require.NoError(t, err)
	faucetKey := faucetSeed.Key(ecdsaKeyType)
	faucetKeySeq := lo.ToPtr(uint32(0))
	faucetAccount := faucetSeed.AccountId(ecdsaKeyType, faucetKeySeq)
	t.Logf("Funding account: %s", acc)
	require.NoError(t, autoFillSignAndSubmitTx(t, remote, &fundXrpTx, faucetAccount, faucetKey, faucetKeySeq))
	t.Logf("The account %s is funded", acc)
}

func buildXrpPaymentTxForMultiSigning(t *testing.T, remote *ripplewebsockets.Remote, from, to rippledata.Account) rippledata.Payment {
	t.Helper()

	xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
	require.NoError(t, err)

	xrpPaymentTx := rippledata.Payment{
		Destination: to,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}
	autoFillTx(t, remote, &xrpPaymentTx, from)
	// important for the multi-signing
	xrpPaymentTx.TxBase.SigningPubKey = &rippledata.PublicKey{}

	return xrpPaymentTx
}

func genSeed() *rippledata.Seed {
	familySeed, err := ripplecrypto.GenerateFamilySeed(randString(10))
	if err != nil {
		panic(err)
	}
	seed, err := rippledata.NewSeedFromAddress(familySeed.String())
	if err != nil {
		panic(err)
	}

	return seed
}

func randString(n int) string {
	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}
