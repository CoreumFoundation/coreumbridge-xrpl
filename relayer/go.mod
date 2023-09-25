module github.com/CoreumFoundation/coreumbridge-xrpl/relayer

go 1.21

// same replacements as in coreum
replace (
	// cosmos keyring
	github.com/99designs/keyring => github.com/cosmos/keyring v1.2.0
	// dgrijalva/jwt-go is deprecated and doesn't receive security updates.
	// TODO: remove it: https://github.com/cosmos/cosmos-sdk/issues/13134
	github.com/dgrijalva/jwt-go => github.com/golang-jwt/jwt/v4 v4.4.2
	// Fix upstream GHSA-h395-qcrw-5vmq vulnerability.
	// TODO Remove it: https://github.com/cosmos/cosmos-sdk/issues/10409
	github.com/gin-gonic/gin => github.com/gin-gonic/gin v1.9.0
	// https://github.com/cosmos/cosmos-sdk/issues/14949
	// pin the version of goleveldb to v1.0.1-0.20210819022825-2ae1ddf74ef7 required by SDK v47 upgrade guide.
	github.com/syndtr/goleveldb => github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7
)

require (
	github.com/CoreumFoundation/coreum-tools v0.4.1-0.20230920110418-b30366f1b19b
	github.com/golang/mock v1.6.0
	github.com/rubblelabs/ripple v0.0.0-20230908201244-7f73b1fe5e22
	github.com/samber/lo v1.38.1
	github.com/stretchr/testify v1.8.4
	go.uber.org/zap v1.23.0
)

require (
	github.com/bits-and-blooms/bitset v1.2.1 // indirect
	github.com/btcsuite/btcd/btcec/v2 v2.3.2 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.1.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/pkg/errors v0.9.1
	github.com/rogpeppe/go-internal v1.11.0 // indirect
	go.uber.org/atomic v1.10.0 // indirect
	go.uber.org/multierr v1.8.0 // indirect
	golang.org/x/crypto v0.11.0 // indirect
	golang.org/x/exp v0.0.0-20230711153332-06a737ee72cb // indirect
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
