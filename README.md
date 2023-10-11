# Coreumbrdige XRPL

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
