package cli_test

import (
	"testing"

	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/coreum"
	"github.com/CoreumFoundation/coreumbridge-xrpl/relayer/runner"
)

func TestMain(m *testing.M) {
	coreum.SetSDKConfig(string(runner.DefaultCoreumChainID))
}
