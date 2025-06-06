package xrpl_test

import (
	"context"
	"encoding/json"
	"testing"

	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestRPCClient_Submit(t *testing.T) {
	ctx := t.Context()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	logMock := logger.NewAnyLogMock(ctrl)
	httpClientMock := NewMockHTTPClient(ctrl)
	metricRegistry := NewMockRPCMetricRegistry(ctrl)

	var txHash rippledata.Hash256
	copy(txHash[:], "1")
	tx := &rippledata.TransactionWithMetaData{
		LedgerSequence: 1,
		Transaction: &rippledata.Payment{
			Amount: rippledata.Amount{
				Value: &rippledata.Value{},
			},
			TxBase: rippledata.TxBase{
				Hash: txHash,
			},
		},
	}

	rpcResult, err := json.Marshal(
		xrpl.RPCResponse{
			Result: json.RawMessage(`
      {
	  "engine_result": "UnexpectedCode",
	  "engine_result_code": 123456789,
	  "engine_result_message": "The transaction was applied. Only final in a validated ledger.",
	  "tx_blob": "data",
	  "tx_json": {
		"Data": "data"
	  }
	}`,
			),
		},
	)
	require.NoError(t, err)

	httpClientMock.EXPECT().DoJSON(ctx, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(
			ctx context.Context,
			method, url string,
			reqBody any,
			resDecoder func([]byte) error,
		) error {
			return resDecoder(rpcResult)
		},
	)

	logMock.EXPECT().Error(
		gomock.Any(), gomock.Any(), gomock.Any(),
	)

	metricRegistry.EXPECT().IncrementXRPLRPCDecodingErrorCounter()

	rpcClient := xrpl.NewRPCClient(xrpl.DefaultRPCClientConfig(""), logMock, httpClientMock, metricRegistry)
	_, err = rpcClient.Submit(ctx, tx)
	require.Error(t, err)
	require.ErrorContains(t, err, xrpl.UnknownTransactionResultErrorText)
}

func TestRPCClient_AccountTx(t *testing.T) {
	ctx := t.Context()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	logMock := logger.NewAnyLogMock(ctrl)
	httpClientMock := NewMockHTTPClient(ctrl)
	metricRegistry := NewMockRPCMetricRegistry(ctrl)

	var txHash1 rippledata.Hash256
	copy(txHash1[:], "1")
	tx1 := &rippledata.TransactionWithMetaData{
		LedgerSequence: 1,
		Transaction: &rippledata.Payment{
			Amount: rippledata.Amount{
				Value: &rippledata.Value{},
			},
			TxBase: rippledata.TxBase{
				Hash: txHash1,
			},
		},
	}
	tx1JSON, err := json.Marshal(tx1)
	require.NoError(t, err)

	var txHash2 rippledata.Hash256
	copy(txHash2[:], "2")
	tx2 := &rippledata.TransactionWithMetaData{
		LedgerSequence: 2,
		Transaction: &rippledata.Payment{
			Amount: rippledata.Amount{
				Value: &rippledata.Value{},
			},
			TxBase: rippledata.TxBase{
				Hash: txHash2,
			},
		},
	}
	tx2JSON, err := json.Marshal(tx2)
	require.NoError(t, err)

	rpcResult, err := json.Marshal(
		xrpl.RPCResponse{
			Result: xrpl.AccountTxWithRawTxsResult{
				Marker: nil,
				Transactions: []json.RawMessage{
					// the json in the middle is invalid
					tx1JSON, json.RawMessage(`{"x": "y"}`), tx2JSON,
				},
				Validated: true,
			},
		},
	)
	require.NoError(t, err)

	httpClientMock.EXPECT().DoJSON(ctx, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(
			ctx context.Context,
			method, url string,
			reqBody any,
			resDecoder func([]byte) error,
		) error {
			return resDecoder(rpcResult)
		},
	)

	logMock.EXPECT().Error(
		gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any(), gomock.Any(), gomock.Any(),
		gomock.Any(), gomock.Any(), gomock.Any(),
	)

	metricRegistry.EXPECT().IncrementXRPLRPCDecodingErrorCounter()

	rpcClient := xrpl.NewRPCClient(xrpl.DefaultRPCClientConfig(""), logMock, httpClientMock, metricRegistry)
	txRes, err := rpcClient.AccountTx(ctx, rippledata.Account{}, -1, -1, nil)
	require.NoError(t, err)
	require.Len(t, txRes.Transactions, 2)
	require.Equal(t, tx1.LedgerSequence, txRes.Transactions[0].LedgerSequence)
	require.Equal(t, tx2.LedgerSequence, txRes.Transactions[1].LedgerSequence)
}
