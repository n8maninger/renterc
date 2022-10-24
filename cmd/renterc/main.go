package main

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/rodaine/table"
	"github.com/siacentral/apisdkgo"
	"github.com/siacentral/apisdkgo/sia"
	"github.com/spf13/cobra"
	"go.sia.tech/renterd/api"
	"go.sia.tech/siad/types"
	"lukechampine.com/frand"
)

var (
	// initialize the Sia Central API client
	siaCentralClient = apisdkgo.NewSiaClient()
	// initialize the renterd API client
	renterdClient = func() *api.Client {
		return api.NewClient(os.Getenv("RENTERD_API_ADDR"), os.Getenv("RENTERD_API_PASSWORD"))
	}()
)

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

// args
var (
	dataDir    string
	dryRun     bool
	hashAlgo   string
	renterPriv api.PrivateKey
)

var (
	rootCmd = &cobra.Command{
		Use:   "renterc",
		Short: "renterc interacts with a renterd API",
		Run:   func(cmd *cobra.Command, args []string) {},
	}

	hostsCmd = &cobra.Command{
		Use:   "hosts",
		Short: "get a list of hosts",
		Run: func(cmd *cobra.Command, args []string) {
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
		},
	}
)

func init() {
	// register contract flags
	formCmd.Flags().StringVarP(&contractDurationStr, "duration", "D", "1w", "contract duration, accepts a duration and suffix (e.g. 1w)")
	formCmd.Flags().StringVarP(&contractUsageStr, "usage", "U", "1GiB", "contract usage, accepts a size and suffix (e.g. 1TiB)")

	// register file flags
	downloadCmd.Flags().BoolVar(&dryRun, "dry-run", false, "dry run, don't actually download the file")
	downloadCmd.Flags().StringVarP(&hashAlgo, "algo", "a", "sha256", "hash algorithm to use for verification")

	uploadCmd.Flags().Uint8VarP(&minShards, "min-shards", "m", 1, "minimum number of shards")
	uploadCmd.Flags().Uint8VarP(&totalShards, "total-shards", "n", 1, "total number of shards")
	uploadCmd.Flags().StringVarP(&hashAlgo, "algo", "a", "sha256", "hash algorithm to use for verification")

	// wallet flags
	fragCmd.Flags().BoolVar(&dryRun, "dry-run", false, "dry run, don't actually broadcast the transaction")

	// register global flags
	rootCmd.PersistentFlags().StringVarP(&dataDir, "dir", "d", ".", "data directory")

	// before running any command, load the renter key and initialize the
	// directory
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		// create the data directory if it doesn't exist
		_ = os.MkdirAll(dataDir, 0700)

		var err error
		// load or generate the renter key
		renterPriv, err = loadOrInitRenterKey(dataDir)
		if err != nil {
			log.Fatal("failed to load renter key:", err)
		}
	}

	// add contract commands
	contractsCmd.AddCommand(formCmd)
	// add file commands
	objectsCmd.AddCommand(uploadCmd, downloadCmd)
	// add wallet commands
	walletCmd.AddCommand(addressCmd, balanceCmd, fragCmd)
	// add commands to root
	rootCmd.AddCommand(contractsCmd, hostsCmd, objectsCmd, walletCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
