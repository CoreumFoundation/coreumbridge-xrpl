package xrpl

import (
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
)

// XRPLFee is static fee we use for the XRPL transaction submission.
// According to https://xrpl.org/transaction-cost.html multisigned transaction require fee equal to
// 10 drops * (1 + Number of Signatures Provided).
// For simplicity, we assume that there are maximum 32 signatures.
const XRPLFee = "330"

// GetTxFee returns the fee required for the transaction.
func GetTxFee(_ rippledata.Transaction) (rippledata.Value, error) {
	fee, err := rippledata.NewValue(XRPLFee, true)
	if err != nil {
		return rippledata.Value{}, errors.Wrapf(err, "failed to convert fee to ripple fee")
	}
	return *fee, nil
}
