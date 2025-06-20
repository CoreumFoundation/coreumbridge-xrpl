package processes

import (
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/samber/lo"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

// BuildTicketCreateTxForMultiSigning builds TicketCreate transaction operation from the contract operation.
func BuildTicketCreateTxForMultiSigning(
	bridgeXRPLAddress rippledata.Account,
	operation coreum.Operation,
) (*rippledata.TicketCreate, error) {
	tx := rippledata.TicketCreate{
		TxBase: rippledata.TxBase{
			Account:         bridgeXRPLAddress,
			TransactionType: rippledata.TICKET_CREATE,
		},
		TicketCount: &operation.OperationType.AllocateTickets.Number,
	}
	if operation.TicketSequence != 0 {
		tx.TicketSequence = &operation.TicketSequence
	} else {
		tx.Sequence = operation.AccountSequence
	}
	// important for the multi-signing
	tx.SigningPubKey = &rippledata.PublicKey{}

	fee, err := xrpl.GetMultiSigningTxFee(operation.XRPLBaseFee)
	if err != nil {
		return nil, err
	}
	tx.Fee = fee

	return &tx, nil
}

// BuildTrustSetTxForMultiSigning builds TrustSet transaction operation from the contract operation.
func BuildTrustSetTxForMultiSigning(
	bridgeXRPLAddress rippledata.Account,
	operation coreum.Operation,
) (*rippledata.TrustSet, error) {
	trustSetType := operation.OperationType.TrustSet
	value, err := ConvertCoreumAmountToXRPLAmount(
		trustSetType.TrustSetLimitAmount,
		trustSetType.Issuer,
		trustSetType.Currency,
	)
	if err != nil {
		return nil, err
	}
	tx := rippledata.TrustSet{
		TxBase: rippledata.TxBase{
			Account:         bridgeXRPLAddress,
			TransactionType: rippledata.TRUST_SET,
			Flags:           lo.ToPtr(rippledata.TxSetNoRipple),
		},
		LimitAmount: value,
	}
	tx.TicketSequence = &operation.TicketSequence
	// important for the multi-signing
	tx.SigningPubKey = &rippledata.PublicKey{}

	fee, err := xrpl.GetMultiSigningTxFee(operation.XRPLBaseFee)
	if err != nil {
		return nil, err
	}
	tx.Fee = fee

	return &tx, nil
}

// BuildCoreumToXRPLXRPLOriginatedTokenTransferPaymentTxForMultiSigning builds Payment transaction for
// XRPL originated token from the contract operation.
func BuildCoreumToXRPLXRPLOriginatedTokenTransferPaymentTxForMultiSigning(
	bridgeXRPLAddress rippledata.Account,
	operation coreum.Operation,
) (*rippledata.Payment, error) {
	coreumToXRPLTransferOperationType := operation.OperationType.CoreumToXRPLTransfer
	amount, err := ConvertCoreumAmountToXRPLAmount(
		coreumToXRPLTransferOperationType.Amount,
		coreumToXRPLTransferOperationType.Issuer,
		coreumToXRPLTransferOperationType.Currency,
	)
	if err != nil {
		return nil, err
	}
	// if the max amount was provided set it or use nil
	var maxAmount *rippledata.Amount
	if coreumToXRPLTransferOperationType.MaxAmount != nil {
		convertedMaxAmount, err := ConvertCoreumAmountToXRPLAmount(
			*coreumToXRPLTransferOperationType.MaxAmount,
			coreumToXRPLTransferOperationType.Issuer,
			coreumToXRPLTransferOperationType.Currency,
		)
		if err != nil {
			return nil, err
		}
		maxAmount = &convertedMaxAmount
	}

	tx, err := buildPaymentTx(bridgeXRPLAddress, operation, amount, maxAmount)
	if err != nil {
		return nil, err
	}

	return &tx, nil
}

// BuildSignerListSetTxForMultiSigning builds SignerListSet transaction operation from the contract operation.
func BuildSignerListSetTxForMultiSigning(
	bridgeXRPLAddress rippledata.Account,
	operation coreum.Operation,
) (*rippledata.SignerListSet, error) {
	rotateKeysOperationType := operation.OperationType.RotateKeys

	signerEntries := make([]rippledata.SignerEntry, 0, len(rotateKeysOperationType.NewRelayers))
	for _, relayer := range rotateKeysOperationType.NewRelayers {
		xrplRelayerAddress, err := rippledata.NewAccountFromAddress(relayer.XRPLAddress)
		if err != nil {
			return nil, errors.Wrapf(
				err, "faield to convert relayer XRPL address to rippledata.Account, address:%s", relayer.XRPLAddress,
			)
		}
		signerEntries = append(signerEntries, rippledata.SignerEntry{
			SignerEntry: rippledata.SignerEntryItem{
				Account:      xrplRelayerAddress,
				SignerWeight: lo.ToPtr(uint16(1)),
			},
		})
	}

	tx := rippledata.SignerListSet{
		SignerQuorum: uint32(rotateKeysOperationType.NewEvidenceThreshold),
		TxBase: rippledata.TxBase{
			Account:         bridgeXRPLAddress,
			TransactionType: rippledata.SIGNER_LIST_SET,
		},
		SignerEntries: signerEntries,
	}
	if operation.TicketSequence != 0 {
		tx.TicketSequence = &operation.TicketSequence
	} else {
		tx.Sequence = operation.AccountSequence
	}
	// important for the multi-signing
	tx.SigningPubKey = &rippledata.PublicKey{}

	fee, err := xrpl.GetMultiSigningTxFee(operation.XRPLBaseFee)
	if err != nil {
		return nil, err
	}
	tx.Fee = fee

	return &tx, nil
}

func buildPaymentTx(
	bridgeXRPLAddress rippledata.Account,
	operation coreum.Operation,
	amount rippledata.Amount,
	maxAmount *rippledata.Amount,
) (rippledata.Payment, error) {
	recipient, err := rippledata.NewAccountFromAddress(operation.OperationType.CoreumToXRPLTransfer.Recipient)
	if err != nil {
		return rippledata.Payment{}, errors.Wrapf(
			err,
			"failed to convert XRPL recipient to rippledata.Account, recipient:%s",
			operation.OperationType.CoreumToXRPLTransfer.Recipient,
		)
	}
	tx := rippledata.Payment{
		Destination: *recipient,
		TxBase: rippledata.TxBase{
			Account:         bridgeXRPLAddress,
			TransactionType: rippledata.PAYMENT,
		},
		Amount:  amount,
		SendMax: maxAmount,
	}
	tx.TicketSequence = &operation.TicketSequence
	// important for the multi-signing
	tx.SigningPubKey = &rippledata.PublicKey{}

	fee, err := xrpl.GetMultiSigningTxFee(operation.XRPLBaseFee)
	if err != nil {
		return rippledata.Payment{}, err
	}
	tx.Fee = fee
	return tx, nil
}
