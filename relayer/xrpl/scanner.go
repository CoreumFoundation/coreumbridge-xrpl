package xrpl

import (
	"context"
	"time"

	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"go.uber.org/zap"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
)

//go:generate ../../bin/mockgen -destination=scanner_mocks_test.go -package=xrpl_test . RPCTxProvider,ScannerMetricRegistry

// ScannerMetricRegistry is scanner metric registry.
type ScannerMetricRegistry interface {
	SetXRPLAccountRecentHistoryScanLedgerIndex(index float64)
	SetXRPLAccountFullHistoryScanLedgerIndex(index float64)
}

// RPCTxProvider is RPC transactions provider.
type RPCTxProvider interface {
	LedgerCurrent(ctx context.Context) (LedgerCurrentResult, error)
	AccountTx(
		ctx context.Context,
		account rippledata.Account,
		minLedger, maxLedger int64,
		marker map[string]any,
	) (AccountTxResult, error)
}

// AccountScannerConfig is the AccountScanner config.
type AccountScannerConfig struct {
	Account rippledata.Account

	RecentScanEnabled bool
	RecentScanWindow  int64
	RepeatRecentScan  bool

	FullScanEnabled bool
	RepeatFullScan  bool

	RetryDelay time.Duration
}

// DefaultAccountScannerConfig returns the default AccountScannerConfig.
func DefaultAccountScannerConfig(account rippledata.Account) AccountScannerConfig {
	return AccountScannerConfig{
		Account: account,

		RecentScanEnabled: true,
		RecentScanWindow:  10_000,
		RepeatRecentScan:  true,

		FullScanEnabled: true,
		RepeatFullScan:  true,
		RetryDelay:      10 * time.Second,
	}
}

// AccountScanner is XRPL transactions scanner.
type AccountScanner struct {
	cfg            AccountScannerConfig
	log            logger.Logger
	rpcTxProvider  RPCTxProvider
	metricRegistry ScannerMetricRegistry
}

// NewAccountScanner returns a nw instance of the AccountScanner.
func NewAccountScanner(
	cfg AccountScannerConfig,
	log logger.Logger,
	rpcTxProvider RPCTxProvider,
	metricRegistry ScannerMetricRegistry,
) *AccountScanner {
	return &AccountScanner{
		cfg:            cfg,
		log:            log,
		rpcTxProvider:  rpcTxProvider,
		metricRegistry: metricRegistry,
	}
}

// ScanTxs subscribes on rpc account transactions and continuously scans the recent and historical transactions.
func (s *AccountScanner) ScanTxs(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error {
	s.log.Info(ctx, "Subscribing on XRPL tx scanner", zap.Any("config", s.cfg))

	if !s.cfg.RecentScanEnabled && !s.cfg.FullScanEnabled {
		return errors.Errorf("both recent and full scans are disabled")
	}

	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		if s.cfg.RecentScanEnabled {
			currentLedgerRes, err := s.rpcTxProvider.LedgerCurrent(ctx)
			if err != nil {
				return err
			}
			currentLedger := currentLedgerRes.LedgerCurrentIndex
			spawn("recent-history-scanner", parallel.Continue, func(ctx context.Context) error {
				s.scanRecentHistory(ctx, currentLedger, ch)
				return nil
			})
		}

		if s.cfg.FullScanEnabled {
			spawn("full-history-scanner", parallel.Continue, func(ctx context.Context) error {
				s.scanFullHistory(ctx, ch)
				return nil
			})
		}

		return nil
	}, parallel.WithGroupLogger(s.log))
}

func (s *AccountScanner) scanRecentHistory(
	ctx context.Context,
	currentLedger int64,
	ch chan<- rippledata.TransactionWithMetaData,
) {
	// in case we don't have enough ledgers for the window we start from the initial
	minLedger := int64(0)
	if currentLedger > s.cfg.RecentScanWindow {
		minLedger = currentLedger - s.cfg.RecentScanWindow
	}

	s.doWithRepeat(ctx, s.cfg.RepeatRecentScan, func() error {
		s.log.Debug(
			ctx,
			"Scanning recent XRPL account history",
			zap.Int64("minLedger", minLedger),
			zap.String("account", s.cfg.Account.String()),
		)
		lastLedger, err := s.scanTransactions(ctx, minLedger, s.metricRegistry.SetXRPLAccountRecentHistoryScanLedgerIndex, ch)
		// set minLedger to start with it in next iteration
		// even if the error was returned we still re-scan from the lastLedger
		if lastLedger > 0 {
			minLedger = lastLedger + 1
		}
		if err != nil {
			return err
		}
		s.log.Debug(ctx, "Scanning of the recent history is done", zap.Int64("lastLedger", lastLedger))
		return nil
	})
}

func (s *AccountScanner) scanFullHistory(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) {
	minLedger := int64(-1)
	s.doWithRepeat(ctx, s.cfg.RepeatFullScan, func() error {
		s.log.Debug(ctx, "Scanning XRPL account full history", zap.String("account", s.cfg.Account.String()))
		lastLedger, err := s.scanTransactions(ctx, minLedger, s.metricRegistry.SetXRPLAccountFullHistoryScanLedgerIndex, ch)
		if err != nil {
			// set minLedger to start with it in next iteration to complete the scanning
			minLedger = lastLedger + 1
			return err
		}
		// if scanning was done successfully update minLedger to start form the beginning in the next iteration
		minLedger = int64(-1)
		s.log.Debug(ctx, "Scanning of full history is done", zap.Int64("lastLedger", lastLedger))
		return nil
	})
}

func (s *AccountScanner) scanTransactions(
	ctx context.Context,
	minLedger int64,
	indexRegistryFunc func(float64),
	ch chan<- rippledata.TransactionWithMetaData,
) (int64, error) {
	if minLedger <= 0 {
		minLedger = -1
	}
	var (
		marker              map[string]any
		lastLedger          int64
		prevProcessedLedger int64
	)
	for {
		accountTxResult, err := s.rpcTxProvider.AccountTx(ctx, s.cfg.Account, minLedger, -1, marker)
		if err != nil {
			return lastLedger, errors.Wrapf(
				err,
				"failed to get account transactions, account:%s, minLedger:%d, marker:%+v",
				s.cfg.Account.String(), minLedger, marker,
			)
		}
		// we accept the transaction from the validated ledger only
		if accountTxResult.Validated {
			for _, tx := range accountTxResult.Transactions {
				// init prev processed ledger wasn't initialized we expect that we processed the prev ledger
				if prevProcessedLedger == 0 {
					prevProcessedLedger = int64(tx.LedgerSequence)
				}
				if prevProcessedLedger < int64(tx.LedgerSequence) {
					lastLedger = prevProcessedLedger
					prevProcessedLedger = int64(tx.LedgerSequence)
				}
				indexRegistryFunc(float64(prevProcessedLedger))
				if tx == nil {
					continue
				}
				select {
				case <-ctx.Done():
					return lastLedger, ctx.Err()
				case ch <- *tx:
				}
			}
		}
		if len(accountTxResult.Marker) == 0 {
			lastLedger = prevProcessedLedger
			break
		}
		marker = accountTxResult.Marker
	}

	return lastLedger, nil
}

func (s *AccountScanner) doWithRepeat(ctx context.Context, shouldRepeat bool, f func() error) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := f(); err != nil {
				s.log.Error(ctx, "XRPL scanner is failed in do with repeat", zap.Error(err))
			}
			if !shouldRepeat {
				s.log.Info(ctx, "Execution is fully stopped.")
				return
			}
			s.log.Debug(ctx, "Waiting before the next execution.", zap.String("delay", s.cfg.RetryDelay.String()))
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.cfg.RetryDelay):
			}
		}
	}
}
