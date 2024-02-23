module github.com/CoreumFoundation/coreumbridge-xrpl/build

go 1.21

require (
	github.com/CoreumFoundation/coreum-tools v0.4.1-0.20240223065112-97400ae15d8e
	// FIXME (wojciech): Replace with the new commit ID before merging once
	// https://reviewable.io/reviews/CoreumFoundation/crust/366 is merged
	github.com/CoreumFoundation/crust/build v0.0.0-20240223105832-a89ef59d6437
	github.com/pkg/errors v0.9.1
	go.uber.org/zap v1.26.0
	golang.org/x/mod v0.14.0
)

require (
	github.com/BurntSushi/toml v1.3.2 // indirect
	github.com/samber/lo v1.39.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	// Make sure to not bump x/exp dependency without cosmos-sdk updated because their breaking change is not compatible
	// with cosmos-sdk v0.47.
	// Details: https://github.com/cosmos/cosmos-sdk/issues/18415
	golang.org/x/exp v0.0.0-20230713183714-613f0c0eb8a1 // indirect
)