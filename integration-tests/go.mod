module github.com/CoreumFoundation/coreumbridge-xrpl/integration-tests

go 1.21

replace github.com/CoreumFoundation/coreumbridge-xrpl/relayer => ../relayer

require (
	// FIXME update after the PR merge
	github.com/CoreumFoundation/coreum-tools v0.4.1-0.20230919073720-bbe20b4ce859
	github.com/CoreumFoundation/coreumbridge-xrpl/relayer v1.0.0
	github.com/pkg/errors v0.9.1
	github.com/rubblelabs/ripple v0.0.0-20230908201244-7f73b1fe5e22
	github.com/samber/lo v1.38.1
	github.com/stretchr/testify v1.8.4
	go.uber.org/zap v1.23.0
)

require (
	github.com/benbjohnson/clock v1.1.0 // indirect
	github.com/bits-and-blooms/bitset v1.2.1 // indirect
	github.com/btcsuite/btcd/btcec/v2 v2.1.3 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	go.uber.org/atomic v1.9.0 // indirect
	go.uber.org/multierr v1.8.0 // indirect
	golang.org/x/crypto v0.0.0-20211117183948-ae814b36b871 // indirect
	golang.org/x/exp v0.0.0-20230817173708-d852ddb80c63 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
