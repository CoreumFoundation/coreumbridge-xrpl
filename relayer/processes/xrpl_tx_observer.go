package processes

import (
	"context"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/tracing"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

// XRPLTxObserverConfig is XRPLTxObserver config.
type XRPLTxObserverConfig struct {
	BridgeAccount  rippledata.Account
	RelayerAddress sdk.AccAddress
}

// XRPLTxObserver is process which observes the XRPL txs and register the evidences in the contract.
type XRPLTxObserver struct {
	cfg            XRPLTxObserverConfig
	log            logger.Logger
	txScanner      XRPLAccountTxScanner
	contractClient ContractClient
}

// NewXRPLTxObserver returns a new instance of the XRPLTxObserver.
func NewXRPLTxObserver(
	cfg XRPLTxObserverConfig,
	log logger.Logger,
	txScanner XRPLAccountTxScanner,
	contractClient ContractClient,
) *XRPLTxObserver {
	return &XRPLTxObserver{
		cfg:            cfg,
		log:            log,
		txScanner:      txScanner,
		contractClient: contractClient,
	}
}

// Init validates the process state.
func (o *XRPLTxObserver) Init(ctx context.Context) error {
	o.log.Debug(ctx, "Initializing process")

	if o.cfg.RelayerAddress.Empty() {
		return errors.Errorf("failed to init process, relayer address is nil or empty")
	}
	if !o.contractClient.IsInitialized() {
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
			return errors.WithStack(ctx.Err())
		case tx := <-txCh:
			if err := o.processTx(ctx, tx); err != nil {
				o.log.Error(ctx, "Failed to process XRPL tx", logger.Error(err))
			}
		}
	}
}

func (o *XRPLTxObserver) processTx(ctx context.Context, tx rippledata.TransactionWithMetaData) error {
	ctx = tracing.WithTracingXRPLTxHash(tracing.WithTracingID(ctx), tx.GetHash().String())
	if !txIsFinal(tx) {
		o.log.Debug(ctx, "Transaction is not final", logger.StringField("txStatus", tx.MetaData.TransactionResult.String()))
		return nil
	}
	if o.cfg.BridgeAccount == tx.GetBase().Account {
		return o.processOutgoingTx(ctx, tx)
	}

	return o.processIncomingTx(ctx, tx)
}

func (o *XRPLTxObserver) processIncomingTx(ctx context.Context, tx rippledata.TransactionWithMetaData) error {
	txType := tx.GetType()
	if !tx.MetaData.TransactionResult.Success() {
		o.log.Debug(
			ctx,
			"Skipping not successful transaction",
			logger.StringField("type", txType),
			logger.StringField("txResult", tx.MetaData.TransactionResult.String()),
		)
		return nil
	}

	o.log.Debug(ctx, "Start processing of XRPL incoming tx", logger.StringField("type", txType))
	// we process only incoming payment transactions, other transactions are ignored
	if txType != rippledata.PAYMENT.String() {
		o.log.Debug(ctx, "Skipping not payment transaction", logger.StringField("type", txType))
		return nil
	}
	paymentTx, ok := tx.Transaction.(*rippledata.Payment)
	if !ok {
		return errors.Errorf("failed to cast tx to Payment, data:%+v", tx)
	}
	coreumRecipient := xrpl.DecodeCoreumRecipientFromMemo(paymentTx.Memos)
	if coreumRecipient == nil {
		o.log.Info(ctx, "Bridge memo does not include expected structure", logger.AnyField("memos", paymentTx.Memos))
		return nil
	}

	deliveredXRPLAmount := tx.MetaData.DeliveredAmount
	coreumAmount, err := ConvertXRPLOriginTokenXRPLAmountToCoreumAmount(*deliveredXRPLAmount)
	if err != nil {
		return err
	}
	if coreumAmount.IsZero() {
		o.log.Info(ctx, "Nothing to send, amount is zero")
		return nil
	}

	evidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    paymentTx.GetHash().String(),
		Issuer:    deliveredXRPLAmount.Issuer.String(),
		Currency:  xrpl.ConvertCurrencyToString(deliveredXRPLAmount.Currency),
		Amount:    coreumAmount,
		Recipient: coreumRecipient,
	}

	_, err = o.contractClient.SendXRPLToCoreumTransferEvidence(ctx, o.cfg.RelayerAddress, evidence)
	if err == nil {
		return nil
	}

	if coreum.IsTokenNotRegisteredError(err) {
		o.log.Info(ctx, "Token not registered")
		return nil
	}

	if IsEvidenceErrorCausedByResubmission(err) {
		o.log.Debug(ctx, "Received expected send evidence error caused by re-submission")
		return nil
	}

	return err
}

func (o *XRPLTxObserver) processOutgoingTx(ctx context.Context, tx rippledata.TransactionWithMetaData) error {
	txType := tx.GetType()
	o.log.Debug(ctx, "Start processing of XRPL outgoing tx",
		logger.StringField("type", txType),
	)

	switch txType {
	case rippledata.TICKET_CREATE.String():
		return o.sendXRPLTicketsAllocationTransactionResultEvidence(ctx, tx)
	case rippledata.TRUST_SET.String():
		return o.sendXRPLTrustSetTransactionResultEvidence(ctx, tx)
	default:
		// TODO(dzmitryhil) replace with the error once we integrate all supported types
		o.log.Warn(ctx, "Found unsupported transaction type", logger.AnyField("tx", tx))
		return nil
	}
}

// IsEvidenceErrorCausedByResubmission returns true is error is cause of the re-submitting of the transaction.
func IsEvidenceErrorCausedByResubmission(err error) bool {
	return coreum.IsEvidenceAlreadyProvidedError(err) ||
		coreum.IsOperationAlreadyExecutedError(err) ||
		coreum.IsPendingOperationNotFoundError(err)
}

func (o *XRPLTxObserver) sendXRPLTicketsAllocationTransactionResultEvidence(ctx context.Context, tx rippledata.TransactionWithMetaData) error {
	tickets := extractTicketSequencesFromMetaData(tx.MetaData)
	txResult := coreum.TransactionResultAccepted
	if !tx.MetaData.TransactionResult.Success() {
		txResult = coreum.TransactionResultRejected
		tickets = nil
	}
	evidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            tx.GetHash().String(),
			TransactionResult: txResult,
		},
		Tickets: tickets,
	}
	ticketCreateTx, ok := tx.Transaction.(*rippledata.TicketCreate)
	if !ok {
		return errors.Errorf("failed to cast tx to TicketCreate, data:%+v", tx)
	}
	if ticketCreateTx.Sequence != 0 {
		evidence.AccountSequence = lo.ToPtr(ticketCreateTx.Sequence)
	}
	if ticketCreateTx.TicketSequence != nil && *ticketCreateTx.TicketSequence != 0 {
		evidence.TicketSequence = lo.ToPtr(*ticketCreateTx.TicketSequence)
	}
	_, err := o.contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
		ctx,
		o.cfg.RelayerAddress,
		evidence,
	)
	if err == nil {
		if evidence.TransactionResult != coreum.TransactionResultAccepted {
			o.log.Warn(ctx, "Transaction was rejected", logger.StringField("txResult", tx.MetaData.TransactionResult.String()))
		}
		return nil
	}
	if IsEvidenceErrorCausedByResubmission(err) {
		o.log.Debug(ctx, "Received expected send evidence error")
		return nil
	}

	return err
}

func (o *XRPLTxObserver) sendXRPLTrustSetTransactionResultEvidence(ctx context.Context, tx rippledata.TransactionWithMetaData) error {
	txResult := coreum.TransactionResultAccepted
	if !tx.MetaData.TransactionResult.Success() {
		txResult = coreum.TransactionResultRejected
	}
	trustSetTx, ok := tx.Transaction.(*rippledata.TrustSet)
	if !ok {
		return errors.Errorf("failed to cast tx to TrustSet, data:%+v", tx)
	}
	evidence := coreum.XRPLTransactionResultTrustSetEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            tx.GetHash().String(),
			TransactionResult: txResult,
			TicketSequence:    trustSetTx.TicketSequence,
		},
		Issuer:   trustSetTx.LimitAmount.Issuer.String(),
		Currency: xrpl.ConvertCurrencyToString(trustSetTx.LimitAmount.Currency),
	}

	_, err := o.contractClient.SendXRPLTrustSetTransactionResultEvidence(
		ctx,
		o.cfg.RelayerAddress,
		evidence,
	)
	if err == nil {
		if evidence.TransactionResult != coreum.TransactionResultAccepted {
			o.log.Warn(ctx, "Transaction was rejected", logger.StringField("txResult", tx.MetaData.TransactionResult.String()))
		}
		return nil
	}
	if IsEvidenceErrorCausedByResubmission(err) {
		o.log.Debug(ctx, "Received expected send evidence error")
		return nil
	}

	return err
}

// txIsFinal returns value which indicates whether the transaction if final and can be used.
// Result Code	 Finality.
// tesSUCCESS	 Final when included in a validated ledger.
// Any tec code	 Final when included in a validated ledger.
// Any tem code	 Final unless the protocol changes to make the transaction valid.
// tefPAST_SEQ	 Final when another transaction with the same sequence number is included in a validated ledger.
// tefMAX_LEDGER Final when a validated ledger has a ledger index higher than the transaction's LastLedgerSequence field, and no validated ledger includes the transaction.
func txIsFinal(tx rippledata.TransactionWithMetaData) bool {
	txResult := tx.MetaData.TransactionResult
	if tx.MetaData.TransactionResult.Success() ||
		strings.HasPrefix(txResult.String(), xrpl.TecTxResultPrefix) ||
		strings.HasPrefix(txResult.String(), xrpl.TemTxResultPrefix) ||
		txResult.String() == xrpl.TefPastSeqTxResult ||
		txResult.String() == xrpl.TefMaxLedgerTxResult {
		return true
	}

	return false
}

func extractTicketSequencesFromMetaData(metaData rippledata.MetaData) []uint32 {
	ticketSequences := make([]uint32, 0)
	for _, node := range metaData.AffectedNodes {
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

		ticketSequences = append(ticketSequences, *ticket.TicketSequence)
	}

	return ticketSequences
}
