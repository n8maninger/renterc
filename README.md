# renterd demo

## Overview
Wraps the `renterd` API in a simple CLI interface.

## Build
```sh
go build -o bin/ ./cmd/renterc
```

## Usage
The environment variables `RENTERD_API_ADDR` and `RENTERD_API_PASSWORD` must be set to connect to a `renterd` instance.
### List Contracts:
```sh
renterc contracts
```

### Form Contract:
```sh
renterc contracts form ed25519:1fb61da55e8c54d6bc0fa0350b4eb5065af2a52485714a16680e7e21f686e2c7
```

By default, contracts are formed for 1 week and 1 GiB of upload and download.
Duration can be changed with the `--duration 1m` flag  and data can be changed
with the `--data 10GiB` flag. Since renterd is still in development, it's
recommended to only upload test data to small short duration contracts.

### Upload Data:

#### Single File
```sh
renterc objects upload -m 1 -n 3 Big_Buck_Bunny_1080_10s_30MB.mp4
```

Uploads the file `Big_Buck_Bunny_1080_10s_30MB.mp4` with 3x redundancy. A
minimum of three contracts must be formed. `-m` and `-n` are optional and
default to 1 (no redundancy). `n` is the total number of shards to upload, while
`m` is the minimum number of shards required to reconstruct the file. For
example, Sia's default redundancy would be `-m 10 -n 30`.

#### Multiple Files
If multiple file paths are provided, they will be efficiently packed together.
This prevents wasting storage with small files due to Sia's 4MiB minimum sector
size. Each file will still be available under an individual key for downloading,
but deleting and repair become more complicated and may require reuploading some
files.

```sh
renterc objects upload -m 1 -n 3 file_1.jpeg file_2.jpeg
```

Packs and uploads `file_1.jpeg` and `file_2.jpeg` with 3x redundancy. 

### Download Data:
```sh
renterc objects download Big_Buck_Bunny_1080_10s_30MB.mp4 ~/dest.mp4
```

Downloads the file `Big_Buck_Bunny_1080_10s_30MB.mp4` from the network using
the metadata stored in `renterd`'s object store.
