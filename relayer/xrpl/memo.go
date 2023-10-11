package xrpl

import (
	"encoding/base64"
	"encoding/json"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pkg/errors"
	rippledata "github.com/rubblelabs/ripple/data"
)

const (
	// BridgeMemoType is the string type with version of the current memo, used in the memo json we expect.
	BridgeMemoType = "coreumbridge-xrpl-v1"
)

// BridgeMemo is struct we expect to be in the memo to indicate that the operation is the bridge related.
//
//nolint:tagliatelle // we use the same style as the contract
type BridgeMemo struct {
	Type            string `json:"type"`
	CoreumRecipient string `json:"coreum_recipient"`
}

// DecodeCoreumRecipientFromMemo decodes the coreum recipient form memo or returns nil in case the memo is not as expected.
func DecodeCoreumRecipientFromMemo(memos rippledata.Memos) sdk.AccAddress {
	var bridgeMemo BridgeMemo
	for _, memo := range memos {
		if len(memo.Memo.MemoData) == 0 {
			continue
		}

		data, err := base64.StdEncoding.DecodeString(string(memo.Memo.MemoData.Bytes()))
		if err != nil {
			return nil
		}

		if err := json.Unmarshal(data, &bridgeMemo); err != nil {
			return nil
		}
		if bridgeMemo.Type != BridgeMemoType {
			return nil
		}
		acc, err := sdk.AccAddressFromBech32(bridgeMemo.CoreumRecipient)
		if err != nil {
			return nil
		}

		return acc
	}

	return nil
}

// EncodeCoreumRecipientToMemo encodes the bridge memo with the coreum recipient.
func EncodeCoreumRecipientToMemo(coreumRecipient sdk.AccAddress) (rippledata.Memo, error) {
	data, err := json.Marshal(BridgeMemo{
		Type:            BridgeMemoType,
		CoreumRecipient: coreumRecipient.String(),
	})
	if err != nil {
		return rippledata.Memo{}, errors.Wrapf(err, "failed to marshal BridgeMemo")
	}
	base64Data := base64.StdEncoding.EncodeToString(data)
	return rippledata.Memo{
		Memo: rippledata.MemoItem{
			MemoData: []byte(base64Data),
		},
	}, nil
}
