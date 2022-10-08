# renterd demo

## Overview
Wraps the `renterd` API in a simple CLI interface.

## Build
```sh
go build -o bin/ ./cmd/renterc
```

## Usage
### Form Contract:
```sh
renterc form ed25519:1fb61da55e8c54d6bc0fa0350b4eb5065af2a52485714a16680e7e21f686e2c7
```

Contracts are formed for 1 week and 10 sectors of storage.

### Upload Data:
```sh
renterc upload Big_Buck_Bunny_1080_10s_30MB.mp4
```

Uploads the file `Big_Buck_Bunny_1080_10s_30MB.mp4` to a single host. At least
one contract must be formed.

### Download Data:
```sh
renterc download Big_Buck_Bunny_1080_10s_30MB.mp4
```

Downloads the file `Big_Buck_Bunny_1080_10s_30MB.mp4` from the network. Using
the metadata stored after uploading.