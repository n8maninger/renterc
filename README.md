# renterd demo

## Overview
Wraps the `renterd` API in a simple CLI interface.

## Build
```sh
go build -o bin/ ./cmd/renterc
```

## Usage
### List Contracts:
```sh
renterc contracts
```

### Form Contract:
```sh
renterc contracts form ed25519:1fb61da55e8c54d6bc0fa0350b4eb5065af2a52485714a16680e7e21f686e2c7
```

Contracts are formed for 1 week and 10 sectors of storage.

### Upload Data:
```sh
renterc objects upload -m 1 -n 3 Big_Buck_Bunny_1080_10s_30MB.mp4
```

Uploads the file `Big_Buck_Bunny_1080_10s_30MB.mp4` with 3x redundancy. A minimum
of three contracts must be formed.

### Download Data:
```sh
renterc objects download Big_Buck_Bunny_1080_10s_30MB.mp4
```

Downloads the file `Big_Buck_Bunny_1080_10s_30MB.mp4` from the network, using
the metadata stored in `renterd`'s object store.