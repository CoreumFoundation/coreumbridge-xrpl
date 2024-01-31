#!/bin/sh

if [ ! -d "/app_data" ]; then
    cp -a "/app" "/app_data"
fi

echo "Starting the cored node."

exec cored start \
  --home /app_data \
  --log_level info \
  --trace \
  --chain-id coreum-devnet-1 \
  --minimum-gas-prices 0.000000000000000001udevcore \
  --wasm.memory_cache_size 100 \
  --wasm.query_gas_limit 3000000
