module github.com/CoreumFoundation/xrpl-bridge-v2/relayer

go 1.21.0

// TODO remove once PR with the changes is accepped
replace github.com/rubblelabs/ripple => ../../../dzmitryhil/rubblelabs-ripple

require (
	github.com/CoreumFoundation/coreum-tools v0.4.0
	github.com/pkg/errors v0.9.1
	github.com/rubblelabs/ripple v0.0.0-20230906094910-7739c2f347e3
	github.com/samber/lo v1.38.1
	github.com/stretchr/testify v1.8.4
)

require (
	github.com/bits-and-blooms/bitset v1.2.1 // indirect
	github.com/btcsuite/btcd/btcec/v2 v2.1.3 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/golang/glog v1.0.0 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/crypto v0.0.0-20211117183948-ae814b36b871 // indirect
	golang.org/x/exp v0.0.0-20230817173708-d852ddb80c63 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
