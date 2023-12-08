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

### Init relayer default config

The relayer uses `relayer.yaml` for its work. The file contains all required setting which can be adjusted.
To init the default config call.

```bash
./coreumbridge-xrpl-relayer init
```

The command will generate the default `relayer.yaml` config in the `$HOME/.coreumbridge-xrpl-relayer`.
Optionally you can provide `--home` to set different home directory.

## Run relayer in docker

If relayer docker image is not built, build it.

### Add relayer key

```bash
docker run --rm -it -v ${PWD}/keys:/keys coreumbridge-xrpl-relayer:local keys add coreumbridge-xrpl-relayer --keyring-dir /keys
```

### Run relayer

```bash
docker run -it --name coreumbridge-xrpl-relayer -v ${PWD}/keys:/keys coreumbridge-xrpl-relayer:local start --keyring-dir /keys
```

### Restart running instance

```bash
docker restart coreumbridge-xrpl-relayer && docker attach coreumbridge-xrpl-relayer
```

Once you are attached, press any key and enter the keyring password.
