# Coreumbridge XRPL

Two-way Coreum XRPL bridge.

## Specification

The specification is described [here](spec/spec.md).

## Build relayer

### Relayer binary

```bash 
make build-relayer
```

### Relayer docker

```bash 
make build-relayer-docker
```

## Init relayer

### Set env variables

```bash
export COREUM_CHAIN_ID={Coreum chain id}
export COREUM_GRPC_URL={Coreum GRPC URL}
export XRPL_RPC_URL={XRPL RPC URL}
```

#### Init the config

```bash
./coreumbridge-xrpl-relayer init --coreum-chain-id $COREUM_CHAIN_ID --coreum-grpc-url $COREUM_GRPC_URL --xrpl-rpc-url $XRPL_RPC_URL
```

## Bootstrap the bridge

### Init relayer (for each relayer)

#### Pass the [Init relayer](#init-relayer) section.

#### Generate the relayer keys

```bash
./coreumbridge-xrpl-relayer keys-coreum add coreum-relayer 

./coreumbridge-xrpl-relayer keys-xrpl add xrpl-relayer 
```

The `coreum-relayer` and `xrpl-relayer` are key names set by default in the `relayer.yaml`. If for some reason you want
to update them, then updated them in the `relayer.yaml` as well.

#### Extract data for the contract deployment

```bash
./coreumbridge-xrpl-relayer relayer-keys-info 
```

Output example:

```bash
2023-12-10T18:04:55.235+0300    info    cli/cli.go:205  Keys info        {"coreumAddress": "core1dukhz42p4qxkrtxg8ap7nj6wn3f2lqjqwf8gny", "xrplAddress": "r3YU6MLbmnxnLwCrRQYBAbaXmBR1RgK5mu", "xrplPubKey": "02ED720F8BF89D333CF7C4EAC763DA6EB7051895924DEB33AD34E87A624FE6B8F0"}
```

The output contains the `coreumAddress`, `xrplAddress` and `xrplPubKey` used for the contract deployment.
Create the account with the `coreumAddress` by sending some tokens to in on the Coreum chain and XRPL account with the
`xrplAddress` by sending 10XRP tokens and to it. Once the accounts are created share the keys info with contract
deployer.

### Run bootstrapping

#### Pass the [Init relayer](#init-relayer) section.

#### Generate new key which will be used for the bridge bootstrapping

```bash
./coreumbridge-xrpl-relayer keys-coreum add bridge-account 
```

#### Fund the Coreum account

```bash
./coreumbridge-xrpl-relayer keys-coreum show -a bridge-account 
```

Get the Coreum address from the output and fund it on the Coreum side.
The balance should cover the token issuance fee and fee for the deployment transaction.

#### Generate config template

```bash
export RELAYERS_COUNT={Relayes count to be used}
./coreumbridge-xrpl-relayer bootstrap-bridge bootstrapping.yaml --key-name bridge-account --init-only --relayers-count $RELAYERS_COUNT 
```

The output will print the XRPL bridge address and min XRPL bridge account balance. Fund it and proceed to the nex step.

#### Modify the `bootstrapping.yaml` config

Collect the config from the relayer and modify the bootstrapping config.

Config example:

```yaml
owner: ""
admin: ""
relayers:
  - coreum_address: ""
    xrpl_address: ""
    xrpl_pub_key: ""
evidence_threshold: 0
used_ticket_sequence_threshold: 150
trust_set_limit_amount: "100000000000000000000000000000000000"
contract_bytecode_path: ""
```

If you don't have the contract bytecode download it.

#### Run the bootstrapping

```bash
./coreumbridge-xrpl-relayer bootstrap-bridge bootstrapping.yaml --key-name bridge-account
```

Once the command is executed get the bridge contract address from the output and share among the relayers to update in
the relayers config.

#### Remove the bridge-account key

```bash
./coreumbridge-xrpl-relayer keys delete bridge-account 
```

#### Run all relayers

Run all relayers see [Run relayer](#run-relayer) section.

#### Recover tickets

Use [Recover tickets](#recover-tickets) instruction and recover tickets.

### Run relayer

#### Run relayer using binary

```bash
./coreumbridge-xrpl-relayer start --keyring-dir $HOME/.coreumbridge-xrpl-relayer/keys
```

#### Run relayer in docker

If relayer docker image is not built, build it.

##### Run relayer

```bash
docker run -dit --name coreumbridge-xrpl-relayer \
  -v $HOME/.coreumbridge-xrpl-relayer:/.coreumbridge-xrpl-relayer \
  coreumbridge-xrpl-relayer:local start \
  --home /.coreumbridge-xrpl-relayer
  
docker attach coreumbridge-xrpl-relayer  
```

Once you are attached, press any key and enter the keyring password.
It is expected that at that time the relayer is initialized and its keys are generated and accounts are funded.

##### Restart running instance

```bash
docker restart coreumbridge-xrpl-relayer && docker attach coreumbridge-xrpl-relayer
```

Once you are attached, press any key and enter the keyring password.

## Public CLI

### Pass the [Init relayer](#init-relayer) section.

Additionally, set the bridge contract address in the `relayer.yaml`

### Send from coreum to XRPL

```bash 
./coreumbridge-xrpl-relayer send-from-coreum-to-xrpl 1000000ucore rrrrrrrrrrrrrrrrrrrrrhoLvTp --key-name sender --keyring-dir $HOME/.coreumbridge-xrpl-relayer/keys
```

### Send from XRPL to coreum

```bash 
./coreumbridge-xrpl-relayer send-from-xrpl-to-coreum 1000000 rrrrrrrrrrrrrrrrrrrrrhoLvTp XRP testcore1adst6w4e79tddzhcgaru2l2gms8jjep6a4caa7 --key-name sender --keyring-dir $HOME/.coreumbridge-xrpl-relayer/keys
```

### Get contact config

```bash
./coreumbridge-xrpl-relayer contract-config
```

### Get all registered tokens

```bash 
./coreumbridge-xrpl-relayer registered-tokens
```

### Get Coreum balances

```bash 
./coreumbridge-xrpl-relayer coreum-balances testcore1adst6w4e79tddzhcgaru2l2gms8jjep6a4caa7
```

### Get XRPL balances

```bash 
./coreumbridge-xrpl-relayer xrpl-balances rrrrrrrrrrrrrrrrrrrrrhoLvTp
```

### Set XRPL TrustSet

```bash 
./coreumbridge-xrpl-relayer set-xrpl-trust-set 1e80 rrrrrrrrrrrrrrrrrrrrrhoLvTp XRP --key-name sender --keyring-dir $HOME/.coreumbridge-xrpl-relayer/keys
```

## Owner CLI

### Pass the [Init relayer](#init-relayer) section.

Additionally, set the bridge contract address in the `relayer.yaml`

### Recover tickets to allow XRPL to coreum operations

```bash
./coreumbridge-xrpl-relayer recovery-tickets --key-name owner --keyring-dir $HOME/.coreumbridge-xrpl-relayer/keys
```

### Register Coreum token

```bash
./coreumbridge-xrpl-relayer register-coreum-token ucore 6 2 500000000000000 --key-name owner --keyring-dir $HOME/.coreumbridge-xrpl-relayer/keys
```

### Register XRPL token

```bash
./coreumbridge-xrpl-relayer register-xrpl-token rcoreNywaoz2ZCQ8Lg2EbSLnGuRBmun6D 434F524500000000000000000000000000000000 2 500000000000000 --key-name owner --keyring-dir $HOME/.coreumbridge-xrpl-relayer/keys
```
