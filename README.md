# Coreumbridge XRPL

Two-way Coreum <-> XRPL bridge.

## Specification

The specification is described [here](spec/spec.md).

## Relayer

### Build relayer binary

```bash 
make build-relayer
```

### Build relayer docker

```bash 
make build-relayer-docker
```

## Contract

### Build dev contract (fast build)

```bash 
make build-dev-contract
```

### Build contract in docker

```bash 
make build-contract
```

## Dev environment

* Set-up [crust](https://github.com/CoreumFoundation/crust) and [coreum](https://github.com/CoreumFoundation/coreum)

As a reference on how to set it up and run for development you can use
the [relayer-ci.yml](.github/workflows/relayer-ci.yml)

## Build dev environment

```bash
make build-dev-env
```

## Run dev environment

```bash
make run-dev-env
```

