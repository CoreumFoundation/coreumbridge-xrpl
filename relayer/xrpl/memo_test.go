package xrpl_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cosmos/cosmos-sdk/types"
	rippledata "github.com/rubblelabs/ripple/data"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestDecodeCoreumRecipientFromMemo(t *testing.T) {
	t.Parallel()

	accAddress := coreum.GenAccount()
	staticJSONMemo := fmt.Sprintf("{\"type\":\"coreumbridge-xrpl-v1\",\"coreum_recipient\":\"%s\"}", accAddress.String())

	tests := []struct {
		name  string
		memos rippledata.Memos
		want  types.AccAddress
	}{
		{
			name:  "valid_memo",
			memos: encodeToCoreumBridgeMemos(t, xrpl.BridgeMemoType, accAddress.String()),
			want:  accAddress,
		},
		{
			name: "valid_static_memo",
			memos: rippledata.Memos{
				{
					Memo: rippledata.MemoItem{
						MemoData: rippledata.VariableLength(staticJSONMemo),
					},
				},
			},
			want: accAddress,
		},
		{
			name:  "invalid_type",
			memos: encodeToCoreumBridgeMemos(t, "invalid", accAddress.String()),
			want:  nil,
		},
		{
			name:  "invalid_address",
			memos: encodeToCoreumBridgeMemos(t, xrpl.BridgeMemoType, "coreum123"),
			want:  nil,
		},
		{
			name: "invalid_structure",
			memos: rippledata.Memos{
				rippledata.Memo{
					Memo: rippledata.MemoItem{
						MemoData: []byte("invalid"),
					},
				},
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, xrpl.DecodeCoreumRecipientFromMemo(tt.memos))
		})
	}
}

func encodeToCoreumBridgeMemos(t *testing.T, mtype, address string) rippledata.Memos {
	t.Helper()

	data, err := json.Marshal(xrpl.BridgeMemo{
		Type:            mtype,
		CoreumRecipient: address,
	})
	require.NoError(t, err)
	return rippledata.Memos{
		rippledata.Memo{
			Memo: rippledata.MemoItem{
				MemoData: data,
			},
		},
	}
}
