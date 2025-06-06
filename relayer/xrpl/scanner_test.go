package xrpl_test

import (
	"context"
	"encoding/hex"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

type txTemplate struct {
	Hash           string
	LedgerSequence uint32
}

func TestAccountScanner_ScanTxs(t *testing.T) {
	t.Parallel()

	account := xrpl.GenPrivKeyTxSigner().Account()
	notEmptyMarker := map[string]any{"key": "val"}

	tests := []struct {
		name          string
		cfg           xrpl.AccountScannerConfig
		rpcTxProvider func(ctrl *gomock.Controller) xrpl.RPCTxProvider
		wantTxHashes  []string
		wantErr       bool
	}{
		{
			name: "full_scan_positive_with_retry_two_pages",
			cfg: xrpl.AccountScannerConfig{
				Account:         account,
				FullScanEnabled: true,
				RepeatFullScan:  true,
				RetryDelay:      time.Millisecond,
			},
			rpcTxProvider: func(ctrl *gomock.Controller) xrpl.RPCTxProvider {
				mockedProvider := NewMockRPCTxProvider(ctrl)
				callNumber := 0
				mockedProvider.EXPECT().AccountTx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(
						ctx context.Context,
						account rippledata.Account,
						minLedger, maxLedger int64,
						marker map[string]any,
					) (xrpl.AccountTxResult, error) {
						callNumber++
						switch callNumber {
						case 1:
							require.Equal(t, int64(-1), minLedger)
							return xrpl.AccountTxResult{
								Validated: true,
								Transactions: buildEmptyTransactions([]txTemplate{
									{
										Hash:           "1",
										LedgerSequence: 1,
									},
									{
										Hash:           "2",
										LedgerSequence: 2,
									},
									{
										Hash:           "3",
										LedgerSequence: 3,
									},
								}),
								Marker: notEmptyMarker,
							}, nil
						// emulate error
						case 2:
							require.Equal(t, int64(-1), minLedger)
							return xrpl.AccountTxResult{}, errors.New("error")
						case 3:
							require.Equal(t, int64(3), minLedger)
							return xrpl.AccountTxResult{
								Validated: true,
								Transactions: buildEmptyTransactions([]txTemplate{
									{
										Hash:           "4",
										LedgerSequence: 3,
									},
									{
										Hash:           "5",
										LedgerSequence: 4,
									},
								}),
							}, nil
						default:
							panic("unexpected call")
						}
					}).AnyTimes()
				return mockedProvider
			},
			wantTxHashes: []string{
				"1", "2", "3", "4", "5",
			},
			wantErr: false,
		},
		{
			name: "recent_scan_positive_with_retry_four_pages",
			cfg: xrpl.AccountScannerConfig{
				Account:           account,
				RecentScanEnabled: true,
				RecentScanWindow:  10,
				RepeatRecentScan:  true,
				RetryDelay:        time.Millisecond,
			},
			rpcTxProvider: func(ctrl *gomock.Controller) xrpl.RPCTxProvider {
				mockedProvider := NewMockRPCTxProvider(ctrl)

				mockedProvider.EXPECT().LedgerCurrent(gomock.Any()).Return(xrpl.LedgerCurrentResult{
					LedgerCurrentIndex: 100,
				}, nil)

				callNumber := 0
				mockedProvider.EXPECT().AccountTx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(
						ctx context.Context,
						account rippledata.Account,
						minLedger, maxLedger int64,
						marker map[string]any,
					) (xrpl.AccountTxResult, error) {
						callNumber++
						switch callNumber {
						case 1:
							require.Equal(t, int64(100-10), minLedger)
							return xrpl.AccountTxResult{
								Validated: true,
								Transactions: buildEmptyTransactions([]txTemplate{
									{
										Hash:           "1",
										LedgerSequence: 90,
									},
									{
										Hash:           "2",
										LedgerSequence: 91,
									},
									{
										Hash:           "3",
										LedgerSequence: 91,
									},
									{
										Hash:           "4",
										LedgerSequence: 92,
									},
								}),
								Marker: notEmptyMarker,
							}, nil
						case 2:
							require.Equal(t, int64(100-10), minLedger)
							return xrpl.AccountTxResult{
								Validated: true,
								Transactions: buildEmptyTransactions([]txTemplate{
									{
										Hash:           "5",
										LedgerSequence: 92,
									},
								}),
								// finish
								Marker: nil,
							}, nil
						case 3:
							require.Equal(t, int64(93), minLedger)
							return xrpl.AccountTxResult{}, errors.New("error")
						case 4:
							require.Equal(t, int64(93), minLedger)
							return xrpl.AccountTxResult{
								Validated: true,
								Transactions: buildEmptyTransactions([]txTemplate{
									{
										Hash:           "6",
										LedgerSequence: 93,
									},
								}),
								// finish
								Marker: nil,
							}, nil
						case 5:
							require.Equal(t, int64(94), minLedger)
							return xrpl.AccountTxResult{
								Validated: true,
								Transactions: buildEmptyTransactions([]txTemplate{
									{
										Hash:           "7",
										LedgerSequence: 94,
									},
								}),
							}, nil
						default:
							panic("unexpected call")
						}
					}).AnyTimes()
				return mockedProvider
			},
			wantTxHashes: []string{
				"1", "2", "3", "4", "5", "6", "7",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			logMock := logger.NewAnyLogMock(ctrl)
			logMock.EXPECT().Error(gomock.Any(), gomock.Any(), gomock.Any())
			rpcTxProvider := tt.rpcTxProvider(ctrl)

			metricRegistryMock := NewMockScannerMetricRegistry(ctrl)
			metricRegistryMock.EXPECT().SetXRPLAccountRecentHistoryScanLedgerIndex(gomock.Any()).AnyTimes()
			metricRegistryMock.EXPECT().SetXRPLAccountFullHistoryScanLedgerIndex(gomock.Any()).AnyTimes()

			s := xrpl.NewAccountScanner(
				tt.cfg,
				logMock,
				rpcTxProvider,
				metricRegistryMock,
			)
			txsCh := make(chan rippledata.TransactionWithMetaData)

			ctx := t.Context()
			require.NoError(t, parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
				spawn("scan", parallel.Continue, func(ctx context.Context) error {
					return s.ScanTxs(ctx, txsCh)
				})
				spawn("read", parallel.Exit, func(ctx context.Context) error {
					// validate that we have received expected hashes
					gotTxHashes := readTxHashesFromChannels(ctx, t, txsCh, len(tt.wantTxHashes))
					expectedTxHashes := lo.SliceToMap(tt.wantTxHashes, func(hash string) (string, struct{}) {
						return hash, struct{}{}
					})
					if !reflect.DeepEqual(expectedTxHashes, gotTxHashes) {
						return errors.Errorf("expectec tx hashes:%v, got:%v", expectedTxHashes, gotTxHashes)
					}
					return nil
				})
				return nil
			}))
		})
	}
}

func readTxHashesFromChannels(
	ctx context.Context,
	t *testing.T,
	txsCh chan rippledata.TransactionWithMetaData,
	count int,
) map[string]struct{} {
	gotTxHashes := make(map[string]struct{})
	for {
		select {
		case <-ctx.Done():
			t.Fail()
		case tx := <-txsCh:
			decoded, err := hex.DecodeString(strings.TrimRight(strings.ToUpper(tx.GetHash().String()), "0"))
			require.NoError(t, err)
			gotTxHashes[string(decoded)] = struct{}{}
			if len(gotTxHashes) == count {
				return gotTxHashes
			}
		}
	}
}

func buildEmptyTransactions(txsData []txTemplate) []*rippledata.TransactionWithMetaData {
	txs := make([]*rippledata.TransactionWithMetaData, 0, len(txsData))
	for _, tx := range txsData {
		var txHash rippledata.Hash256
		copy(txHash[:], tx.Hash)
		txs = append(txs, &rippledata.TransactionWithMetaData{
			LedgerSequence: tx.LedgerSequence,
			Transaction: &rippledata.Payment{
				TxBase: rippledata.TxBase{
					Hash: txHash,
				},
			},
		})
	}
	// order by ledger sequence
	sort.Slice(txs, func(i, j int) bool {
		return txs[i].LedgerSequence < txs[j].LedgerSequence
	})
	return txs
}
