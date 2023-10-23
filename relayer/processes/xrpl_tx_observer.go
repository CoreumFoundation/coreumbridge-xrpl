package processes

import (
	"context"
	"encoding/hex"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/tracing"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

//go:generate mockgen -destination=xrpl_tx_observer_mocks_test.go -package=processes_test . EvidencesConsumer,XRPLAccountTxScanner

// EvidencesConsumer is the interface the evidences consumer interface.
type EvidencesConsumer interface {
	IsInitialized() bool
	SendXRPLToCoreumTransferEvidence(ctx context.Context, sender sdk.AccAddress, evidence coreum.XRPLToCoreumTransferEvidence) (*sdk.TxResponse, error)
}

// XRPLAccountTxScanner is XRPL account tx scanner.
type XRPLAccountTxScanner interface {
	ScanTxs(ctx context.Context, ch chan<- rippledata.TransactionWithMetaData) error
}

// XRPLTxObserverConfig is XRPLTxObserver config.
type XRPLTxObserverConfig struct {
	BridgeAccount rippledata.Account
}

// XRPLTxObserver is process which observes the XRPL txs and register the evidences in the contract.
type XRPLTxObserver struct {
	cfg               XRPLTxObserverConfig
	log               logger.Logger
	relayerAddress    sdk.AccAddress
	txScanner         XRPLAccountTxScanner
	evidencesConsumer EvidencesConsumer
}

// NewXRPLTxObserver returns a new instance of the XRPLTxObserver.
func NewXRPLTxObserver(
	cfg XRPLTxObserverConfig,
	log logger.Logger,
	relayerAddress sdk.AccAddress,
	txScanner XRPLAccountTxScanner,
	evidencesConsumer EvidencesConsumer,
) *XRPLTxObserver {
	return &XRPLTxObserver{
		cfg:               cfg,
		log:               log,
		relayerAddress:    relayerAddress,
		txScanner:         txScanner,
		evidencesConsumer: evidencesConsumer,
	}
}

// Init validates the process state.
func (o *XRPLTxObserver) Init(ctx context.Context) error {
	o.log.Debug(ctx, "Initializing process")

	if o.relayerAddress.Empty() {
		return errors.Errorf("failed to init process, relayer address is nil or empty")
	}
	if !o.evidencesConsumer.IsInitialized() {
		return errors.Errorf("failed to init process, contract client is not initialized")
	}

	return nil
}

// Start starts the process.
func (o *XRPLTxObserver) Start(ctx context.Context) error {
	txCh := make(chan rippledata.TransactionWithMetaData)
	if err := o.txScanner.ScanTxs(ctx, txCh); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case tx := <-txCh:
			if err := o.processTx(ctx, tx); err != nil {
				o.log.Error(ctx, "Failed to process XRPL tx", logger.Error(err))
			}
		}
	}
}

func (o *XRPLTxObserver) processTx(ctx context.Context, tx rippledata.TransactionWithMetaData) error {
	ctx = tracing.WithTracingXRPLTxHash(tracing.WithTracingID(ctx), tx.GetHash().String())
	if o.cfg.BridgeAccount == tx.GetBase().Account {
		return o.processOutgoingTx(ctx, tx)
	}

	return o.processIncomingTx(ctx, tx)
}

func (o *XRPLTxObserver) processIncomingTx(ctx context.Context, tx rippledata.TransactionWithMetaData) error {
	if !tx.MetaData.TransactionResult.Success() {
		o.log.Debug(
			ctx,
			"Skipping not successful transaction",
			logger.StringFiled("type", tx.GetType()),
			logger.StringFiled("txResult", tx.MetaData.TransactionResult.String()),
		)
		return nil
	}

	txType := tx.GetType()
	o.log.Debug(ctx, "Start processing of XRPL incoming tx", logger.StringFiled("type", txType))
	// we process only incoming payment transactions, other transactions are ignored
	if txType != rippledata.PAYMENT.String() {
		o.log.Debug(ctx, "Skipping not payment transaction", logger.StringFiled("type", tx.GetType()))
		return nil
	}
	paymentTx, ok := tx.Transaction.(*rippledata.Payment)
	if !ok {
		return errors.Errorf("failed to cast tx to Payment, data:%v", tx)
	}
	coreumRecipient := xrpl.DecodeCoreumRecipientFromMemo(paymentTx.Memos)
	if coreumRecipient == nil {
		o.log.Info(ctx, "Bridge memo does not include expected structure", logger.AnyFiled("memos", paymentTx.Memos))
		return nil
	}

	deliveredXRPLAmount := tx.MetaData.DeliveredAmount
	coreumAmount, err := ConvertXRPLNativeTokenAmountToCoreum(*deliveredXRPLAmount)
	if err != nil {
		return err
	}
	if coreumAmount.IsZero() {
		o.log.Info(ctx, "Nothing to send, amount is zero")
		return nil
	}

	currency := deliveredXRPLAmount.Currency.String()
	if len(currency) > 3 {
		currency = hex.EncodeToString([]byte(currency))
	}
	evidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    paymentTx.GetHash().String(),
		Issuer:    deliveredXRPLAmount.Issuer.String(),
		Currency:  currency,
		Amount:    coreumAmount,
		Recipient: coreumRecipient,
	}

	_, err = o.evidencesConsumer.SendXRPLToCoreumTransferEvidence(ctx, o.relayerAddress, evidence)
	if err == nil {
		return nil
	}

	if coreum.IsTokenNotRegisteredError(err) {
		o.log.Info(ctx, "Token not registered")
		return nil
	}

	if coreum.IsEvidenceAlreadyProvidedError(err) {
		o.log.Debug(ctx, "Evidence already provided")
		return nil
	}

	if coreum.IsOperationAlreadyExecutedError(err) {
		o.log.Info(ctx, "Operation already executed")
		return nil
	}

	return err
}

func (o *XRPLTxObserver) processOutgoingTx(ctx context.Context, tx rippledata.TransactionWithMetaData) error {
	o.log.Debug(ctx, "Start processing of XRPL outgoing tx",
		logger.StringFiled("type", tx.GetType()),
	)
	// the func will be implemented later and will contain the implementation of the XRPL tx result confirmation
	return nil
}
