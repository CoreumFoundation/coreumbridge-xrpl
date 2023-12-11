package xrpl_test

import (
	"testing"

	"github.com/rubblelabs/ripple/crypto"
	"github.com/stretchr/testify/require"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/xrpl"
)

func TestXRP_ConstantsStringer(t *testing.T) {
	require.Equal(t, crypto.ACCOUNT_ZERO, xrpl.XRPTokenIssuer.String())
	require.Equal(t, "XRP", xrpl.XRPTokenCurrency.String())
}
