package xrpl

import (
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
)

// XRPLFee is static fee we use for the XRPL transaction submission.
// Currently, it is fixed as a constant but might be updated in the future.
const XRPLFee = "100"

// GetTxFee returns the fee required for the transaction.
func GetTxFee(_ rippledata.Transaction) (rippledata.Value, error) {
	fee, err := rippledata.NewValue(XRPLFee, true)
	if err != nil {
		return rippledata.Value{}, errors.Wrapf(err, "failed to convert fee to ripple fee")
	}
	return *fee, nil
}
