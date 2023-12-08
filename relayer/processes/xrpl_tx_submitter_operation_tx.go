package processes

import (
	"fmt"

	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
)

// BuildTicketCreateTxForMultiSigning builds TicketCreate transaction operation from the contract operation.
//
//nolint:lll // TODO(dzmitryhil) linter length limit
func BuildTicketCreateTxForMultiSigning(bridgeXRPLAddress rippledata.Account, operation coreum.Operation) (*rippledata.TicketCreate, error) {
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
		tx.TxBase.Sequence = operation.AccountSequence
	}
	// important for the multi-signing
	tx.TxBase.SigningPubKey = &rippledata.PublicKey{}

	fee, err := GetTxFee(&tx)
	if err != nil {
		return nil, err
	}
	tx.TxBase.Fee = fee

	return &tx, nil
}

// BuildTrustSetTxForMultiSigning builds TrustSet transaction operation from the contract operation.
//
//nolint:lll // TODO(dzmitryhil) linter length limit
func BuildTrustSetTxForMultiSigning(bridgeXRPLAddress rippledata.Account, operation coreum.Operation) (*rippledata.TrustSet, error) {
	trustSetType := operation.OperationType.TrustSet
	value, err := ConvertXRPLOriginatedTokenCoreumAmountToXRPLAmount(
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
		},
		LimitAmount: value,
	}
	tx.TicketSequence = &operation.TicketSequence
	// important for the multi-signing
	tx.TxBase.SigningPubKey = &rippledata.PublicKey{}

	fee, err := GetTxFee(&tx)
	if err != nil {
		return nil, err
	}
	tx.TxBase.Fee = fee

	return &tx, nil
}

// BuildCoreumToXRPLXRPLOriginatedTokenTransferPaymentTxForMultiSigning builds Payment transaction for XRPL originated token from the contract operation.
//
//nolint:lll // TODO(dzmitryhil) linter length limit
func BuildCoreumToXRPLXRPLOriginatedTokenTransferPaymentTxForMultiSigning(bridgeXRPLAddress rippledata.Account, operation coreum.Operation) (*rippledata.Payment, error) {
	coreumToXRPLTransferOperationType := operation.OperationType.CoreumToXRPLTransfer
	value, err := ConvertXRPLOriginatedTokenCoreumAmountToXRPLAmount(
		coreumToXRPLTransferOperationType.Amount,
		coreumToXRPLTransferOperationType.Issuer,
		coreumToXRPLTransferOperationType.Currency,
	)
	if err != nil {
		return nil, err
	}

	tx, err := buildPaymentTx(bridgeXRPLAddress, operation, value)
	if err != nil {
		return nil, err
	}

	return &tx, nil
}

// BuildCoreumToXRPLCoreumOriginatedTokenTransferPaymentTxForMultiSigning builds Payment transaction for coreum originated token from the contract operation.
//
//nolint:lll // TODO(dzmitryhil) linter length limit
func BuildCoreumToXRPLCoreumOriginatedTokenTransferPaymentTxForMultiSigning(
	bridgeXRPLAddress rippledata.Account,
	operation coreum.Operation,
) (*rippledata.Payment, error) {
	coreumToXRPLTransferOperationType := operation.OperationType.CoreumToXRPLTransfer
	value, err := ConvertXRPLOriginatedTokenCoreumAmountToXRPLAmount(
		coreumToXRPLTransferOperationType.Amount,
		coreumToXRPLTransferOperationType.Issuer,
		coreumToXRPLTransferOperationType.Currency,
	)
	if err != nil {
		return nil, err
	}

	tx, err := buildPaymentTx(bridgeXRPLAddress, operation, value)
	if err != nil {
		return nil, err
	}

	return &tx, nil
}

//nolint:lll // TODO(dzmitryhil) linter length limit
func buildPaymentTx(
	bridgeXRPLAddress rippledata.Account,
	operation coreum.Operation,
	value rippledata.Amount,
) (rippledata.Payment, error) {
	recipient, err := rippledata.NewAccountFromAddress(operation.OperationType.CoreumToXRPLTransfer.Recipient)
	if err != nil {
		return rippledata.Payment{}, errors.Wrap(err, fmt.Sprintf("failed to convert XRPL recipient to rippledata.Account, recipient:%s", operation.OperationType.CoreumToXRPLTransfer.Recipient))
	}
	tx := rippledata.Payment{
		Destination: *recipient,
		TxBase: rippledata.TxBase{
			Account:         bridgeXRPLAddress,
			TransactionType: rippledata.PAYMENT,
		},
		Amount: value,
	}
	tx.TicketSequence = &operation.TicketSequence
	// important for the multi-signing
	tx.TxBase.SigningPubKey = &rippledata.PublicKey{}

	fee, err := GetTxFee(&tx)
	if err != nil {
		return rippledata.Payment{}, err
	}
	tx.TxBase.Fee = fee
	return tx, nil
}
