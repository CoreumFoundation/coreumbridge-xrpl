package xrpl

import (
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
)

// GetTxFee returns the fee required for the transaction.
func GetTxFee(signatureCount uint32) (rippledata.Value, error) {
	// According to https://xrpl.org/transaction-cost.html multisigned transaction require fee equal to
	// 10 drops * (1 + Number of Signatures Provided).
	// For simplicity, we decided that the weight of each signature in the multisig account is always set to 1.
	// Algorithm always collect the minimum number of signatures required to fulfill the quorum requirement.
	// Those two statements together mean that the number of signatures is always equal to the quorum weight configured
	// for the multisig account. That's why code calling this function passes `xrplWeightsQuorum` as `signatureCount`.

	fee, err := rippledata.NewNativeValue((1 + int64(signatureCount)) * 10)
	if err != nil {
		return rippledata.Value{}, errors.Wrapf(err, "failed to compute fee")
	}
	return *fee, nil
}
