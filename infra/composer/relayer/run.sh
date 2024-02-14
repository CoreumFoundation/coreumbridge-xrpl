#!/bin/sh

set -ex

ZNET_IP="172.18.0.1"
CONTRACT_ADDR="devcore14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sd4f0ak"

# coreum-relayer account: devcore1d3et2s6wy0ltc0ju6zxtejhgkxnn3ykzgmq4gp
MNEMONIC_COREUM="dice quick social basic morning defense birth silly embrace fatal tornado couple truck age obtain drama wheel mountain wreck umbrella spider present perfect large"

# xrpl-relayer account: r48jPfLuH4oWXkq7TXxxbNTxU7Ca212ZT3
MNEMONIC_XRPL="goat fish barrel afford voice coil injury run trade retire solution unique lawn oil cattle lazy audit joke long grace income neglect mail sell"

coreumbridge-xrpl-relayer init \
  --coreum-chain-id coreum-devnet-1 \
  --coreum-contract-address "$CONTRACT_ADDR" \
  --coreum-grpc-url "http://$ZNET_IP:9090" \
  --xrpl-rpc-url "http://$ZNET_IP:5005" \
  --metrics-enable \
  --metrics-listen-addr=:9090


echo "$MNEMONIC_COREUM" | coreumbridge-xrpl-relayer keys-coreum add coreum-relayer --recover --keyring-backend=test
echo "$MNEMONIC_XRPL" | coreumbridge-xrpl-relayer keys-xrpl add xrpl-relayer --recover --keyring-backend=test

coreumbridge-xrpl-relayer start --keyring-backend=test
