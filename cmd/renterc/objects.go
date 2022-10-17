package main

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rodaine/table"
	"github.com/spf13/cobra"
	"go.sia.tech/renterd/api"
	"go.sia.tech/renterd/object"
	"go.sia.tech/renterd/rhp/v2"
)

var (
	// upload command args
	minShards   uint8
	totalShards uint8
)

var (
	objectsCmd = &cobra.Command{
		Use:   "objects",
		Short: "list objects",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 1 {
				obj, err := renterdClient.Object(args[0])
				if err != nil {
					log.Fatalf("failed to get object: %v", err)
				}

				js, err := json.MarshalIndent(obj, "", "  ")
				if err != nil {
					log.Fatalf("failed to marshal object: %v", err)
				}
				fmt.Println(string(js))
				return
			}
			entries, err := renterdClient.ObjectEntries("")
			if err != nil {
				log.Fatal("failed to get object entries:", err)
			}
			tbl := table.New("Name")
			for _, entry := range entries {
				tbl.AddRow(entry)
			}
			tbl.Print()
		},
	}

	uploadCmd = &cobra.Command{
		Use:   "upload",
		Short: "upload a file to the network",
		Run: func(cmd *cobra.Command, files []string) {
			for _, file := range files {
				log.Println("Uploading", file)
				start := time.Now()
				checksum, err := uploadFile(renterPriv, filepath.Join(dataDir, "files"), file, minShards, totalShards)
				if err != nil {
					log.Fatal("failed to upload file:", err)
				}
				log.Printf("Uploaded %v in %v (%v %x)", file, time.Since(start), hashAlgo, checksum)
			}
		},
	}

	downloadCmd = &cobra.Command{
		Use:   "download",
		Short: "download a file from the network",
		Long:  "Usage: renterc download <object> <file>",
		Run: func(cmd *cobra.Command, files []string) {
			var outputPath string
			key := files[0]
			if !dryRun {
				outputPath = files[1]
			}

			log.Println("Downloading object with key", key)
			start := time.Now()
			checksum, err := downloadFile(renterPriv, key, outputPath)
			if err != nil {
				log.Fatal("failed to download file:", err)
			}
			log.Printf("Downloaded %v in %v (%v %x)", key, time.Since(start), hashAlgo, checksum)
		},
	}
)

// uploadFile uploads a file to the Sia network and adds a new object to renterd
func uploadFile(renterPriv api.PrivateKey, dataDir, fp string, minShards, totalShards uint8) ([]byte, error) {
	// choose the contracts to use
	contracts, err := getUsableContracts(renterPriv, int(totalShards))
	if err != nil {
		return nil, fmt.Errorf("failed to get usable contracts: %w", err)
	}

	stat, err := os.Stat(fp)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	f, err := os.Open(fp)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	// grab the current block height
	tip, err := renterdClient.ConsensusTip()
	if err != nil {
		return nil, fmt.Errorf("failed to get consensus tip: %w", err)
	}

	var h hash.Hash
	switch strings.ToLower(hashAlgo) {
	case "sha256":
		h = sha256.New()
	case "sha1":
		h = sha1.New()
	case "md5":
		h = md5.New()
	default:
		return nil, fmt.Errorf("unknown hash algorithm: %v", hashAlgo)
	}
	tr := io.TeeReader(f, h)

	slabs, err := renterdClient.UploadSlabs(tr, minShards, totalShards, tip.Height, contracts)
	if err != nil {
		return nil, fmt.Errorf("failed to upload slabs: %w", err)
	}

	objs := object.SplitSlabs(slabs, []int{int(stat.Size())})
	err = renterdClient.AddObject(filepath.Base(fp), object.Object{
		Key:   object.GenerateEncryptionKey(),
		Slabs: objs[0],
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add object: %w", err)
	}
	return h.Sum(nil), nil
}

func downloadFile(renterPriv api.PrivateKey, objectKey, outputPath string) ([]byte, error) {
	obj, err := renterdClient.Object(objectKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get object: %w", err)
	}

	currentContracts, err := renterdClient.Contracts()
	if err != nil {
		return nil, fmt.Errorf("failed to get contracts: %w", err)
	}

	tip, err := renterdClient.ConsensusTip()
	if err != nil {
		return nil, fmt.Errorf("failed to get consensus tip: %w", err)
	}

	hostContracts := make(map[api.PublicKey]rhp.Contract)
	for _, c := range currentContracts {
		// if the contract has expired, remove it. If it's is too close to the
		// proof window start or if no renter funds remain, skip it
		if tip.Height > c.EndHeight() {
			renterdClient.DeleteContract(c.ID())
			continue
		} else if tip.Height >= uint64(c.Revision.NewWindowStart)-144 || c.Revision.NewValidProofOutputs[0].Value.IsZero() {
			continue
		}

		hostContracts[c.HostKey()] = c
	}

	// find a contract for each shard
	added := make(map[api.PublicKey]bool)
	var contracts []api.Contract
	var len int64
	for _, slab := range obj.Slabs {
		var count uint8
		for _, shard := range slab.Shards {
			// if there is no contract for this host, skip it
			if _, ok := hostContracts[shard.Host]; !ok {
				continue
			}

			if !added[shard.Host] {
				// grab the host's net address from the Sia Central API
				host, err := siaCentralClient.GetHost(shard.Host.String())
				if err != nil {
					return nil, fmt.Errorf("failed to get host: %w", err)
				}

				contracts = append(contracts, api.Contract{
					HostKey:   shard.Host,
					HostIP:    host.NetAddress,
					ID:        hostContracts[shard.Host].ID(),
					RenterKey: renterPriv,
				})
				added[shard.Host] = true
			}

			count++
		}

		if count < slab.MinShards {
			return nil, fmt.Errorf("not enough contracts available to download file")
		}

		len += int64(slab.Length)
	}

	if dryRun {
		js, _ := json.MarshalIndent(api.SlabsDownloadRequest{
			Slabs:     obj.Slabs,
			Offset:    0,
			Length:    len,
			Contracts: contracts,
		}, "", "  ")
		fmt.Println(string(js))
		return nil, nil
	}

	// download the file
	f, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	var h hash.Hash
	switch strings.ToLower(hashAlgo) {
	case "sha256":
		h = sha256.New()
	case "sha1":
		h = sha1.New()
	case "md5":
		h = md5.New()
	default:
		return nil, fmt.Errorf("unknown hash algorithm: %v", hashAlgo)
	}
	mw := io.MultiWriter(f, h)

	if err := renterdClient.DownloadSlabs(mw, obj.Slabs, 0, len, contracts); err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	} else if err := f.Sync(); err != nil {
		return nil, fmt.Errorf("failed to sync file: %w", err)
	}
	return h.Sum(nil), nil
}
