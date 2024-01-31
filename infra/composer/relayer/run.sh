#!/bin/sh

set -ex

coreumbridge-xrpl-relayer init \
  --coreum-chain-id coreum-devnet-1 \
  --coreum-grpc-url http://cored:9090 \
  --xrpl-rpc-url http://xrpld:5005

coreumbridge-xrpl-relayer import-xrpl-faucet-key faucet --keyring-backend=test

# coreum-admin account: devcore1pnv2nwe67tyjlplszmdgch4hvue5vuhe03u8c9
echo "announce already cherry rotate pull apology banana dignity region horse aspect august country exit connect unit agent curious violin tide town link unable whip" | coreumbridge-xrpl-relayer keys-coreum add coreum-admin --recover --keyring-backend=test

# coreum-relayer account: devcore1u6dycnl606n95ggeatusc3zlfd5m4xqpw66et4
echo "mandate canyon major bargain bamboo soft fetch aisle extra confirm monster jazz atom ball summer solar tell glimpse square uniform situate body ginger protect" | coreumbridge-xrpl-relayer keys-coreum add coreum-relayer --recover --keyring-backend=test

# xrpl-admin account: rnNdCGwDs796mMLSaY8BkwtLUFmXHxkyqj
echo "notable rate tribe effort deny void security page regular spice safe prize engage version hour bless normal mother exercise velvet load cry front ordinary" | coreumbridge-xrpl-relayer keys-xrpl add xrpl-admin --recover --keyring-backend=test
coreumbridge-xrpl-relayer send-xrpl --key-name=faucet --keyring-backend=test rnNdCGwDs796mMLSaY8BkwtLUFmXHxkyqj 100000000000

# xrpl-relayer account: rPfgErcqL9XKsAr14cXeCAwxjy9gVGhUeK
echo "move equip digital assault wrong speed border multiply knife steel trash donor isolate remember lucky moon cupboard achieve canyon smooth pulp chief hold symptom" | coreumbridge-xrpl-relayer keys-xrpl add xrpl-relayer --recover --keyring-backend=test
coreumbridge-xrpl-relayer send-xrpl --key-name=faucet --keyring-backend=test rPfgErcqL9XKsAr14cXeCAwxjy9gVGhUeK 100000000000

sleep 10

coreumbridge-xrpl-relayer bootstrap-bridge /app/bootstrap.yaml \
  --keyring-backend=test \
  --xrpl-key-name=xrpl-admin \
  --coreum-key-name=coreum-admin \
  --update-config

coreumbridge-xrpl-relayer start \
  --keyring-backend=test \
  --telemetry-addr=:9090
