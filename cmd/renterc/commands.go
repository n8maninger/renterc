package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/siacentral/apisdkgo"
	"go.sia.tech/renterd/api"
	"go.sia.tech/renterd/slab"
	"go.sia.tech/renterd/wallet"
	"go.sia.tech/siad/types"
	"lukechampine.com/frand"
)

type fileMeta struct {
	FileSize    uint64      `json:"filesize"`
	MinShards   uint8       `json:"minshards"`
	TotalShards uint8       `json:"totalshards"`
	Slabs       []slab.Slab `json:"slabs"`
}

// getUsableContracts returns a list of contracts that can be used for storage
func getUsableContracts(renterPriv api.PrivateKey, required int) ([]api.Contract, error) {
	// initialize the Sia Central API client
	siaCentralClient := apisdkgo.NewSiaClient()
	// initialize the renterd API client
	renterdClient := getAPIClient()

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
	usable := contracts[:0]
	for _, contract := range contracts {
		// if the contract is too close to the proof window start or if no
		// renter funds remain, skip it.
		if tip.Height >= uint64(contract.Revision.NewWindowStart)-144 || contract.Revision.NewValidProofOutputs[0].Value.IsZero() {
			continue
		}
		usable = append(usable, contract)
	}

	if len(usable) < required {
		return nil, fmt.Errorf("not enough usable contracts, need %v, have %v", required, len(usable))
	}

	used := make([]api.Contract, 0, required)
	for i := 0; i < required; i++ {
		idx := frand.Intn(len(usable))
		// choose a random contract
		contract := usable[idx]
		// remove the contract from the usable slice
		usable = append(usable[:idx], usable[idx+1:]...)

		host, err := siaCentralClient.GetHost(contract.HostKey().String())
		if err != nil {
			return nil, fmt.Errorf("failed to get host %v info: %w", contract.HostKey().String(), err)
		}

		used = append(used, api.Contract{
			ID:        contract.ID(),
			HostKey:   contract.HostKey(),
			HostIP:    host.NetAddress,
			RenterKey: renterPriv,
		})
	}
	return used, nil
}

// formContract forms a new contract with the host and adds it to renterd
func formContract(renterPriv api.PrivateKey, hostPub api.PublicKey) (types.FileContractID, error) {
	// initialize the Sia Central API client
	siaCentralClient := apisdkgo.NewSiaClient()
	// initialize the renterd API client
	renterdClient := getAPIClient()

	// get the wallet's address
	renterAddr, err := renterdClient.WalletAddress()
	if err != nil {
		return types.FileContractID{}, fmt.Errorf("failed to get wallet address: %w", err)
	}

	// get the current block height
	tip, err := renterdClient.ConsensusTip()
	if err != nil {
		return types.FileContractID{}, fmt.Errorf("failed to get consensus tip: %w", err)
	}

	// use the Sia Central API to get the host's net address since there is
	// no host db at this point.
	host, err := siaCentralClient.GetHost(hostPub.String())
	if err != nil {
		return types.FileContractID{}, fmt.Errorf("failed to get host info: %w", err)
	}

	// get the host's current settings
	settings, err := renterdClient.RHPScan(hostPub, host.NetAddress)
	if err != nil {
		return types.FileContractID{}, fmt.Errorf("failed to scan host: %w", err)
	}

	// max upload and download of 10 sectors for 7 days
	dataUsage := uint64(10 << 22)
	duration := uint64(144 * 7)

	uploadCost := settings.UploadBandwidthPrice.Mul64(dataUsage)
	downloadCost := settings.DownloadBandwidthPrice.Mul64(dataUsage)
	storageCost := settings.StoragePrice.Mul64(dataUsage).Mul64(uint64(duration))
	hostCollateral := settings.Collateral.Mul64(dataUsage).Mul64(uint64(duration))

	estimatedCost := settings.ContractPrice.Add(uploadCost).Add(downloadCost).Add(storageCost)

	// prepare the contract for formation
	fc, cost, err := renterdClient.RHPPrepareForm(renterPriv, hostPub, estimatedCost, renterAddr, hostCollateral, tip.Height+duration, settings)
	if err != nil {
		return types.FileContractID{}, fmt.Errorf("failed to prepare contract: %w", err)
	}

	formTxn := types.Transaction{
		FileContracts: []types.FileContract{fc},
	}

	// fund the formation transaction
	toSign, parents, err := renterdClient.WalletFund(&formTxn, cost)
	if err != nil {
		return types.FileContractID{}, fmt.Errorf("failed to fund formation transaction: %w", err)
	}

	// sign the transaction
	cf := wallet.ExplicitCoveredFields(formTxn)
	if err := renterdClient.WalletSign(&formTxn, toSign, cf); err != nil {
		return types.FileContractID{}, fmt.Errorf("failed to sign formation transaction: %w", err)
	}

	// form the contract
	contract, _, err := renterdClient.RHPForm(renterPriv, hostPub, host.NetAddress, append(parents, formTxn))
	if err != nil {
		renterdClient.WalletDiscard(formTxn) // formation error discard the inputs, ignore the error
		return types.FileContractID{}, fmt.Errorf("failed to form contract: %w", err)
	}

	// add the contract to renterd
	if err := renterdClient.AddContract(contract); err != nil {
		return types.FileContractID{}, fmt.Errorf("failed to add contract: %w", err)
	}

	return contract.ID(), nil
}

// uploadFile uploads a file with no redundancy to the Sia network
func uploadFile(renterPriv api.PrivateKey, dataDir, fp string, minShards, totalShards uint8) error {
	// initialize the renterd API client
	renterdClient := getAPIClient()

	// choose the contracts to use
	contracts, err := getUsableContracts(renterPriv, int(totalShards))
	if err != nil {
		return fmt.Errorf("failed to get usable contracts: %w", err)
	}

	stat, err := os.Stat(fp)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	file, err := os.Open(fp)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// grab the current block height
	tip, err := renterdClient.ConsensusTip()
	if err != nil {
		return fmt.Errorf("failed to get consensus tip: %w", err)
	}

	slabs, err := renterdClient.UploadSlabs(file, minShards, totalShards, tip.Height, contracts)
	if err != nil {
		return fmt.Errorf("failed to upload slabs: %w", err)
	}

	// build the metadata
	data := fileMeta{
		Slabs:       slabs,
		MinShards:   minShards,
		TotalShards: totalShards,
		FileSize:    uint64(stat.Size()),
	}

	// write the output file
	fileName := filepath.Base(fp)
	out, err := os.Create(filepath.Join(dataDir, fileName+".slab"))
	if err != nil {
		return fmt.Errorf("failed to create slab file: %w", err)
	}
	defer out.Close()

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")

	if err := enc.Encode(data); err != nil {
		return fmt.Errorf("failed to encode slabs: %w", err)
	} else if err := out.Sync(); err != nil {
		return fmt.Errorf("failed to sync slab file: %w", err)
	}
	return nil
}
