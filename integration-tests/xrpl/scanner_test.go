//go:build integrationtests
// +build integrationtests

package xrpl_test

import (
	"context"
	"testing"
	"time"

	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreum-tools/pkg/http"
	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestFullHistoryScanAccountTx(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	const txsCount = 20

	senderAcc := chains.XRPL.GenAccount(ctx, t, 100)
	t.Logf("Sender account: %s", senderAcc)

	recipientAcc := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("Recipient account: %s", recipientAcc)

	// generate txs
	writtenTxHashes := sendMultipleTxs(ctx, t, chains.XRPL, txsCount, senderAcc, recipientAcc)

	rpcClientConfig := xrpl.DefaultRPCClientConfig(chains.XRPL.Config().RPCAddress)
	// update the page limit to low to emulate multiple pages
	rpcClientConfig.PageLimit = 2
	rpcClient := xrpl.NewRPCClient(
		rpcClientConfig,
		chains.Log,
		http.NewRetryableClient(http.DefaultClientConfig()),
	)

	// enable just historical scan
	scannerCfg := xrpl.AccountScannerConfig{
		Account:           senderAcc,
		RecentScanEnabled: false,
		FullScanEnabled:   true,
		RetryDelay:        time.Second,
	}
	scanner := xrpl.NewAccountScanner(scannerCfg, chains.Log, rpcClient)
	// add timeout to finish the tests in case of error

	txsCh := make(chan rippledata.TransactionWithMetaData, txsCount)

	t.Logf("Waiting for %d transactions to be scanned by the historycal scanner", len(writtenTxHashes))

	require.NoError(t, parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		spawn("scan", parallel.Continue, func(ctx context.Context) error {
			return scanner.ScanTxs(ctx, txsCh)
		})
		spawn("read", parallel.Exit, func(ctx context.Context) error {
			return validateTxsHashesInChannel(ctx, writtenTxHashes, txsCh)
		})
		return nil
	}))
}

func TestRecentHistoryScanAccountTx(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)

	const txsCount = 20

	senderAcc := chains.XRPL.GenAccount(ctx, t, 100)
	t.Logf("Sender account: %s", senderAcc)

	recipientAcc := chains.XRPL.GenAccount(ctx, t, 0)
	t.Logf("Recipient account: %s", recipientAcc)

	rpcClientConfig := xrpl.DefaultRPCClientConfig(chains.XRPL.Config().RPCAddress)
	// update the page limit to low to emulate multiple pages
	rpcClientConfig.PageLimit = 2
	rpcClient := xrpl.NewRPCClient(
		rpcClientConfig,
		chains.Log,
		http.NewRetryableClient(http.DefaultClientConfig()),
	)

	// update config to use recent scan only
	scannerCfg := xrpl.AccountScannerConfig{
		Account:           senderAcc,
		RecentScanEnabled: true,
		RecentScanWindow:  5,
		RepeatRecentScan:  true,
		FullScanEnabled:   false,
		RetryDelay:        500 * time.Millisecond,
	}
	scanner := xrpl.NewAccountScanner(scannerCfg, chains.Log, rpcClient)

	// await for the state when the current ledger is valid to run the scanner
	currentLedger, err := chains.XRPL.RPCClient().LedgerCurrent(ctx)
	require.NoError(t, err)
	chains.XRPL.AwaitLedger(ctx, t, currentLedger.LedgerCurrentIndex+scannerCfg.RecentScanWindow)

	var writtenTxHashes map[string]struct{}
	receivedTxHashes := make(map[string]struct{})

	txsCh := make(chan rippledata.TransactionWithMetaData, txsCount)
	require.NoError(t, parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		spawn("scan", parallel.Continue, func(ctx context.Context) error {
			return scanner.ScanTxs(ctx, txsCh)
		})
		spawn("write", parallel.Continue, func(ctx context.Context) error {
			writtenTxHashes = sendMultipleTxs(ctx, t, chains.XRPL, txsCount, senderAcc, recipientAcc)
			return nil
		})
		spawn("wait", parallel.Exit, func(ctx context.Context) error {
			t.Logf("Waiting for %d transactions to be scanned by the scanner", txsCount)
			for tx := range txsCh {
				receivedTxHashes[tx.GetHash().String()] = struct{}{}
				if len(receivedTxHashes) == txsCount {
					return nil
				}
			}
			return nil
		})
		return nil
	}))

	require.Equal(t, writtenTxHashes, receivedTxHashes)
}

func sendMultipleTxs(
	ctx context.Context,
	t *testing.T,
	xrplChain integrationtests.XRPLChain,
	count int,
	senderAcc, recipientAcc rippledata.Account,
) map[string]struct{} {
	writtenTxHashes := make(map[string]struct{})
	for i := 0; i < count; i++ {
		xrpAmount, err := rippledata.NewAmount("100000") // 0.1 XRP tokens
		require.NoError(t, err)
		xrpPaymentTx := rippledata.Payment{
			Destination: recipientAcc,
			Amount:      *xrpAmount,
			TxBase: rippledata.TxBase{
				TransactionType: rippledata.PAYMENT,
			},
		}
		require.NoError(t, xrplChain.AutoFillSignAndSubmitTx(ctx, t, &xrpPaymentTx, senderAcc))
		writtenTxHashes[xrpPaymentTx.Hash.String()] = struct{}{}
	}
	t.Logf("Successfully sent %d transactions", len(writtenTxHashes))
	return writtenTxHashes
}

func validateTxsHashesInChannel(
	ctx context.Context, writtenTxHashes map[string]struct{}, txsCh chan rippledata.TransactionWithMetaData,
) error {
	scanCtx, scanCtxCancel := context.WithTimeout(ctx, time.Minute)
	defer scanCtxCancel()
	// copy the original map
	expectedHashes := make(map[string]struct{}, len(writtenTxHashes))
	for k, v := range writtenTxHashes {
		expectedHashes[k] = v
	}
	for {
		select {
		case <-scanCtx.Done():
			return scanCtx.Err()
		case tx := <-txsCh:
			// validate that we have all sent hashed and no duplicated
			hash := tx.GetHash().String()
			if _, found := expectedHashes[hash]; !found {
				return errors.Errorf("not found expected tx hash:%s", hash)
			}

			delete(expectedHashes, hash)
			if len(expectedHashes) == 0 {
				return nil
			}
		}
	}
}
