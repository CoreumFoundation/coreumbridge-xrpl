# Coreumbridge XRPL

Two-way Coreum <-> XRPL bridge.

## Specification

The specification is described [here](spec/spec.md).

## Relayer

### Build relayer binary

```bash 
make build-relayer
```

### Build relayer docker image

```bash 
make images
```

## Contract

### Build contract in docker

```bash 
make build-contract
```

## Dev environment

### Start dev environment

```bash 
make znet-start
```

### Stop dev environment

```bash 
make znet-remove
```
