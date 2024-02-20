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
export RELEASE_VERSION={Relayer release version}
```

### Install relayer (with docker)

For the document simplicity we use the alias for the command which will be executed in docker.
Pay attention that all files outputs are related to docker container.

```bash
alias coreumbridge-xrpl-relayer="docker run --user $(id -u):$(id -g) -it --rm -v $HOME/.coreumbridge-xrpl-relayer:/root/.coreumbridge-xrpl-relayer coreumfoundation/coreumbridge-xrpl-relayer:$RELEASE_VERSION"
```

### Init relayer config

```bash
coreumbridge-xrpl-relayer init \
    --coreum-chain-id $COREUM_CHAIN_ID \
    --coreum-grpc-url $COREUM_GRPC_URL  \
    --xrpl-rpc-url $XRPL_RPC_URL   
```

## Bootstrap the bridge

### Init relayer

#### Pass the [Init relayer](#init-relayer ) section.

#### Generate the relayer keys

Generate new keys:

```bash
coreumbridge-xrpl-relayer keys-coreum add coreum-relayer

coreumbridge-xrpl-relayer keys-xrpl add xrpl-relayer
```

!!! Save output mnemonics to a safe place to be able to recover the relayer later. !!!

The `coreum-relayer` and `xrpl-relayer` are key names set by default in the `relayer.yaml`. If for some reason you want
to update them, then updated them in the `relayer.yaml` as well.

Or import the existing mnemonics:
```bash
coreumbridge-xrpl-relayer keys-coreum add coreum-relayer --recover

coreumbridge-xrpl-relayer keys-xrpl add xrpl-relayer --recover
```

#### Extract keys info for the contract deployment

```bash
coreumbridge-xrpl-relayer relayer-keys-info
```

Output example:

```bash
Keys info
    coreumAddress: "testcore1lwzy78a7ulernmvdgvjyagaslsmp7x7g496jj4"
    xrplAddress: "r41Cc8WLZMeUvZfvB4Fc4hRjpHya4T4Nqq"
    xrplPubKey: "022ED182ACEBFE4C55CE0A0EA561468C31336F9B4E71FB487FC84D94A2826F1C10"
```

The output contains the `coreumAddress`, `xrplAddress` and `xrplPubKey` used for the contract deployment.
Create the account with the `coreumAddress` by sending some tokens to in on the Coreum chain and XRPL account with the
`xrplAddress` by sending 10XRP tokens and to it. Once the accounts are created share the keys info with contract
deployer.

### Run bootstrapping

#### Pass the [Init relayer](#init-relayer) section.

#### Generate new key which will be used for the XRPL bridge account creation

```bash
coreumbridge-xrpl-relayer keys-xrpl add bridge-account
```

#### Generate new key which will be used for the contract deployment

```bash
coreumbridge-xrpl-relayer keys-coreum add contract-deployer
```

Send some core tokens to the generated address, to have enough for the contract deployment.

#### Generate config template

```bash
coreumbridge-xrpl-relayer bootstrap-bridge /root/.coreumbridge-xrpl-relayer/bootstrapping.yaml \
  --xrpl-key-name bridge-account --coreum-key-name contract-deployer --init-only --relayers-count 32
```

The output will print the XRPL bridge address and min XRPL bridge account balance. Fund it and proceed to the nex step.

Output example:

```bash
XRPL bridge address
    address: "rDtBdHaGpZpgQ4vEZv3nKujhudd5kUHVQ"
Coreum deployer address
    address: "testcore1qfhm09t9wyf5ttuj9e52v90h7rhrk72zwjxv5l"
Initializing default bootstrapping config
    path: "/root/.coreumbridge-xrpl-relayer/bootstrapping.yaml"
Computed minimum XRPL bridge balance
    balance: 594
```

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
evidence_threshold: 2
used_ticket_sequence_threshold: 150
trust_set_limit_amount: "340000000000000000000000000000000000000"
contract_bytecode_path: ""
xrpl_base_fee: 10
```

If you don't have the contract bytecode download it.

#### Run the bootstrapping

```bash
coreumbridge-xrpl-relayer bootstrap-bridge /root/.coreumbridge-xrpl-relayer/bootstrapping.yaml \
  --xrpl-key-name bridge-account --coreum-key-name contract-deployer
```

Once the command is executed get the bridge contract address from the output and share among the relayers to update in
the relayers config.

#### Remove the bridge-account key

```bash
coreumbridge-xrpl-relayer xrpl-keys delete bridge-account 
```

#### Run all relayers

Run all relayers see [Run relayer](#run-relayer) section.

#### Recover tickets (initial tickets set)

```bash
coreumbridge-xrpl-relayer recover-tickets --tickets-to-allocate 250 --key-name owner
```

### Run relayer

#### Run relayer with docker

If relayer docker image is not built, build it.

##### Set up release version

```bash
export RELEASE_VERSION={Relayer release version}
```

##### Run

```bash
docker run -dit --name coreumbridge-xrpl-relayer \
    -v $HOME/.coreumbridge-xrpl-relayer:/root/.coreumbridge-xrpl-relayer \
    coreumfoundation/coreumbridge-xrpl-relayer:$RELEASE_VERSION \
    start
  
docker attach coreumbridge-xrpl-relayer
```

Once you are attached, press any key and enter the keyring password.
It is expected that at that time the relayer is initialized and its keys are generated and accounts are funded.

##### Restart running instance

```bash
docker restart coreumbridge-xrpl-relayer && docker attach coreumbridge-xrpl-relayer
```

Once you are attached, press any key and enter the keyring password.
