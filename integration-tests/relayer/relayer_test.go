//go:build integrationtests
// +build integrationtests

package relayer_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/assert"

	integrationtests "github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests"
)

// TODO(dzmitryil) remove that test once we have real test with the coreum chain.
func TestCoreumParamsQuery(t *testing.T) {
	t.Parallel()

	ctx, chains := integrationtests.NewTestingContext(t)
	issueFee := chains.Coreum.QueryAssetFTParams(ctx, t).IssueFee

	assert.True(t, issueFee.Amount.GT(sdkmath.ZeroInt()))
	assert.Equal(t, chains.Coreum.ChainSettings.Denom, issueFee.Denom)
}
