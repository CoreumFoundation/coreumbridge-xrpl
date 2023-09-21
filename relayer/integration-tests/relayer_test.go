//go:build integrationtests
// +build integrationtests

package integrationtests

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/assert"
)

// TODO(dzmitryil) remove that test once we have real test with the coreum chain.
func TestCoreumParamsQuery(t *testing.T) {
	t.Parallel()

	ctx, chains := NewTestingContext(t)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	assert.True(t, issueFee.Amount.GT(sdkmath.ZeroInt()))
	assert.Equal(t, chains.Coreum.ChainSettings.Denom, issueFee.Denom)
}
