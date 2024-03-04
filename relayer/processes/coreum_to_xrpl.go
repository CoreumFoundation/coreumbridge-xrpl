package processes

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"
	"go.uber.org/zap"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/logger"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

// MultiSignableTransaction is XRPL multi-singable transaction type.
type MultiSignableTransaction interface {
	rippledata.MultiSignable
	rippledata.Transaction
}

// BridgeSigners full signers info.
type BridgeSigners struct {
	XRPLWeights         map[rippledata.Account]uint16
	XRPLWeightsQuorum   uint32
	XRPLPubKeys         map[rippledata.Account]rippledata.PublicKey
	CoreumToXRPLAccount map[string]rippledata.Account
}

// CoreumToXRPLProcessConfig is the CoreumToXRPLProcess config.
type CoreumToXRPLProcessConfig struct {
	BridgeXRPLAddress    rippledata.Account
	RelayerCoreumAddress sdk.AccAddress
	XRPLTxSignerKeyName  string
	RepeatRecentScan     bool
	RepeatDelay          time.Duration
}

// DefaultCoreumToXRPLProcessConfig returns the default CoreumToXRPLProcess.
func DefaultCoreumToXRPLProcessConfig(
	bridgeXRPLAddress rippledata.Account,
	relayerAddress sdk.AccAddress,
) CoreumToXRPLProcessConfig {
	return CoreumToXRPLProcessConfig{
		BridgeXRPLAddress:    bridgeXRPLAddress,
		RelayerCoreumAddress: relayerAddress,
		RepeatRecentScan:     true,
		RepeatDelay:          10 * time.Second,
	}
}

// CoreumToXRPLProcess is process which observes pending XRPL operations, signs them and executes them.
type CoreumToXRPLProcess struct {
	cfg            CoreumToXRPLProcessConfig
	log            logger.Logger
	contractClient ContractClient
	xrplRPCClient  XRPLRPCClient
	xrplSigner     XRPLTxSigner
	metricRegistry MetricRegistry
}

// NewCoreumToXRPLProcess returns a new instance of the CoreumToXRPLProcess.
func NewCoreumToXRPLProcess(
	cfg CoreumToXRPLProcessConfig,
	log logger.Logger,
	contractClient ContractClient,
	xrplRPCClient XRPLRPCClient,
	xrplSigner XRPLTxSigner,
	metricRegistry MetricRegistry,
) (*CoreumToXRPLProcess, error) {
	if cfg.RelayerCoreumAddress.Empty() {
		return nil, errors.Errorf("failed to init process, relayer address is nil or empty")
	}
	if !contractClient.IsInitialized() {
		return nil, errors.Errorf("failed to init process, contract client is not initialized")
	}
	if xrplSigner == nil {
		return nil, errors.Errorf("nil xrplSigner")
	}

	return &CoreumToXRPLProcess{
		cfg:            cfg,
		log:            log,
		contractClient: contractClient,
		xrplRPCClient:  xrplRPCClient,
		xrplSigner:     xrplSigner,
		metricRegistry: metricRegistry,
	}, nil
}

// Start starts the process.
func (p *CoreumToXRPLProcess) Start(ctx context.Context) error {
	p.log.Info(ctx, "Starting Coreum to XRPL process")
	for {
		select {
		case <-ctx.Done():
			return errors.WithStack(ctx.Err())
		default:
			if err := p.processPendingOperations(ctx); err != nil && !errors.Is(err, context.Canceled) {
				return errors.Wrap(err, "failed to process pending operations")
			}
			if !p.cfg.RepeatRecentScan {
				p.log.Info(ctx, "Process repeating is disabled, process is finished")
				return nil
			}
			p.log.Debug(ctx, "Waiting before the next execution", zap.String("delay", p.cfg.RepeatDelay.String()))
			select {
			case <-ctx.Done():
				return errors.WithStack(ctx.Err())
			case <-time.After(p.cfg.RepeatDelay):
			}
		}
	}
}

func (p *CoreumToXRPLProcess) processPendingOperations(ctx context.Context) error {
	operations, err := p.contractClient.GetPendingOperations(ctx)
	if err != nil {
		return err
	}
	if len(operations) == 0 {
		p.log.Debug(ctx, "No pending operations to process")
		return nil
	}
	p.log.Debug(ctx, "Processing pending operations", zap.Int("count", len(operations)))

	bridgeSigners, err := p.getBridgeSigners(ctx)
	if err != nil {
		return err
	}

	for _, operation := range operations {
		if err := p.signOrSubmitOperation(ctx, operation, bridgeSigners); err != nil {
			p.log.Error(
				ctx,
				"Failed to process pending operation, skipping processing",
				zap.Error(err),
				zap.Any("operation", operation),
			)
			continue
		}
	}

	return nil
}

func (p *CoreumToXRPLProcess) getBridgeSigners(ctx context.Context) (BridgeSigners, error) {
	xrplWeights, xrplWeightsQuorum, err := p.getBridgeXRPLSignerAccountsWithWeights(ctx)
	if err != nil {
		return BridgeSigners{}, err
	}
	contractConfig, err := p.contractClient.GetContractConfig(ctx)
	if err != nil {
		return BridgeSigners{}, err
	}

	xrplPubKeys := make(map[rippledata.Account]rippledata.PublicKey, 0)
	coreumToXRPLAccount := make(map[string]rippledata.Account, 0)
	for _, relayer := range contractConfig.Relayers {
		xrplAcc, err := rippledata.NewAccountFromAddress(relayer.XRPLAddress)
		if err != nil {
			return BridgeSigners{}, errors.Wrapf(
				err,
				"failed to covert XRPL relayer address to Account type, address:%s",
				relayer.XRPLAddress,
			)
		}
		var accPubKey rippledata.PublicKey
		if err := accPubKey.UnmarshalText([]byte(relayer.XRPLPubKey)); err != nil {
			return BridgeSigners{}, errors.Wrapf(
				err,
				"failed to unmarshal XRPL relayer pubkey, address:%s, pubKey:%s",
				relayer.XRPLAddress,
				relayer.XRPLPubKey,
			)
		}

		xrplPubKeys[*xrplAcc] = accPubKey
		coreumToXRPLAccount[relayer.CoreumAddress.String()] = *xrplAcc
	}

	return BridgeSigners{
		XRPLWeights:         xrplWeights,
		XRPLWeightsQuorum:   xrplWeightsQuorum,
		XRPLPubKeys:         xrplPubKeys,
		CoreumToXRPLAccount: coreumToXRPLAccount,
	}, nil
}

func (p *CoreumToXRPLProcess) getBridgeXRPLSignerAccountsWithWeights(
	ctx context.Context,
) (map[rippledata.Account]uint16, uint32, error) {
	accountInfo, err := p.xrplRPCClient.AccountInfo(ctx, p.cfg.BridgeXRPLAddress)
	if err != nil {
		return nil, 0, err
	}
	signerList := accountInfo.AccountData.SignerList
	if len(signerList) != 1 {
		return nil, 0, errors.Errorf("received unexpected length of the signer list")
	}
	signerData := accountInfo.AccountData.SignerList[0]
	weightsQuorum := *signerData.SignerQuorum
	accountWights := make(map[rippledata.Account]uint16, 0)
	for _, signerEntry := range signerData.SignerEntries {
		accountWights[*signerEntry.SignerEntry.Account] = *signerEntry.SignerEntry.SignerWeight
	}

	return accountWights, weightsQuorum, nil
}

func (p *CoreumToXRPLProcess) signOrSubmitOperation(
	ctx context.Context,
	operation coreum.Operation,
	bridgeSigners BridgeSigners,
) error {
	valid, err := p.preValidateOperation(ctx, operation)
	if err != nil {
		return err
	}
	if !valid {
		p.log.Warn(ctx, "Operation is invalid", zap.Any("operation", operation))
		return nil
	}
	p.log.Debug(
		ctx,
		"Pre-validation of the operation passed, operation is valid",
		zap.Any("operation", operation),
	)

	tx, quorumIsReached, err := p.buildSubmittableTransaction(ctx, operation, bridgeSigners)
	if err != nil {
		return err
	}
	if !quorumIsReached {
		return p.registerTxSignature(ctx, operation)
	}

	txRes, err := p.xrplRPCClient.Submit(ctx, tx)
	if err != nil {
		return errors.Wrapf(err, "failed to submit transaction:%+v", tx)
	}
	if txRes.EngineResult.Success() {
		p.log.Info(
			ctx,
			"XRPL multi-sign transaction has been successfully submitted",
			zap.String("txHash", strings.ToUpper(tx.GetHash().String())),
			zap.Any("tx", tx),
		)
		return nil
	}
	// These codes indicate that the transaction failed, but it was applied to a ledger to apply the transaction cost.
	if strings.HasPrefix(txRes.EngineResult.String(), xrpl.TecTxResultPrefix) {
		p.log.Debug(
			ctx,
			fmt.Sprintf(
				"The transaction has been sent, but will be reverted, code:%s, description:%s",
				txRes.EngineResult.String(), txRes.EngineResult.Human(),
			),
		)
		return nil
	}

	switch txRes.EngineResult {
	case rippledata.TefNO_TICKET, rippledata.TefPAST_SEQ:
		p.log.Debug(
			ctx,
			"Transaction has been already submitted",
		)
		return nil
	case rippledata.TelINSUF_FEE_P:
		p.log.Warn(
			ctx,
			"The Fee from the transaction is not high enough to meet the server's current transaction cost requirement.",
		)
		return nil
	default:
		return errors.Errorf("failed to submit transaction, received unexpected result, code:%s result:%+v, tx:%+v",
			txRes.EngineResult.String(), txRes, tx)
	}
}

func (p *CoreumToXRPLProcess) buildSubmittableTransaction(
	ctx context.Context,
	operation coreum.Operation,
	bridgeSigners BridgeSigners,
) (MultiSignableTransaction, bool, error) {
	txSigners := make([]rippledata.Signer, 0)
	signedWeight := uint32(0)
	signingThresholdIsReached := false
	for _, signature := range operation.Signatures {
		xrplAcc, ok := bridgeSigners.CoreumToXRPLAccount[signature.RelayerCoreumAddress.String()]
		if !ok {
			p.log.Warn(ctx, "Found unknown signer", zap.String("coreumAddress", signature.RelayerCoreumAddress.String()))
			continue
		}
		xrplPubKey, ok := bridgeSigners.XRPLPubKeys[xrplAcc]
		if !ok {
			p.log.Warn(
				ctx,
				"Found XRPL signer address without pub key in the contract",
				zap.String("xrplAddress", xrplAcc.String()),
			)
			continue
		}
		xrplAccWeight, ok := bridgeSigners.XRPLWeights[xrplAcc]
		if !ok {
			p.log.Warn(ctx, "Found XRPL signer address without weight", zap.String("xrplAddress", xrplAcc.String()))
			continue
		}
		var txSignature rippledata.VariableLength
		if err := txSignature.UnmarshalText([]byte(signature.Signature)); err != nil {
			p.registerInvalidSignatureMetric(operation.GetOperationID(), signature)
			p.log.Error(
				ctx,
				"Failed to unmarshal tx signature",
				zap.Error(err), zap.String("signature", signature.Signature),
				zap.String("xrplAddress", xrplAcc.String()), zap.String("coreumAddress", signature.RelayerCoreumAddress.String()),
			)
			continue
		}
		txSigner := rippledata.Signer{
			Signer: rippledata.SignerItem{
				Account:       xrplAcc,
				TxnSignature:  &txSignature,
				SigningPubKey: &xrplPubKey,
			},
		}
		tx, err := p.buildXRPLTxFromOperation(operation)
		if err != nil {
			return nil, false, err
		}
		if err := rippledata.SetSigners(tx, txSigner); err != nil {
			return nil, false, errors.Errorf("failed to set tx signer, signer:%+v", txSigner)
		}
		isValid, _, err := rippledata.CheckMultiSignature(tx)
		if err != nil {
			p.registerInvalidSignatureMetric(operation.GetOperationID(), signature)
			p.log.Error(
				ctx,
				"failed to check transaction signature, err:%s, signer:%+v",
				zap.Error(err),
				zap.Any("signer", txSigner),
				zap.String("xrplAddress", xrplAcc.String()), zap.String("coreumAddress", signature.RelayerCoreumAddress.String()),
			)
			continue
		}
		if !isValid {
			p.registerInvalidSignatureMetric(operation.GetOperationID(), signature)
			p.log.Error(
				ctx,
				"Invalid tx signature",
				zap.Error(err),
				zap.Any("txSigner", txSigner),
				zap.String("xrplAddress", xrplAcc.String()), zap.String("coreumAddress", signature.RelayerCoreumAddress.String()),
			)
			continue
		}
		txSigners = append(txSigners, txSigner)
		signedWeight += uint32(xrplAccWeight)
		// the fewer signatures we use the less fee we pay
		if signedWeight >= bridgeSigners.XRPLWeightsQuorum {
			signingThresholdIsReached = true
			break
		}
	}
	// quorum is not reached
	if !signingThresholdIsReached {
		return nil, false, nil
	}
	// build tx one more time to be sure that it is not affected
	tx, err := p.buildXRPLTxFromOperation(operation)
	if err != nil {
		return nil, false, err
	}
	if err := rippledata.SetSigners(tx, txSigners...); err != nil {
		return nil, false, errors.Errorf("failed to set tx signer, signeres:%+v", txSigners)
	}

	return tx, true, nil
}

// preValidateOperation checks if the operation is valid, and it makes sense to submit the corresponding transaction
// or the operation should be canceled with the `invalid` state. For now the main purpose of the function is to filter
// out the `AllocateTickets` operation with the invalid sequence.
func (p *CoreumToXRPLProcess) preValidateOperation(ctx context.Context, operation coreum.Operation) (bool, error) {
	// no need to check if the current relayer has already provided the signature
	// this check prevents the state when relayer votes and then changes its vote because of different current state
	for _, signature := range operation.Signatures {
		if signature.RelayerCoreumAddress.String() == p.cfg.RelayerCoreumAddress.String() {
			return true, nil
		}
	}

	// currently we validate only the allocate tickets operation with not zero sequence
	if operation.OperationType.AllocateTickets == nil ||
		operation.OperationType.AllocateTickets.Number == 0 ||
		operation.AccountSequence == 0 {
		return true, nil
	}

	bridgeXRPLAccInfo, err := p.xrplRPCClient.AccountInfo(ctx, p.cfg.BridgeXRPLAddress)
	if err != nil {
		return false, err
	}
	// sequence is valid
	if *bridgeXRPLAccInfo.AccountData.Sequence == operation.AccountSequence {
		return true, nil
	}
	p.log.Info(
		ctx,
		"Invalid bridge account sequence",
		zap.Uint32("expected", *bridgeXRPLAccInfo.AccountData.Sequence),
		zap.Uint32("inOperation", operation.AccountSequence),
	)
	evidence := coreum.XRPLTransactionResultTicketsAllocationEvidence{
		XRPLTransactionResultEvidence: coreum.XRPLTransactionResultEvidence{
			TransactionResult: coreum.TransactionResultInvalid,
			AccountSequence:   lo.ToPtr(operation.AccountSequence),
			// we intentionally don't set the ticket number since it is unexpected to have invalid
			// tx with the ticket number
		},
	}
	p.log.Info(ctx, "Sending invalid tx evidence")
	_, err = p.contractClient.SendXRPLTicketsAllocationTransactionResultEvidence(ctx, p.cfg.RelayerCoreumAddress, evidence)
	if err == nil {
		return false, nil
	}
	if IsExpectedEvidenceSubmissionError(err) {
		p.log.Debug(ctx, "Received expected evidence submission error", zap.String("errText", err.Error()))
		return false, nil
	}

	return false, nil
}

func (p *CoreumToXRPLProcess) registerInvalidSignatureMetric(operationID uint32, signature coreum.Signature) {
	p.metricRegistry.SetMaliciousBehaviourKey(
		fmt.Sprintf(
			"invalid_signature_for_operation_%d_relayer_%s",
			operationID, signature.RelayerCoreumAddress.String(),
		),
	)
}

func (p *CoreumToXRPLProcess) registerTxSignature(ctx context.Context, operation coreum.Operation) error {
	tx, err := p.buildXRPLTxFromOperation(operation)
	if err != nil {
		return err
	}
	signer, err := p.xrplSigner.MultiSign(tx, p.cfg.XRPLTxSignerKeyName)
	if err != nil {
		return errors.Wrapf(err, "failed to sign transaction, keyName:%s", p.cfg.XRPLTxSignerKeyName)
	}
	_, err = p.contractClient.SaveSignature(
		ctx,
		p.cfg.RelayerCoreumAddress,
		operation.GetOperationID(),
		operation.Version,
		signer.Signer.TxnSignature.String(),
	)
	if err == nil {
		p.log.Info(
			ctx,
			"Signature registered for the operation",
			zap.String("signature", signer.Signer.TxnSignature.String()),
			zap.Any("operation", operation),
		)
		return nil
	}
	if coreum.IsSignatureAlreadyProvidedError(err) ||
		coreum.IsPendingOperationNotFoundError(err) ||
		coreum.IsOperationVersionMismatchError(err) ||
		coreum.IsBridgeHaltedError(err) {
		p.log.Debug(
			ctx,
			"Received expected evidence error on saving signature",
			zap.String("errText", err.Error()),
		)

		return nil
	}

	return errors.Wrap(err, "failed to register transaction signature")
}

func (p *CoreumToXRPLProcess) buildXRPLTxFromOperation(operation coreum.Operation) (MultiSignableTransaction, error) {
	switch {
	case isAllocateTicketsOperation(operation):
		return BuildTicketCreateTxForMultiSigning(p.cfg.BridgeXRPLAddress, operation)
	case isTrustSetOperation(operation):
		return BuildTrustSetTxForMultiSigning(p.cfg.BridgeXRPLAddress, operation)
	case isCoreumToXRPLTransferOperation(operation):
		return BuildCoreumToXRPLXRPLOriginatedTokenTransferPaymentTxForMultiSigning(p.cfg.BridgeXRPLAddress, operation)
	case isRotateKeysOperation(operation):
		return BuildSignerListSetTxForMultiSigning(p.cfg.BridgeXRPLAddress, operation)
	default:
		return nil, errors.Errorf("failed to process operation, unable to determine operation type, operation:%+v", operation)
	}
}

func isAllocateTicketsOperation(operation coreum.Operation) bool {
	return operation.OperationType.AllocateTickets != nil &&
		operation.OperationType.AllocateTickets.Number > 0
}

func isTrustSetOperation(operation coreum.Operation) bool {
	return operation.OperationType.TrustSet != nil &&
		operation.OperationType.TrustSet.Issuer != "" &&
		operation.OperationType.TrustSet.Currency != ""
}

func isCoreumToXRPLTransferOperation(operation coreum.Operation) bool {
	return operation.OperationType.CoreumToXRPLTransfer != nil &&
		operation.OperationType.CoreumToXRPLTransfer.Issuer != "" &&
		operation.OperationType.CoreumToXRPLTransfer.Currency != "" &&
		!operation.OperationType.CoreumToXRPLTransfer.Amount.IsZero() &&
		operation.OperationType.CoreumToXRPLTransfer.Recipient != ""
}

func isRotateKeysOperation(operation coreum.Operation) bool {
	return operation.OperationType.RotateKeys != nil &&
		len(operation.OperationType.RotateKeys.NewRelayers) != 0 &&
		operation.OperationType.RotateKeys.NewEvidenceThreshold > 0
}
