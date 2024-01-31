#!/bin/sh

mkdir -p /app_data

echo "Starting the xrpl node."

rippled -a --start --conf /app/rippled.cfg  &

while true; do
  sleep 0.3
  wget -q -O- --post-data '{"method": "ledger_accept", "params": []}' --header='Content-Type:application/json'  http://localhost:5005/
done
