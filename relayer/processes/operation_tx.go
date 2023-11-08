package processes

import (
	rippledata "github.com/rubblelabs/ripple/data"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
)

// BuildTicketCreateTxForMultiSigning builds TicketCreate transaction operation from the contract operation.
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
func BuildTrustSetTxForMultiSigning(bridgeXRPLAddress rippledata.Account, operation coreum.Operation) (*rippledata.TrustSet, error) {
	trustSetType := operation.OperationType.TrustSet
	value, err := ConvertXRPLOriginTokenCoreumAmountToXRPLAmount(
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
