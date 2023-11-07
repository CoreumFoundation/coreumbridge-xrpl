package processes

import (
	rippledata "github.com/rubblelabs/ripple/data"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
)

// BuildTicketCreateTxForMultiSigning builds TicketCreate transaction operation from the contract operation.
func BuildTicketCreateTxForMultiSigning(bridgeAccount rippledata.Account, operation coreum.Operation) (*rippledata.TicketCreate, error) {
	tx := rippledata.TicketCreate{
		TxBase: rippledata.TxBase{
			Account:         bridgeAccount,
			TransactionType: rippledata.TICKET_CREATE,
		},
		TicketCount: &operation.OperationType.AllocateTickets.Number,
	}
	if operation.TicketNumber != 0 {
		tx.TicketSequence = &operation.TicketNumber
	} else {
		tx.TxBase.Sequence = operation.SequenceNumber
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
func BuildTrustSetTxForMultiSigning(bridgeAccount rippledata.Account, operation coreum.Operation) (*rippledata.TrustSet, error) {
	trustSetType := operation.OperationType.TrustSet
	value, err := ConvertXRPLNativeTokenCoreumAmountToXRPLAmount(
		trustSetType.TrustSetLimitAmount,
		trustSetType.Issuer,
		trustSetType.Currency,
	)
	if err != nil {
		return nil, err
	}
	tx := rippledata.TrustSet{
		TxBase: rippledata.TxBase{
			Account:         bridgeAccount,
			TransactionType: rippledata.TRUST_SET,
		},
		LimitAmount: value,
	}
	tx.TicketSequence = &operation.TicketNumber
	// important for the multi-signing
	tx.TxBase.SigningPubKey = &rippledata.PublicKey{}

	fee, err := GetTxFee(&tx)
	if err != nil {
		return nil, err
	}
	tx.TxBase.Fee = fee

	return &tx, nil
}
