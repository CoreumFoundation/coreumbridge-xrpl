package xrpl

import (
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
)

const (
	// DefaultXRPLBaseFee is default XRPL base fee used for transactions.
	DefaultXRPLBaseFee = uint32(10)
)

// GetMultiSigningTxFee is static fee we use for the XRPL transaction submission.
// According to https://xrpl.org/transaction-cost.html multisigned transaction require fee equal to
// xrpl_base_fee * (1 + Number of Signatures Provided).
// For simplicity, we assume that there are maximum 32 signatures.
func GetMultiSigningTxFee(xrplBaseFee uint32) (rippledata.Value, error) {
	fee, err := rippledata.NewNativeValue(int64(xrplBaseFee * (1 + MaxAllowedXRPLSigners)))
	if err != nil {
		return rippledata.Value{}, errors.Wrapf(err, "failed to convert fee to ripple fee")
	}
	return *fee, nil
}

// ComputeXRPLBaseFee computes the required XRPL base with load factor.
// Check https://xrpl.org/transaction-cost.html#server_state for more detail.
func ComputeXRPLBaseFee(baseFee, loadFactor, loadBase uint32) uint32 {
	return (baseFee * loadFactor) / loadBase
}
