package main

import (
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rodaine/table"
	"github.com/siacentral/apisdkgo"
	"github.com/siacentral/apisdkgo/sia"
	"go.sia.tech/renterd/api"
	"go.sia.tech/siad/types"
	"lukechampine.com/frand"
)

func getAPIClient() *api.Client {
	return api.NewClient(os.Getenv("RENTERD_API_ADDR"), os.Getenv("RENTERD_API_PASSWORD"))
}

// loadOrInitRenterKey loads the renter key from the data directory, or
// generates a new one if it doesn't exist.
func loadOrInitRenterKey(dataDir string) (api.PrivateKey, error) {
	renterKeyPath := filepath.Join(dataDir, "renter.key")
	renterKeyFile, err := os.Open(renterKeyPath)
	if errors.Is(err, fs.ErrNotExist) {
		// file doesn't exist, generate a new key
		renterKey := api.PrivateKey(ed25519.NewKeyFromSeed(frand.Bytes(32)))
		renterKeyFile, err = os.Create(renterKeyPath)
		if err != nil {
			return api.PrivateKey{}, err
		}
		defer renterKeyFile.Close()

		_, err = renterKeyFile.Write(renterKey[:])
		if err != nil {
			return api.PrivateKey{}, err
		}
		return renterKey, nil
	} else if err != nil {
		return api.PrivateKey{}, fmt.Errorf("failed to open renter key file: %w", err)
	}
	defer renterKeyFile.Close()

	renterKey := make(api.PrivateKey, 32)
	_, err = io.ReadFull(renterKeyFile, renterKey)
	return renterKey, err
}

func main() {
	var dataDir string

	flag.StringVar(&dataDir, "d", ".", "data directory")

	// load or generate the renter key
	renterPriv, err := loadOrInitRenterKey(dataDir)
	if err != nil {
		log.Fatal("failed to load renter key:", err)
	}

	cmd := strings.ToLower(os.Args[1])
	switch cmd {
	case "height":
		flag.Parse()

		// get the current block height
		tip, err := getAPIClient().ConsensusTip()
		if err != nil {
			log.Fatal("failed to get consensus tip:", err)
		}
		log.Println("current block:", tip.Height, tip.ID)
	case "balance": // check the wallet's balance
		flag.Parse()

		balance, err := getAPIClient().WalletBalance()
		if err != nil {
			log.Fatal("failed to get wallet balance:", err)
		}
		log.Println("balance:", balance.HumanString())
	case "list-hosts": // list the hosts from the Sia Central API
		flag.Parse()

		// initialize the Sia Central API client
		siaCentralClient := apisdkgo.NewSiaClient()

		// get the list of hosts
		acceptingContracts, benchmarked := true, true
		maxContractPrice := types.SiacoinPrecision.Div64(2)
		var minUptime float32 = 0.85
		hosts, err := siaCentralClient.GetActiveHosts(sia.HostFilter{
			AcceptingContracts: &acceptingContracts,
			MaxContractPrice:   &maxContractPrice,
			MinUptime:          &minUptime,
			Benchmarked:        &benchmarked,
		})
		if err != nil {
			log.Fatal("failed to get hosts:", err)
		}
		tbl := table.New("#", "Public Key", "Address")
		for i, host := range hosts {
			tbl.AddRow(i+1, host.PublicKey, host.NetAddress)
		}
		tbl.Print()
	case "form": // form a contract with a host
		flag.Parse()

		// get the host keys from the args
		hostKeys := flag.Args()[1:]

		if len(hostKeys) == 1 {
			log.Println("Forming contract with host", hostKeys[0])
			var hostKey api.PublicKey
			err := hostKey.UnmarshalText([]byte(hostKeys[0]))
			if err != nil {
				log.Fatal("failed to parse host key:", err)
			}
			contractID, err := formContract(renterPriv, hostKey)
			if err != nil {
				log.Fatal("failed to form contract:", err)
			}
			log.Println("Formed contract:", contractID)
			return
		}

		log.Printf("forming contract with %v hosts", len(hostKeys))
		for i, host := range hostKeys {
			log.Printf("Forming contract with host %v (%v/%v)", host, i+1, len(hostKeys))
			var hostKey api.PublicKey
			err := hostKey.UnmarshalText([]byte(hostKeys[0]))
			if err != nil {
				log.Fatal("failed to parse host key:", err)
			}
			contractID, err := formContract(renterPriv, hostKey)
			if err != nil {
				log.Println("failed to form contract:", err)
				continue
			}
			log.Println("Formed contract:", contractID)
		}
	case "list": // list contracts
		flag.Parse()

		contracts, err := getAPIClient().Contracts()
		if err != nil {
			log.Fatal("failed to get contracts:", err)
		}
		for _, c := range contracts {
			log.Println(c.ID(), c.Revision.HostPublicKey())
		}
	case "upload": // upload a file
		filePath := flag.String("f", "", "file to upload")
		minShardsStr := flag.String("m", "1", "minimum number of shards")
		totalShardsStr := flag.String("n", "1", "total number of shards")
		flag.Parse()

		minShards, err := strconv.ParseUint(*minShardsStr, 10, 8)
		if err != nil {
			log.Fatal("failed to parse minShards:", err)
		}
		totalShards, err := strconv.ParseUint(*totalShardsStr, 10, 8)
		if err != nil {
			log.Fatal("failed to parse totalShards:", err)
		}

		if err = uploadFile(renterPriv, filepath.Join(dataDir, "files"), *filePath, uint8(minShards), uint8(totalShards)); err != nil {
			log.Fatal("failed to upload file:", err)
		}
	case "download": // download a file
		panic("not implemented")
	default:
		log.Fatal("unknown command:", cmd)
	}
}
