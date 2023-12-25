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

## Bootstrap the bridge

### Init relayer (for each relayer)

#### Set env variables used in the following instruction

```bash
export COREUM_CHAIN_ID={Coreum chain id}
export COREUM_GRPC_URL={Coreum GRPC URL}
export XRPL_RPC_URL={XRPL RPC URL}
```

#### Init the config and generate the relayer keys

```bash
./coreumbridge-xrpl-relayer init --coreum-chain-id $COREUM_CHAIN_ID --coreum-grpc-url $COREUM_GRPC_URL  --xrpl-rpc-url $XRPL_RPC_URL
./coreumbridge-xrpl-relayer keys add coreum-relayer 
./coreumbridge-xrpl-relayer keys add xrpl-relayer 
```

The `coreum-relayer` and `xrpl-relayer` are key names set by default in the `relayer.yaml`. If for some reason you want
to update them, then updated them in the `relayer.yaml` as well.

#### Extract data for the contract deployment

```bash
./coreumbridge-xrpl-relayer relayer-keys-info 
```

Output example:

```bash
2023-12-10T18:04:55.235+0300    info    cli/cli.go:205  Key info        {"coreumAddress": "core1dukhz42p4qxkrtxg8ap7nj6wn3f2lqjqwf8gny", "xrplAddress": "r3YU6MLbmnxnLwCrRQYBAbaXmBR1RgK5mu", "xrplPubKey": "02ED720F8BF89D333CF7C4EAC763DA6EB7051895924DEB33AD34E87A624FE6B8F0"}
```

The output contains the `coreumAddress`, `xrplAddress` and `xrplPubKey` used for the contract deployment.
Create the account with the `coreumAddress` by sending some tokens to in on the Coreum chain and XRPL account with the
`xrplAddress` by sending 10XRP tokens and to it. Once the accounts are created share the keys info with contract
deployer.

### Run bootstrapping

#### Generate new key which will be used for the bridge bootstrapping

```bash
./coreumbridge-xrpl-relayer keys add bridge-account 
```

#### Fund the Coreum account

```bash
./coreumbridge-xrpl-relayer keys show -a bridge-account 
```

Get the Coreum address from the output and fund it on the Coreum side.
The balance should cover the token issuance fee and fee for the deployment transaction.

#### Generate config template

```bash
export RELAYERS_COUNT={Relayes count to be used}
./coreumbridge-xrpl-relayer bootstrap-bridge bootstraping.yaml --key-name bridge-account --init-only --relayers-count $RELAYERS_COUNT 
```

The output will print the XRPL bridge address and min XRPL bridge account balance. Fund it and proceed to the nex step.

#### Modify the `bootstraping.yaml` config

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
skip_xrpl_balance_validation: false
```

If you don't have the contract bytecode download it.

#### Run the bootstrapping

```bash
./coreumbridge-xrpl-relayer bootstrap-bridge bootstraping.yaml --key-name bridge-account
```

Once the command is executed get the bridge contract address from the output and share among the relayers to update in
the relayers config.

#### Remove the bridge-account key

```bash
./coreumbridge-xrpl-relayer keys delete bridge-account 
```

## Run relayer in docker

If relayer docker image is not built, build it.

### Run relayer

### Run relayer

```bash
docker run -dit --name coreumbridge-xrpl-relayer \
  -v $HOME/.coreumbridge-xrpl-relayer:/.coreumbridge-xrpl-relayer \
  coreumbridge-xrpl-relayer:local start \
  --home /.coreumbridge-xrpl-relayer
  
docker attach coreumbridge-xrpl-relayer  
```

Once you are attached, press any key and enter the keyring password.
It is expected that at that time the relayer is initialized and its keys are generated and accounts are funded.

### Restart running instance

```bash
docker restart coreumbridge-xrpl-relayer && docker attach coreumbridge-xrpl-relayer
```

Once you are attached, press any key and enter the keyring password.
