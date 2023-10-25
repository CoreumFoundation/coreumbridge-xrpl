package processes

import (
	"context"

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
	coreumAmount, err := ConvertXRPLNativeTokenAmountToCoreum(*deliveredXRPLAmount)
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
		Currency:  deliveredXRPLAmount.Currency.String(),
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

	if coreum.IsEvidenceAlreadyProvidedError(err) {
		o.log.Debug(ctx, "Evidence already provided")
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
		tickets := extractTicketSequencesFromMetaData(tx.MetaData)
		evidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
			XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
				TxHash:    tx.GetHash().String(),
				Confirmed: tx.MetaData.TransactionResult.Success(),
			},
			Tickets: tickets,
		}
		ticketCreateTx, ok := tx.Transaction.(*rippledata.TicketCreate)
		if !ok {
			return errors.Errorf("failed to cast tx to TicketCreate, data:%+v", tx)
		}
		if ticketCreateTx.Sequence != 0 {
			evidence.SequenceNumber = lo.ToPtr(ticketCreateTx.Sequence)
		}
		if ticketCreateTx.TicketSequence != nil && *ticketCreateTx.TicketSequence != 0 {
			evidence.TicketNumber = lo.ToPtr(*ticketCreateTx.TicketSequence)
		}
		_, err := o.contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(
			ctx,
			o.cfg.RelayerAddress,
			evidence,
		)
		if err == nil {
			if !evidence.Confirmed {
				o.log.Warn(ctx, "Transaction was rejected", logger.StringField("txResult", tx.MetaData.TransactionResult.String()))
			}
			return nil
		}
		if coreum.IsEvidenceAlreadyProvidedError(err) {
			o.log.Info(ctx, "Evidence already provided")
			return nil
		}
		if coreum.IsOperationAlreadyExecutedError(err) {
			o.log.Info(ctx, "Operation already executed")
			return nil
		}

		return err

	default:
		// TODO(dzmitryhil) replace with the error once we integrate all supported types
		o.log.Warn(ctx, "Found unsupported transaction type", logger.AnyField("tx", tx))
		return nil
	}
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
