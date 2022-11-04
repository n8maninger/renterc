package main

import (
	"bufio"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rodaine/table"
	"github.com/spf13/cobra"
	"go.sia.tech/renterd/api"
	"go.sia.tech/renterd/object"
	"go.sia.tech/renterd/rhp/v2"
	"go.sia.tech/renterd/slab"
	"lukechampine.com/frand"
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
		Long:  "renterc objects [flags] [key]",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 1 {
				obj, err := renterdClient.Object(args[0])
				if err != nil {
					log.Fatalln("failed to get object:", err)
				}

				js, err := json.MarshalIndent(obj, "", "  ")
				if err != nil {
					log.Fatalln("failed to marshal object:", err)
				}
				fmt.Println(string(js))
				return
			}
			entries, err := renterdClient.ObjectEntries("")
			if err != nil {
				log.Fatalln("failed to get object entries:", err)
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
		Short: "upload file(s) to the network",
		Long: `renterc upload [flags] <file1> [<file2> ...]

Splits the local file(s) into shards and uploads them to the Sia network. The files will be packed together if multiple paths are specified to reduce wasted storage space.

The flags -m and -n are used to control redundancy. m is the minimum number of shards required to recover the file, and n is the total number of hosts to use. A file with -m 1 -n 3 would be uploaded to 3 hosts, with 1 host required to recover the file. The siad renter defaults to -m 10 -n 30 for 3x redundancy across 30 hosts. The default is -m 1 -n 1, which has no redundancy. You must form contracts with at least <n> hosts before uploading.`,
		Run: func(cmd *cobra.Command, files []string) {
			log.Printf("Uploading %v objects", len(files))
			start := time.Now()
			if err := uploadFiles(renterPriv, minShards, totalShards, files); err != nil {
				log.Fatalln("failed to upload file:", err)
			}
			log.Printf("Uploaded %v objects in %v", len(files), time.Since(start))
		},
	}

	downloadCmd = &cobra.Command{
		Use:   "download",
		Short: "download a file from the network",
		Long:  "renterc download <object> <file>",
		Args: func(cmd *cobra.Command, args []string) error {
			if dryRun && len(args) != 1 {
				return errors.New("only the object key arg is allowed when using --dry-run")
			} else if !dryRun && len(args) != 2 {
				return errors.New("<object> and <output file> are required")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, files []string) {
			var outputPath string
			key := files[0]
			if !dryRun {
				outputPath = files[1]
			}

			if !skipConfirm {
				if _, err := os.Stat(outputPath); err == nil {
					fmt.Printf("file %v already exists. Overwrite? (y/n): ", outputPath)
					var confirm string
					fmt.Scanln(&confirm)
					if s := strings.ToLower(confirm); s != "y" && s != "yes" {
						log.Fatalln("download aborted")
					}
				}
			}

			println("Downloading object with key", key)
			start := time.Now()
			checksum, err := downloadFile(renterPriv, key, outputPath)
			if err != nil {
				log.Fatalln("failed to download file:", err)
			}
			log.Printf("Downloaded %v in %v (%v %x)", key, time.Since(start), hashAlgo, checksum)
		},
	}
)

// getUsableContracts returns a list of contracts that can be used for storage
//
// TODO: sort contracts by upload/download speed and price instead of random
func getUsableContracts(renterPriv api.PrivateKey, required int) ([]api.Contract, error) {
	// chose the contracts to use
	contracts, err := renterdClient.Contracts()
	if err != nil {
		return nil, fmt.Errorf("failed to get contracts: %w", err)
	}

	tip, err := renterdClient.ConsensusTip()
	if err != nil {
		return nil, fmt.Errorf("failed to get consensus tip: %w", err)
	}

	// remove contracts that are expired or empty
	usable := make([]api.Contract, 0, len(contracts))
	for _, contract := range contracts {
		// if the contract has expired, remove it. If it's is too close to the
		// proof window start or if no renter funds remain, skip it
		if tip.Height > contract.EndHeight() {
			renterdClient.DeleteContract(contract.ID())
			continue
		} else if tip.Height >= uint64(contract.Revision.NewWindowStart)-144 || contract.Revision.NewValidProofOutputs[0].Value.IsZero() {
			continue
		}

		host, err := renterdClient.Host(contract.HostKey())
		if err != nil {
			return nil, fmt.Errorf("failed to get host %v info: %w", contract.HostKey().String(), err)
		}
		netaddress := host.Announcements[len(host.Announcements)-1].NetAddress

		usable = append(usable, api.Contract{
			ID:        contract.ID(),
			HostKey:   contract.HostKey(),
			HostIP:    netaddress,
			RenterKey: renterPriv,
		})
	}

	if len(usable) < required {
		return nil, fmt.Errorf("not enough usable contracts, need %v, have %v", required, len(usable))
	}

	// shuffle the contracts so the same ones are not always used
	frand.Shuffle(len(usable), func(i, j int) { usable[i], usable[j] = usable[j], usable[i] })
	return usable, nil
}

// uploadFiles uploads files to the Sia network and adds a new object for each
// file to renterd
func uploadFiles(renterPriv api.PrivateKey, minShards, totalShards uint8, files []string) error {
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			return fmt.Errorf("failed to stat file %v: %w", f, err)
		}
	}

	// choose the contracts to use
	contracts, err := getUsableContracts(renterPriv, int(totalShards))
	if err != nil {
		return fmt.Errorf("failed to get usable contracts: %w", err)
	}

	// create the hasher
	var h hash.Hash
	switch strings.ToLower(hashAlgo) {
	case "sha256":
		h = sha256.New()
	case "sha1":
		h = sha1.New()
	case "md5":
		h = md5.New()
	default:
		return fmt.Errorf("unknown hash algorithm: %v", hashAlgo)
	}

	// get the total upload length
	var totalUploadBytes int64
	for _, file := range files {
		fi, err := os.Stat(file)
		if err != nil {
			return fmt.Errorf("failed to stat file %v: %w", file, err)
		}
		totalUploadBytes += fi.Size()
	}

	lengths := make([]int, 0, len(files))
	checksums := make([][]byte, 0, len(files))

	// use io.Pipe to treat all files as a continuous stream and pack them
	// together
	r, w := io.Pipe()
	go func() {
		var wg sync.WaitGroup
		wg.Add(len(files))

		for _, file := range files {
			h.Reset()

			err := func() error {
				f, err := os.Open(file)
				if err != nil {
					return fmt.Errorf("failed to open file: %w", err)
				}
				defer f.Close()

				// copy the file contents to the pipe and the hasher
				tr := io.TeeReader(bufio.NewReaderSize(f, rhp.SectorSize), h)
				n, err := io.Copy(w, tr)
				if err != nil {
					return fmt.Errorf("failed to copy file: %w", err)
				}
				// append the length and the checksum
				lengths = append(lengths, int(n))
				checksums = append(checksums, h.Sum(nil))
				return nil
			}()
			if err != nil {
				panic(err)
			}
			wg.Done()
		}

		// wait for all files to be copied
		wg.Wait()
		w.Close()
	}()

	// grab the current block height
	tip, err := renterdClient.ConsensusTip()
	if err != nil {
		return fmt.Errorf("failed to get consensus tip: %w", err)
	}

	// upload each slab, using the pipe as the source. Each file will be copied
	// to the pipe, then the pipe will be closed.
	var slabs []slab.Slab
	maxSlabSize := int64(minShards) * rhp.SectorSize
	// TODO: parallelize
	for i, rem := 0, totalUploadBytes; rem > 0; i, rem = i+1, rem-maxSlabSize {
		uploadSize := rem
		if uploadSize > maxSlabSize {
			// if the upload size is greater than the slab size, use the slab
			// size
			uploadSize = maxSlabSize
		}
		lr := io.LimitReader(r, uploadSize)
		slab, err := renterdClient.UploadSlab(lr, minShards, totalShards, tip.Height, contracts)
		if err != nil {
			return fmt.Errorf("failed to upload slab %v: %w", i, err)
		}
		slabs = append(slabs, slab)
	}

	// split the uploaded slabs into objects and add each object to renterd
	objs := object.SplitSlabs(slabs, lengths)
	for i, file := range files {
		key := filepath.Base(file)
		err = renterdClient.AddObject(key, object.Object{
			Key:   object.GenerateEncryptionKey(),
			Slabs: objs[i],
		})
		log.Printf("Added object %v - %v bytes (%v %x)", key, lengths[i], hashAlgo, checksums[i])
		if err != nil {
			return fmt.Errorf("failed to add object %v: %w", key, err)
		}
	}
	return nil
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
	var fileLength int64
	for _, slab := range obj.Slabs {
		var count uint8
		for _, shard := range slab.Shards {
			// if there is no contract for this host, skip it
			if _, ok := hostContracts[shard.Host]; !ok {
				continue
			}

			if !added[shard.Host] {
				// grab the host's net address from the renterd hostdb
				host, err := renterdClient.Host(shard.Host)
				if err != nil {
					return nil, fmt.Errorf("failed to get host: %w", err)
				}
				netaddress := host.Announcements[len(host.Announcements)-1].NetAddress

				contracts = append(contracts, api.Contract{
					HostKey:   shard.Host,
					HostIP:    netaddress,
					ID:        hostContracts[shard.Host].ID(),
					RenterKey: renterPriv,
				})
				added[shard.Host] = true
			}
			count++
		}

		if count < slab.MinShards {
			return nil, errors.New("not enough contracts available to download file")
		}

		fileLength += int64(slab.Length)
	}

	if dryRun {
		n := len(obj.Slabs)
		for i, slab := range obj.Slabs {
			js, _ := json.MarshalIndent(api.SlabsDownloadRequest{
				Slab:      slab,
				Contracts: contracts,
			}, "", "  ")
			fmt.Printf("-- Request %v of %v --\n", i+1, n)
			fmt.Println(string(js))
		}
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

	for i, slab := range obj.Slabs {
		if err := renterdClient.DownloadSlab(mw, slab, contracts); err != nil {
			return nil, fmt.Errorf("failed to download slab %v: %w", i, err)
		}
	}
	if err := f.Sync(); err != nil {
		return nil, fmt.Errorf("failed to sync file: %w", err)
	}
	return h.Sum(nil), nil
}
