package integrationtests

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum-tools/pkg/http"
	"github.com/CoreumFoundation/coreum-tools/pkg/retry"
	coreumapp "github.com/CoreumFoundation/coreum/v3/app"
	coreumconfig "github.com/CoreumFoundation/coreum/v3/pkg/config"
	coreumkeyring "github.com/CoreumFoundation/coreum/v3/pkg/keyring"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

const (
	// XRPCurrencyCode is XRP toke currency code on XRPL chain.
	XRPCurrencyCode = "XRP"

	xrplTxFee                    = "100"
	xrplReserveToActivateAccount = float64(10)
	xrplReservePerTicket         = float64(2)
	xrplReservePerSigner         = float64(2)

	ecdsaKeyType         = rippledata.ECDSA
	faucetKeyringKeyName = "faucet"
)

// ********** XRPLChain **********

// XRPLChainConfig is a config required for the XRPL chain to be created.
type XRPLChainConfig struct {
	RPCAddress  string
	FundingSeed string
}

// XRPLChain is XRPL chain for the testing.
type XRPLChain struct {
	cfg       XRPLChainConfig
	signer    *xrpl.KeyringTxSigner
	rpcClient *xrpl.RPCClient
	fundMu    *sync.Mutex
}

// NewXRPLChain returns the new instance of the XRPL chain.
func NewXRPLChain(cfg XRPLChainConfig, log logger.Logger) (XRPLChain, error) {
	kr := createInMemoryKeyring()
	faucetPrivateKey, err := extractPrivateKeyFromSeed(cfg.FundingSeed)
	if err != nil {
		return XRPLChain{}, err
	}
	if err := kr.ImportPrivKeyHex(faucetKeyringKeyName, faucetPrivateKey, string(hd.Secp256k1Type)); err != nil {
		return XRPLChain{}, errors.Wrapf(err, "failed to import private key to keyring")
	}

	rpcClient := xrpl.NewRPCClient(
		xrpl.DefaultRPCClientConfig(cfg.RPCAddress),
		log,
		http.NewRetryableClient(http.DefaultClientConfig()),
	)

	signer := xrpl.NewKeyringTxSigner(kr)

	return XRPLChain{
		cfg:       cfg,
		signer:    signer,
		rpcClient: rpcClient,
		fundMu:    &sync.Mutex{},
	}, nil
}

// Config returns the chain config.
func (c XRPLChain) Config() XRPLChainConfig {
	return c.cfg
}

// RPCClient returns the XRPL RPC client.
func (c XRPLChain) RPCClient() *xrpl.RPCClient {
	return c.rpcClient
}

// GenAccount generates the active signer with the initial provided amount.
func (c XRPLChain) GenAccount(ctx context.Context, t *testing.T, amount float64) rippledata.Account {
	t.Helper()

	acc := c.GenEmptyAccount(t)
	c.CreateAccount(ctx, t, acc, amount)

	return acc
}

// GenEmptyAccount generates the signer but doesn't activate it.
func (c XRPLChain) GenEmptyAccount(t *testing.T) rippledata.Account {
	t.Helper()

	const signerKeyName = "signer"
	kr := createInMemoryKeyring()
	_, mnemonic, err := kr.NewMnemonic(
		signerKeyName,
		keyring.English,
		sdk.GetConfig().GetFullBIP44Path(),
		"",
		hd.Secp256k1,
	)
	require.NoError(t, err)
	acc, err := xrpl.NewKeyringTxSigner(kr).Account(signerKeyName)
	require.NoError(t, err)

	// reimport with the key as signer address
	_, err = c.signer.GetKeyring().NewAccount(
		acc.String(),
		mnemonic,
		"",
		sdk.GetConfig().GetFullBIP44Path(),
		hd.Secp256k1,
	)
	require.NoError(t, err)

	return acc
}

// CreateAccount funds the provided account with the amount/reserve to activate the account.
func (c XRPLChain) CreateAccount(ctx context.Context, t *testing.T, acc rippledata.Account, amount float64) {
	t.Helper()
	// amount to activate the account and some tokens on top
	c.FundAccount(ctx, t, acc, amount+xrplReserveToActivateAccount)
}

// GetSignerKeyring returns signer keyring.
func (c XRPLChain) GetSignerKeyring() keyring.Keyring {
	return c.signer.GetKeyring()
}

// GetSignerPubKey returns signer public key.
func (c XRPLChain) GetSignerPubKey(t *testing.T, acc rippledata.Account) rippledata.PublicKey {
	pubKey, err := c.signer.PubKey(acc.String())
	require.NoError(t, err)
	return pubKey
}

// ActivateAccount funds the provided account with the amount required for the activation.
func (c XRPLChain) ActivateAccount(ctx context.Context, t *testing.T, acc rippledata.Account) {
	t.Helper()

	c.FundAccount(ctx, t, acc, xrplReserveToActivateAccount)
}

// FundAccountForTicketAllocation funds the provided account with the amount required for the ticket allocation.
func (c XRPLChain) FundAccountForTicketAllocation(ctx context.Context, t *testing.T, acc rippledata.Account, ticketsNumber uint32) {
	c.FundAccount(ctx, t, acc, xrplReservePerTicket*float64(ticketsNumber))
}

// FundAccountForSignerListSet funds the provided account with the amount required for the multi-signing set.
func (c XRPLChain) FundAccountForSignerListSet(ctx context.Context, t *testing.T, acc rippledata.Account, singersCount int) {
	c.FundAccount(ctx, t, acc, xrplReservePerSigner*float64(singersCount))
}

// FundAccount funds the provided account with the provided amount.
func (c XRPLChain) FundAccount(ctx context.Context, t *testing.T, acc rippledata.Account, amount float64) {
	t.Helper()

	c.fundMu.Lock()
	defer c.fundMu.Unlock()

	xrpAmount, err := rippledata.NewAmount(fmt.Sprintf("%f%s", amount, XRPCurrencyCode))
	require.NoError(t, err)
	fundXrpTx := rippledata.Payment{
		Destination: acc,
		Amount:      *xrpAmount,
		TxBase: rippledata.TxBase{
			TransactionType: rippledata.PAYMENT,
		},
	}

	fundingAcc, err := c.signer.Account(faucetKeyringKeyName)
	require.NoError(t, err)
	c.AutoFillTx(ctx, t, &fundXrpTx, fundingAcc)
	require.NoError(t, c.signer.Sign(&fundXrpTx, faucetKeyringKeyName))

	t.Logf("Funding account, account address: %s, amount: %f", acc, amount)
	require.NoError(t, c.SubmitTx(ctx, t, &fundXrpTx))
	t.Logf("The account %s is funded", acc)
}

// AutoFillSignAndSubmitTx autofills the transaction and submits it.
func (c XRPLChain) AutoFillSignAndSubmitTx(ctx context.Context, t *testing.T, tx rippledata.Transaction, acc rippledata.Account) error {
	t.Helper()

	c.AutoFillTx(ctx, t, tx, acc)
	return c.SignAndSubmitTx(ctx, t, tx, acc)
}

// Multisign signs the transaction for the multi-signing.
func (c XRPLChain) Multisign(t *testing.T, tx rippledata.MultiSignable, acc rippledata.Account) rippledata.Signer {
	t.Helper()

	txSigner, err := c.signer.MultiSign(tx, acc.String())
	require.NoError(t, err)
	return txSigner
}

// SignAndSubmitTx signs the transaction from the signer and submits it.
func (c XRPLChain) SignAndSubmitTx(ctx context.Context, t *testing.T, tx rippledata.Transaction, acc rippledata.Account) error {
	t.Helper()

	require.NoError(t, c.signer.Sign(tx, acc.String()))
	return c.SubmitTx(ctx, t, tx)
}

// AutoFillTx add seq number and fee for the transaction.
func (c XRPLChain) AutoFillTx(ctx context.Context, t *testing.T, tx rippledata.Transaction, sender rippledata.Account) {
	t.Helper()

	accInfo, err := c.rpcClient.AccountInfo(ctx, sender)
	require.NoError(t, err)
	// update base settings
	base := tx.GetBase()
	fee, err := rippledata.NewValue(xrplTxFee, true)
	require.NoError(t, err)
	base.Fee = *fee
	base.Account = sender
	base.Sequence = *accInfo.AccountData.Sequence
}

// SubmitTx submits tx a waits for its result.
func (c XRPLChain) SubmitTx(ctx context.Context, t *testing.T, tx rippledata.Transaction) error {
	t.Helper()

	t.Logf("Submitting transaction, hash:%s", tx.GetHash())
	// submit the transaction
	res, err := c.rpcClient.Submit(ctx, tx)
	if err != nil {
		return err
	}
	if !res.EngineResult.Success() {
		return errors.Errorf("the tx submition is failed, %+v", res)
	}

	retryCtx, retryCtxCancel := context.WithTimeout(ctx, time.Minute)
	defer retryCtxCancel()

	t.Logf("Transaction is submitted waitig for hash:%s", tx.GetHash())
	return retry.Do(retryCtx, 250*time.Millisecond, func() error {
		reqCtx, reqCtxCancel := context.WithTimeout(ctx, 3*time.Second)
		defer reqCtxCancel()
		txRes, err := c.rpcClient.Tx(reqCtx, *tx.GetHash())
		if err != nil {
			return retry.Retryable(err)
		}

		if !txRes.Validated {
			return retry.Retryable(errors.Errorf("transaction is not validated"))
		}
		return nil
	})
}

// GetAccountBalance returns account balance for the provided issuer and currency.
func (c XRPLChain) GetAccountBalance(ctx context.Context, t *testing.T, account, issuer rippledata.Account, currency rippledata.Currency) rippledata.Amount {
	balance, ok := c.GetAccountBalances(ctx, t, account)[fmt.Sprintf("%s/%s", currency.String(), issuer.String())]
	if !ok {
		// equal to zero
		return rippledata.Amount{
			Value:    &rippledata.Value{},
			Currency: currency,
			Issuer:   issuer,
		}
	}
	return balance
}

// GetAccountBalances returns account balances.
func (c XRPLChain) GetAccountBalances(ctx context.Context, t *testing.T, acc rippledata.Account) map[string]rippledata.Amount {
	t.Helper()

	amounts := make(map[string]rippledata.Amount, 0)

	accInfo, err := c.rpcClient.AccountInfo(ctx, acc)
	require.NoError(t, err)
	amounts[XRPCurrencyCode] = rippledata.Amount{
		Value: accInfo.AccountData.Balance,
	}
	// none xrp amounts
	accLines, err := c.rpcClient.AccountLines(ctx, acc, "closed", nil)
	require.NoError(t, err)

	for _, line := range accLines.Lines {
		lineCopy := line
		amounts[fmt.Sprintf("%s/%s", lineCopy.Currency.String(), lineCopy.Account.String())] = rippledata.Amount{
			Value:    &lineCopy.Balance.Value,
			Currency: lineCopy.Currency,
			Issuer:   lineCopy.Account,
		}
	}

	return amounts
}

// AwaitLedger awaits for ledger index.
func (c XRPLChain) AwaitLedger(ctx context.Context, t *testing.T, ledgerIndex int64) {
	t.Helper()

	t.Logf("Waiting for the ledger:%d", ledgerIndex)
	retryCtx, retryCtxCancel := context.WithTimeout(ctx, time.Minute)
	defer retryCtxCancel()
	require.NoError(t, retry.Do(retryCtx, 250*time.Millisecond, func() error {
		reqCtx, reqCtxCancel := context.WithTimeout(retryCtx, 3*time.Second)
		defer reqCtxCancel()
		res, err := c.rpcClient.LedgerCurrent(reqCtx)
		if err != nil {
			return retry.Retryable(err)
		}

		if res.LedgerCurrentIndex < ledgerIndex {
			return retry.Retryable(errors.Errorf("ledger has not passed, current:%d, expected:%d", res.LedgerCurrentIndex, ledgerIndex))
		}

		return nil
	}))
}

func extractPrivateKeyFromSeed(seedPhrase string) (string, error) {
	seed, err := rippledata.NewSeedFromAddress(seedPhrase)
	if err != nil {
		return "", errors.Wrapf(err, "failed to create rippledata seed from seed phrase")
	}
	key := seed.Key(ecdsaKeyType)
	return hex.EncodeToString(key.Private(lo.ToPtr(uint32(0)))), nil
}

func createInMemoryKeyring() keyring.Keyring {
	encodingConfig := coreumconfig.NewEncodingConfig(coreumapp.ModuleBasics)
	return coreumkeyring.NewConcurrentSafeKeyring(keyring.NewInMemory(encodingConfig.Codec))
}

// ExtractTicketsFromMeta extracts tickets info from the tx metadata.
func ExtractTicketsFromMeta(txRes xrpl.TxResult) []*rippledata.Ticket {
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
