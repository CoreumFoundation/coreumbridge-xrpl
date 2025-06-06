//nolint:tagliatelle // contract spec
package xrpl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"go.uber.org/zap"

	"github.com/CoreumFoundation/coreum-tools/pkg/retry"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
)

//go:generate mockgen -destination=rpc_mocks_test.go -package=xrpl_test . HTTPClient,RPCMetricRegistry

// UnknownTransactionResultErrorText error text for the unexpected tx code.
const UnknownTransactionResultErrorText = "Unknown TransactionResult"

// ******************** RPC command request objects ********************

// RPCError is RPC error result.
type RPCError struct {
	Name      string `json:"error"`
	Code      int    `json:"error_code"`
	Message   string `json:"error_message"`
	Exception string `json:"error_exception"`
}

// Error returns error string for the RPCError.
func (e *RPCError) Error() string {
	return fmt.Sprintf("failed to call RPC, error:%s, error code:%d, error message:%s, error exception:%s",
		e.Name, e.Code, e.Message, e.Exception)
}

// AccountDataWithSigners is account data with the signers list.
type AccountDataWithSigners struct {
	rippledata.AccountRoot
	SignerList []rippledata.SignerList `json:"signer_lists"`
}

// AccountInfoRequest is `account_info` method request.
type AccountInfoRequest struct {
	Account     rippledata.Account `json:"account"`
	SignerLists bool               `json:"signer_lists"`
}

// AccountInfoResult is `account_info` method result.
type AccountInfoResult struct {
	LedgerSequence uint32                 `json:"ledger_current_index"`
	AccountData    AccountDataWithSigners `json:"account_data"`
}

// AccountLinesRequest is `account_lines` method request.
type AccountLinesRequest struct {
	Account     rippledata.Account  `json:"account"`
	Limit       uint32              `json:"limit"`
	LedgerIndex any                 `json:"ledger_index,omitempty"`
	Marker      string              `json:"marker,omitempty"`
	Result      *AccountLinesResult `json:"result,omitempty"`
}

// AccountLinesResult is `account_lines` method result.
type AccountLinesResult struct {
	LedgerSequence *uint32                     `json:"ledger_index"`
	Account        rippledata.Account          `json:"account"`
	Marker         string                      `json:"marker"`
	Lines          rippledata.AccountLineSlice `json:"lines"`
}

// SubmitRequest is `submit` method request.
type SubmitRequest struct {
	TxBlob string `json:"tx_blob"`
}

// SubmitResult is `submit` method result.
type SubmitResult struct {
	EngineResult        rippledata.TransactionResult `json:"engine_result"`
	EngineResultCode    int                          `json:"engine_result_code"`
	EngineResultMessage string                       `json:"engine_result_message"`
	TxBlob              string                       `json:"tx_blob"`
	Tx                  any                          `json:"tx_json"`
}

// TxRequest is `tx` method request.
type TxRequest struct {
	Transaction rippledata.Hash256 `json:"transaction"`
}

// TxResult is `tx` method result.
type TxResult struct {
	Validated bool `json:"validated"`
	rippledata.TransactionWithMetaData
}

// UnmarshalJSON is a shim to populate the Validated field before passing control on to
// TransactionWithMetaData.UnmarshalJSON.
func (txr *TxResult) UnmarshalJSON(b []byte) error {
	var extract map[string]any
	if err := json.Unmarshal(b, &extract); err != nil {
		return errors.Errorf("faild to Unmarshal to map[string]any")
	}
	validated, ok := extract["validated"]
	if ok {
		validatedVal, ok := validated.(bool)
		if !ok {
			return errors.Errorf("faild to decode object, the validated attribute is not boolean")
		}
		txr.Validated = validatedVal
	}

	return json.Unmarshal(b, &txr.TransactionWithMetaData)
}

// LedgerCurrentResult is `ledger_current` method request.
type LedgerCurrentResult struct {
	LedgerCurrentIndex int64  `json:"ledger_current_index"`
	Status             string `json:"status"`
}

// AccountTxRequest is `account_tx` method request.
type AccountTxRequest struct {
	Account   rippledata.Account `json:"account"`
	MinLedger int64              `json:"ledger_index_min"`
	MaxLedger int64              `json:"ledger_index_max"`
	Binary    bool               `json:"binary,omitempty"`
	Forward   bool               `json:"forward,omitempty"`
	Limit     uint32             `json:"limit,omitempty"`
	Marker    map[string]any     `json:"marker,omitempty"`
}

// AccountTxResult is `account_tx` method result.
type AccountTxResult struct {
	Marker       map[string]any              `json:"marker,omitempty"`
	Transactions rippledata.TransactionSlice `json:"transactions,omitempty"`
	Validated    bool                        `json:"validated"`
}

// AccountTxWithRawTxsResult is `account_tx` method result with json.RawMessage transactions.
type AccountTxWithRawTxsResult struct {
	Marker       map[string]any    `json:"marker,omitempty"`
	Transactions []json.RawMessage `json:"transactions,omitempty"`
	Validated    bool              `json:"validated"`
}

// ServerStateValidatedLedger is the latest validated ledger from the server state.
type ServerStateValidatedLedger struct {
	BaseFee     uint32 `json:"base_fee"`
	CloseTime   uint32 `json:"close_time"`
	Hash        string `json:"hash"`
	Seq         int64  `json:"seq"`
	ReserveBase int64  `json:"reserve_base"`
	ReserveInc  int64  `json:"reserve_inc"`
}

// ServerState is server state.
type ServerState struct {
	BuildVersion            string                     `json:"build_version"`
	CompleteLedgers         string                     `json:"complete_ledgers"`
	InitialSyncDurationUs   string                     `json:"initial_sync_duration_us"`
	LoadBase                uint32                     `json:"load_base"`
	LoadFactor              uint32                     `json:"load_factor"`
	LoadFactorFeeEscalation uint32                     `json:"load_factor_fee_escalation"`
	LoadFactorFeeQueue      uint32                     `json:"load_factor_fee_queue"`
	LoadFactorFeeReference  uint32                     `json:"load_factor_fee_reference"`
	LoadFactorServer        uint32                     `json:"load_factor_server"`
	NetworkID               uint32                     `json:"network_id"`
	ValidatedLedger         ServerStateValidatedLedger `json:"validated_ledger"`
}

// ServerStateResult is `server_state` method result.
type ServerStateResult struct {
	State  ServerState `json:"state"`
	Status string      `json:"status"`
}

// SrcCurrency is source currency for the pathfinding.
type SrcCurrency struct {
	Currency rippledata.Currency `json:"currency"`
}

// RipplePathFindRequest is ripple_path_find request.
type RipplePathFindRequest struct {
	SrcAccount    rippledata.Account `json:"source_account"`
	SrcCurrencies *[]SrcCurrency     `json:"source_currencies,omitempty"`
	DestAccount   rippledata.Account `json:"destination_account"`
	DestAmount    rippledata.Amount  `json:"destination_amount"`
}

// RipplePathFindResult is ripple_path_find result.
type RipplePathFindResult struct {
	Alternatives []struct {
		SrcAmount      rippledata.Amount  `json:"source_amount"`
		PathsComputed  rippledata.PathSet `json:"paths_computed,omitempty"`
		PathsCanonical rippledata.PathSet `json:"paths_canonical,omitempty"`
	}
	DestAccount    rippledata.Account    `json:"destination_account"`
	DestCurrencies []rippledata.Currency `json:"destination_currencies"`
}

// ******************** RPC transport objects ********************

// RPCRequest is general RPC request.
type RPCRequest struct {
	Method string `json:"method"`
	Params []any  `json:"params,omitempty"`
}

// RPCResponse is general RPC response.
type RPCResponse struct {
	Result any `json:"result"`
}

// ******************** XRPL RPC Client ********************

// HTTPClient is HTTP client interface.
type HTTPClient interface {
	DoJSON(ctx context.Context, method, url string, reqBody any, resDecoder func([]byte) error) error
}

// RPCMetricRegistry is rpc metric registry.
type RPCMetricRegistry interface {
	IncrementXRPLRPCDecodingErrorCounter()
}

// RPCClientConfig defines the config for the RPCClient.
type RPCClientConfig struct {
	URL       string
	PageLimit uint32
}

// DefaultRPCClientConfig returns default RPCClientConfig.
func DefaultRPCClientConfig(url string) RPCClientConfig {
	return RPCClientConfig{
		URL:       url,
		PageLimit: 100,
	}
}

// RPCClient implement the XRPL RPC client.
type RPCClient struct {
	cfg            RPCClientConfig
	log            logger.Logger
	httpClient     HTTPClient
	metricRegistry RPCMetricRegistry
}

// NewRPCClient returns new instance of the RPCClient.
func NewRPCClient(
	cfg RPCClientConfig,
	log logger.Logger,
	httpClient HTTPClient,
	metricRegistry RPCMetricRegistry,
) *RPCClient {
	return &RPCClient{
		cfg:            cfg,
		log:            log,
		httpClient:     httpClient,
		metricRegistry: metricRegistry,
	}
}

// GetXRPLBalance return account's XRPL balance.
func (c *RPCClient) GetXRPLBalance(
	ctx context.Context,
	acc rippledata.Account,
	currency rippledata.Currency,
	issuer rippledata.Account,
) (rippledata.Amount, error) {
	balances, err := c.GetXRPLBalances(ctx, acc)
	if err != nil {
		return rippledata.Amount{}, err
	}
	for _, balance := range balances {
		if balance.Currency.String() == currency.String() &&
			balance.Issuer.String() == issuer.String() {
			return balance, nil
		}
	}

	return rippledata.Amount{
		Value:    &rippledata.Value{},
		Currency: currency,
		Issuer:   issuer,
	}, nil
}

// GetXRPLBalances returns all account's XRPL balances including XRP token.
func (c *RPCClient) GetXRPLBalances(ctx context.Context, acc rippledata.Account) ([]rippledata.Amount, error) {
	balances := make([]rippledata.Amount, 0)
	accInfo, err := c.AccountInfo(ctx, acc)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get XRPL account info, address:%s", acc.String())
	}
	balances = append(balances, rippledata.Amount{
		Value: accInfo.AccountData.Balance,
		// XRP issuer and currency
		Currency: rippledata.Currency{},
		Issuer:   rippledata.Account{},
	})

	marker := ""
	for {
		accLines, err := c.AccountLines(ctx, acc, "closed", marker)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get XRPL account lines, address:%s", acc.String())
		}
		for _, line := range accLines.Lines {
			lineCopy := line
			// ignore the negative balance, since it means that the token is issued by bridge account and bridge account
			// owes this amount of token to someone
			if line.Balance.IsNegative() {
				continue
			}
			balances = append(balances, rippledata.Amount{
				Value:    &lineCopy.Balance.Value,
				Currency: lineCopy.Currency,
				Issuer:   lineCopy.Account,
			})
		}
		if accLines.Marker == "" {
			break
		}
		marker = accLines.Marker
	}

	return balances, nil
}

// AccountInfo returns the account information for the given account.
func (c *RPCClient) AccountInfo(ctx context.Context, acc rippledata.Account) (AccountInfoResult, error) {
	params := AccountInfoRequest{
		Account:     acc,
		SignerLists: true,
	}
	var result AccountInfoResult
	if err := c.callRPC(ctx, "account_info", params, &result); err != nil {
		return AccountInfoResult{}, err
	}

	return result, nil
}

// AccountLines returns the account lines for a given account.
func (c *RPCClient) AccountLines(
	ctx context.Context,
	account rippledata.Account,
	ledgerIndex any,
	marker string,
) (AccountLinesResult, error) {
	params := AccountLinesRequest{
		Account:     account,
		Limit:       c.cfg.PageLimit,
		Marker:      marker,
		LedgerIndex: ledgerIndex,
	}
	var result AccountLinesResult
	if err := c.callRPC(ctx, "account_lines", params, &result); err != nil {
		return AccountLinesResult{}, err
	}

	return result, nil
}

// Submit submits a transaction to the RPC server.
func (c *RPCClient) Submit(ctx context.Context, tx rippledata.Transaction) (SubmitResult, error) {
	_, raw, err := rippledata.Raw(tx)
	if err != nil {
		return SubmitResult{}, errors.Wrapf(err, "failed to convert transaction to raw data")
	}
	params := SubmitRequest{
		TxBlob: fmt.Sprintf("%X", raw),
	}
	var result SubmitResult
	if err := c.callRPC(ctx, "submit", params, &result); err != nil {
		if strings.Contains(err.Error(), UnknownTransactionResultErrorText) {
			c.log.Error(ctx, "Failed to decode XRPL transaction result", zap.Error(err))
			c.metricRegistry.IncrementXRPLRPCDecodingErrorCounter()
		}

		return SubmitResult{}, err
	}

	return result, nil
}

// Tx retrieves information about a transaction.
func (c *RPCClient) Tx(ctx context.Context, hash rippledata.Hash256) (TxResult, error) {
	params := TxRequest{
		Transaction: hash,
	}
	var result TxResult
	if err := c.callRPC(ctx, "tx", params, &result); err != nil {
		return TxResult{}, err
	}

	return result, nil
}

// LedgerCurrent returns information about current ledger.
func (c *RPCClient) LedgerCurrent(ctx context.Context) (LedgerCurrentResult, error) {
	var result LedgerCurrentResult
	if err := c.callRPC(ctx, "ledger_current", struct{}{}, &result); err != nil {
		return LedgerCurrentResult{}, err
	}

	return result, nil
}

// AccountTx returns paginated account transactions.
// Use minLedger -1 for the earliest ledger available.
// Use maxLedger -1 for the most recent validated ledger.
func (c *RPCClient) AccountTx(
	ctx context.Context,
	account rippledata.Account,
	minLedger, maxLedger int64,
	marker map[string]any,
) (AccountTxResult, error) {
	params := AccountTxRequest{
		Account:   account,
		MinLedger: minLedger,
		MaxLedger: maxLedger,
		Binary:    false,
		Forward:   true,
		Limit:     c.cfg.PageLimit,
		Marker:    marker,
	}
	var result AccountTxWithRawTxsResult
	if err := c.callRPC(ctx, "account_tx", params, &result); err != nil {
		return AccountTxResult{}, err
	}

	txs := make(rippledata.TransactionSlice, 0)
	for i, rawTx := range result.Transactions {
		var tx rippledata.TransactionWithMetaData
		if err := json.Unmarshal(rawTx, &tx); err != nil {
			c.log.Error(
				ctx,
				"Failed to decode json tx to rippledata.TransactionWithMetaData",
				zap.Error(err),
				zap.String("tx", string(rawTx)),
				zap.Int("txIndex", i),
				zap.String("account", account.String()),
				zap.Int64("minLedger", minLedger),
				zap.Int64("maxLedger", maxLedger),
				zap.Any("marker", marker),
			)
			c.metricRegistry.IncrementXRPLRPCDecodingErrorCounter()
			continue
		}
		txs = append(txs, &tx)
	}

	return AccountTxResult{
		Marker:       result.Marker,
		Transactions: txs,
		Validated:    result.Validated,
	}, nil
}

// ServerState returns the server state information.
func (c *RPCClient) ServerState(ctx context.Context) (ServerStateResult, error) {
	var result ServerStateResult
	if err := c.callRPC(ctx, "server_state", struct{}{}, &result); err != nil {
		return ServerStateResult{}, err
	}

	return result, nil
}

// RipplePathFind returns the found ripple paths.
func (c *RPCClient) RipplePathFind(
	ctx context.Context,
	srcAccount rippledata.Account,
	srcCurrencies *[]rippledata.Currency,
	destAccount rippledata.Account,
	destAmount rippledata.Amount,
) (RipplePathFindResult, error) {
	var paramsSrcCurrencies *[]SrcCurrency
	if srcCurrencies != nil {
		paramsSrcCurrencies = lo.ToPtr(
			lo.Map(
				*srcCurrencies,
				func(currency rippledata.Currency, _ int) SrcCurrency {
					return SrcCurrency{
						Currency: currency,
					}
				}),
		)
	}

	params := RipplePathFindRequest{
		SrcAccount:    srcAccount,
		SrcCurrencies: paramsSrcCurrencies,
		DestAccount:   destAccount,
		DestAmount:    destAmount,
	}

	var result RipplePathFindResult
	if err := c.callRPC(ctx, "ripple_path_find", params, &result); err != nil {
		return RipplePathFindResult{}, err
	}

	return result, nil
}

// AutoFillTx add seq number and fee for the transaction.
func (c *RPCClient) AutoFillTx(
	ctx context.Context,
	tx rippledata.Transaction,
	sender rippledata.Account,
	txSignatureCount uint32,
) error {
	accInfo, err := c.AccountInfo(ctx, sender)
	if err != nil {
		return err
	}
	// update base settings
	base := tx.GetBase()
	if err != nil {
		return err
	}
	fee, err := c.CalculateFee(txSignatureCount, DefaultXRPLBaseFee)
	if err != nil {
		return err
	}
	base.Fee = *fee
	base.Account = sender
	base.Sequence = *accInfo.AccountData.Sequence

	return nil
}

// SubmitAndAwaitSuccess submits tx a waits for its result, if result is not success returns an error.
func (c *RPCClient) SubmitAndAwaitSuccess(ctx context.Context, tx rippledata.Transaction) error {
	c.log.Info(ctx, "Submitting XRPL transaction", zap.String("txHash", strings.ToUpper(tx.GetHash().String())))
	// submit the transaction
	res, err := c.Submit(ctx, tx)
	if err != nil {
		return err
	}
	if !res.EngineResult.Success() {
		return errors.Errorf("the tx submition is failed, %+v", res)
	}

	retryCtx, retryCtxCancel := context.WithTimeout(ctx, time.Minute)
	defer retryCtxCancel()
	c.log.Info(
		ctx,
		"Transaction is submitted, waiting for tx to be accepted",
		zap.String("txHash", strings.ToUpper(tx.GetHash().String())),
	)
	return retry.Do(retryCtx, 250*time.Millisecond, func() error {
		reqCtx, reqCtxCancel := context.WithTimeout(ctx, 3*time.Second)
		defer reqCtxCancel()
		txRes, err := c.Tx(reqCtx, *tx.GetHash())
		if err != nil {
			return retry.Retryable(err)
		}
		if !txRes.Validated {
			return retry.Retryable(errors.Errorf("transaction is not validated"))
		}
		return nil
	})
}

// CalculateFee calculates fee for the transaction. It supports single and multiple signatures.
// Check https://xrpl.org/transaction-cost.html#special-transaction-costs for more details.
func (c *RPCClient) CalculateFee(txSignatureCount, baseFee uint32) (*rippledata.Value, error) {
	switch txSignatureCount {
	case 0:
		return nil, errors.New("tx signature count must be greater than 0")
	case 1:
		// Single sig: base_fee
		return rippledata.NewNativeValue(int64(baseFee))
	}

	// Multisig: base_fee × (1 + Number of Signatures Provided)
	return rippledata.NewNativeValue(int64(baseFee * (1 + txSignatureCount)))
}

func (c *RPCClient) callRPC(ctx context.Context, method string, params, result any) error {
	request := RPCRequest{
		Method: method,
		Params: []any{
			params,
		},
	}
	c.log.Debug(ctx, "Executing XRPL RPC request", zap.Any("request", request))

	err := c.httpClient.DoJSON(ctx, http.MethodPost, c.cfg.URL, request, func(resBytes []byte) error {
		c.log.Debug(ctx, "Received XRPL RPC result", zap.String("result", string(resBytes)))
		errResponse := RPCResponse{
			Result: &RPCError{},
		}
		if err := json.Unmarshal(resBytes, &errResponse); err != nil {
			return errors.Wrapf(err, "failed to decode http result to error result, raw http result:%s", string(resBytes))
		}
		errResult, ok := errResponse.Result.(*RPCError)
		if !ok {
			panic("failed to cast result to RPCError")
		}
		if errResult.Code != 0 || strings.TrimSpace(errResult.Name) != "" {
			return errResult
		}
		response := RPCResponse{
			Result: result,
		}
		if err := json.Unmarshal(resBytes, &response); err != nil {
			return errors.Wrapf(err, "failed decode http result to expected struct, raw http result:%s", string(resBytes))
		}

		return nil
	})
	if err != nil {
		return errors.Wrap(err, "failed to call RPC")
	}

	return nil
}
