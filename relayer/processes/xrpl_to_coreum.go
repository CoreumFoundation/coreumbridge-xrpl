package processes

import (
	"context"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"go.uber.org/zap"

	"github.com/CoreumFoundation/coreum-tools/pkg/parallel"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/tracing"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

// XRPLToCoreumProcessConfig is XRPLToCoreumProcess config.
type XRPLToCoreumProcessConfig struct {
	BridgeXRPLAddress    rippledata.Account
	RelayerCoreumAddress sdk.AccAddress
}

// XRPLToCoreumProcess is process which observes the XRPL txs and register the evidences in the contract.
type XRPLToCoreumProcess struct {
	cfg            XRPLToCoreumProcessConfig
	log            logger.Logger
	txScanner      XRPLAccountTxScanner
	contractClient ContractClient
}

// NewXRPLToCoreumProcess returns a new instance of the XRPLToCoreumProcess.
func NewXRPLToCoreumProcess(
	cfg XRPLToCoreumProcessConfig,
	log logger.Logger,
	txScanner XRPLAccountTxScanner,
	contractClient ContractClient,
) (*XRPLToCoreumProcess, error) {
	if cfg.RelayerCoreumAddress.Empty() {
		return nil, errors.Errorf("failed to init process, relayer address is nil or empty")
	}
	if !contractClient.IsInitialized() {
		return nil, errors.Errorf("failed to init process, contract client is not initialized")
	}

	return &XRPLToCoreumProcess{
		cfg:            cfg,
		log:            log,
		txScanner:      txScanner,
		contractClient: contractClient,
	}, nil
}

// Start starts the process.
func (o *XRPLToCoreumProcess) Start(ctx context.Context) error {
	txCh := make(chan rippledata.TransactionWithMetaData)
	return parallel.Run(ctx, func(ctx context.Context, spawn parallel.SpawnFn) error {
		spawn("tx-scanner", parallel.Continue, func(ctx context.Context) error {
			defer close(txCh)
			return o.txScanner.ScanTxs(ctx, txCh)
		})
		spawn("tx-processor", parallel.Fail, func(ctx context.Context) error {
			for tx := range txCh {
				if err := o.processTx(ctx, tx); err != nil {
					if errors.Is(err, context.Canceled) {
						o.log.Warn(ctx, "Context canceled during the XRPL tx processing", zap.String("error", err.Error()))
					} else {
						return errors.Wrapf(err, "failed to process XRPL tx, txHash:%s", strings.ToUpper(tx.GetHash().String()))
					}
				}
			}
			return errors.WithStack(ctx.Err())
		})

		return nil
	}, parallel.WithGroupLogger(o.log))
}

func (o *XRPLToCoreumProcess) processTx(ctx context.Context, tx rippledata.TransactionWithMetaData) error {
	ctx = tracing.WithTracingXRPLTxHash(tracing.WithTracingID(ctx), strings.ToUpper(tx.GetHash().String()))
	if !txIsFinal(tx) {
		o.log.Debug(ctx, "Transaction is not final", zap.String("txStatus", tx.MetaData.TransactionResult.String()))
		return nil
	}
	if o.cfg.BridgeXRPLAddress == tx.GetBase().Account {
		return o.processOutgoingTx(ctx, tx)
	}

	return o.processIncomingTx(ctx, tx)
}

func (o *XRPLToCoreumProcess) processIncomingTx(ctx context.Context, tx rippledata.TransactionWithMetaData) error {
	txType := tx.GetType()
	if !tx.MetaData.TransactionResult.Success() {
		o.log.Debug(
			ctx,
			"Skipping not successful transaction",
			zap.String("type", txType),
			zap.String("txResult", tx.MetaData.TransactionResult.String()),
		)
		return nil
	}

	o.log.Debug(ctx, "Start processing of XRPL incoming tx", zap.String("type", txType))
	// we process only incoming payment transactions, other transactions are ignored
	if txType != rippledata.PAYMENT.String() {
		o.log.Debug(ctx, "Skipping not payment transaction", zap.String("type", txType))
		return nil
	}
	paymentTx, ok := tx.Transaction.(*rippledata.Payment)
	if !ok {
		return errors.Errorf("failed to cast tx to Payment, data:%+v", tx)
	}
	coreumRecipient := xrpl.DecodeCoreumRecipientFromMemo(paymentTx.Memos)
	if coreumRecipient == nil {
		o.log.Info(ctx, "Bridge memo does not include expected structure", zap.Any("memos", paymentTx.Memos))
		return nil
	}

	deliveredXRPLAmount := tx.MetaData.DeliveredAmount

	coreumAmount, err := ConvertXRPLAmountToCoreumAmount(*deliveredXRPLAmount)
	if err != nil {
		if errors.Is(err, ErrSDKMathIntOutOfBounds) || errors.Is(err, ErrContractUint128OutOfBounds) {
			o.log.Info(
				ctx,
				"Found XRPL transaction with out of bounds amount",
				zap.String("amount", deliveredXRPLAmount.String()),
			)
			return nil
		}
		return err
	}

	if coreumAmount.IsZero() {
		o.log.Info(ctx, "Nothing to send, amount is zero")
		return nil
	}

	evidence := coreum.XRPLToCoreumTransferEvidence{
		TxHash:    strings.ToUpper(paymentTx.GetHash().String()),
		Issuer:    deliveredXRPLAmount.Issuer.String(),
		Currency:  xrpl.ConvertCurrencyToString(deliveredXRPLAmount.Currency),
		Amount:    coreumAmount,
		Recipient: coreumRecipient,
	}

	_, err = o.contractClient.SendXRPLToCoreumTransferEvidence(ctx, o.cfg.RelayerCoreumAddress, evidence)
	if err == nil {
		o.log.Info(ctx, "Successfully sent XRPL to Coreum transfer evidence", zap.Any("evidence", evidence))
		return nil
	}

	if coreum.IsTokenNotRegisteredError(err) {
		o.log.Info(ctx, "Token not registered")
		return nil
	}

	if IsExpectedEvidenceSubmissionError(err) {
		o.log.Debug(ctx, "Received expected evidence submission error", zap.String("errText", err.Error()))
		return nil
	}

	if coreum.IsAssetFTStateError(err) {
		o.log.Info(
			ctx,
			"The evidence saving is failed because of the asset FT rules, the evidence is skipped",
			zap.Any("evidence", evidence),
		)
		return nil
	}

	if coreum.IsRecipientBlockedError(err) {
		o.log.Info(
			ctx,
			"The evidence saving is failed because of the recipient address is blocked, the evidence is skipped",
			zap.Any("evidence", evidence),
		)
		return nil
	}

	return err
}

func (o *XRPLToCoreumProcess) processOutgoingTx(ctx context.Context, tx rippledata.TransactionWithMetaData) error {
	txType := tx.GetType()
	o.log.Debug(ctx, "Start processing of XRPL outgoing tx",
		zap.String("type", txType),
	)

	switch txType {
	case rippledata.TICKET_CREATE.String():
		return o.sendXRPLTicketsAllocationTransactionResultEvidence(ctx, tx)
	case rippledata.TRUST_SET.String():
		return o.sendXRPLTrustSetTransactionResultEvidence(ctx, tx)
	case rippledata.PAYMENT.String():
		return o.sendCoreumToXRPLTransferTransactionResultEvidence(ctx, tx)
	case rippledata.SIGNER_LIST_SET.String():
		return o.sendKeysRotationTransactionResultEvidence(ctx, tx)
	// types which we use initially for the account set up
	case rippledata.ACCOUNT_SET.String():
		o.log.Debug(ctx, "Skipped expected tx type", zap.String("txType", txType), zap.Any("tx", tx))
		return nil
	default:
		o.log.Error(ctx, "Found unsupported transaction type", zap.Any("tx", tx))
		return nil
	}
}

func (o *XRPLToCoreumProcess) sendXRPLTicketsAllocationTransactionResultEvidence(
	ctx context.Context,
	tx rippledata.TransactionWithMetaData,
) error {
	tickets := extractTicketSequencesFromMetaData(tx.MetaData)
	txResult := getTransactionResult(tx)
	if txResult == coreum.TransactionResultRejected {
		tickets = nil
	}
	evidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            strings.ToUpper(tx.GetHash().String()),
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
		o.cfg.RelayerCoreumAddress,
		evidence,
	)

	return o.handleOperationEvidenceSubmissionError(ctx, err, tx, evidence.XRPLTransactionResultEvidence)
}

func (o *XRPLToCoreumProcess) sendXRPLTrustSetTransactionResultEvidence(
	ctx context.Context,
	tx rippledata.TransactionWithMetaData,
) error {
	trustSetTx, ok := tx.Transaction.(*rippledata.TrustSet)
	if !ok {
		return errors.Errorf("failed to cast tx to TrustSet, data:%+v", tx)
	}
	evidence := coreum.XRPLTransactionResultTrustSetEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            strings.ToUpper(tx.GetHash().String()),
			TransactionResult: getTransactionResult(tx),
			TicketSequence:    trustSetTx.TicketSequence,
		},
	}

	_, err := o.contractClient.SendXRPLTrustSetTransactionResultEvidence(
		ctx,
		o.cfg.RelayerCoreumAddress,
		evidence,
	)

	return o.handleOperationEvidenceSubmissionError(ctx, err, tx, evidence.XRPLTransactionResultEvidence)
}

func (o *XRPLToCoreumProcess) sendCoreumToXRPLTransferTransactionResultEvidence(
	ctx context.Context,
	tx rippledata.TransactionWithMetaData,
) error {
	paymentTx, ok := tx.Transaction.(*rippledata.Payment)
	if !ok {
		return errors.Errorf("failed to cast tx to Payment, data:%+v", tx)
	}
	evidence := coreum.XRPLTransactionResultCoreumToXRPLTransferEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            strings.ToUpper(tx.GetHash().String()),
			TransactionResult: getTransactionResult(tx),
			TicketSequence:    paymentTx.TicketSequence,
		},
	}

	_, err := o.contractClient.SendCoreumToXRPLTransferTransactionResultEvidence(
		ctx,
		o.cfg.RelayerCoreumAddress,
		evidence,
	)

	return o.handleOperationEvidenceSubmissionError(ctx, err, tx, evidence.XRPLTransactionResultEvidence)
}

func (o *XRPLToCoreumProcess) sendKeysRotationTransactionResultEvidence(
	ctx context.Context,
	tx rippledata.TransactionWithMetaData,
) error {
	signerListSetTx, ok := tx.Transaction.(*rippledata.SignerListSet)
	if !ok {
		return errors.Errorf("failed to cast tx to SignerListSet, data:%+v", tx)
	}
	evidence := coreum.XRPLTransactionResultKeysRotationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TxHash:            strings.ToUpper(tx.GetHash().String()),
			TransactionResult: getTransactionResult(tx),
		},
	}
	// handle the case when the tx was set initially to set up the bridge
	if signerListSetTx.Sequence != 0 {
		o.log.Debug(
			ctx,
			"Skipping the evidence sending for the tx, since the SignerListSet tx contains account sequence.",
			zap.Any("tx", tx),
		)
		return nil
	}
	if signerListSetTx.TicketSequence != nil && *signerListSetTx.TicketSequence != 0 {
		evidence.TicketSequence = lo.ToPtr(*signerListSetTx.TicketSequence)
	}
	_, err := o.contractClient.SendKeysRotationTransactionResultEvidence(
		ctx,
		o.cfg.RelayerCoreumAddress,
		evidence,
	)

	return o.handleOperationEvidenceSubmissionError(ctx, err, tx, evidence.XRPLTransactionResultEvidence)
}

func (o *XRPLToCoreumProcess) handleOperationEvidenceSubmissionError(
	ctx context.Context,
	err error,
	tx rippledata.TransactionWithMetaData,
	evidence coreum.XRPLTransactionResultEvidence,
) error {
	if err == nil {
		o.log.Info(
			ctx,
			"Successfully sent operation evidence",
			zap.String("txResult", tx.MetaData.TransactionResult.String()),
			zap.Any("evidence", evidence),
		)
		return nil
	}
	if IsExpectedEvidenceSubmissionError(err) {
		o.log.Debug(ctx, "Received expected evidence submission error", zap.String("errText", err.Error()))
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
// tefMAX_LEDGER Final when a validated ledger has a ledger index higher than the transaction's LastLedgerSequence
// field, and no validated ledger includes the transaction.
func txIsFinal(tx rippledata.TransactionWithMetaData) bool {
	txResult := tx.MetaData.TransactionResult
	return tx.MetaData.TransactionResult.Success() ||
		strings.HasPrefix(txResult.String(), xrpl.TecTxResultPrefix) ||
		strings.HasPrefix(txResult.String(), xrpl.TemTxResultPrefix) ||
		txResult.String() == xrpl.TefPastSeqTxResult ||
		txResult.String() == xrpl.TefMaxLedgerTxResult
}

func getTransactionResult(tx rippledata.TransactionWithMetaData) coreum.TransactionResult {
	if tx.MetaData.TransactionResult.Success() {
		return coreum.TransactionResultAccepted
	}
	return coreum.TransactionResultRejected
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
