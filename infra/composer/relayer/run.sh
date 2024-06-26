#!/bin/sh

set -ex

coreumbridge-xrpl-relayer init \
  --coreum-chain-id coreum-devnet-1 \
  --coreum-contract-address "$CONTRACT_ADDR" \
  --coreum-grpc-url "$COREUM_GRPC_URL" \
  --xrpl-rpc-url "$XRPL_RPC_URL" \
  --metrics-enabled \
  --metrics-listen-addr=:9090

echo "$MNEMONIC_COREUM" | coreumbridge-xrpl-relayer coreum keys add coreum-relayer --recover --keyring-backend=test
echo "$MNEMONIC_XRPL" | coreumbridge-xrpl-relayer xrpl keys add xrpl-relayer --recover --keyring-backend=test

coreumbridge-xrpl-relayer start --keyring-backend=test
