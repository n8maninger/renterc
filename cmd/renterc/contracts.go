package main

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"go.sia.tech/renterd/api"
	"go.sia.tech/renterd/wallet"
	"go.sia.tech/siad/types"
)

// command args
var (
	contractDurationStr string
	contractUsageStr    string
)

var (
	contractsCmd = &cobra.Command{
		Use:   "contracts",
		Short: "get a list of contracts",
		Run: func(cmd *cobra.Command, args []string) {
			contracts, err := renterdClient.Contracts()
			if err != nil {
				log.Fatal("failed to get contracts:", err)
			}
			for _, c := range contracts {
				log.Println(c.ID(), c.Revision.HostPublicKey())
			}
		},
	}

	formCmd = &cobra.Command{
		Use:   "form",
		Short: "form a contract with host(s)",
		Run: func(cmd *cobra.Command, hostKeys []string) {
			contractUsage, err := parseByteStr(contractUsageStr)
			if err != nil {
				log.Fatal("failed to parse contract usage:", err)
			}
			contractDuration, err := parseBlockDurStr(contractDurationStr)
			if err != nil {
				log.Fatal("failed to parse contract duration:", err)
			}

			switch len(hostKeys) {
			case 0:
				log.Fatal("no host keys provided")
			case 1:
				log.Println("Forming contract with host", hostKeys[0])
			default:
				log.Printf("Forming contract with %v hosts", len(hostKeys))
			}

			for i, host := range hostKeys {
				if len(hostKeys) > 1 {
					log.Printf("Forming contract with host %v (%v/%v)", host, i+1, len(hostKeys))
				}
				var hostKey api.PublicKey
				err := hostKey.UnmarshalText([]byte(hostKeys[0]))
				if err != nil {
					log.Fatal("failed to parse host key:", err)
				}
				contractID, err := formContract(renterPriv, hostKey, contractUsage, contractDuration)
				if err != nil {
					log.Println("failed to form contract:", err)
					continue
				}
				log.Println("Formed contract:", contractID)
			}
		},
	}
)

// formContract forms a new contract with the host and adds it to renterd
func formContract(renterPriv api.PrivateKey, hostPub api.PublicKey, usage, duration uint64) (types.FileContractID, error) {
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

	uploadCost := settings.UploadBandwidthPrice.Mul64(usage)
	downloadCost := settings.DownloadBandwidthPrice.Mul64(usage)
	storageCost := settings.StoragePrice.Mul64(usage).Mul64(uint64(duration))
	hostCollateral := settings.Collateral.Mul64(usage).Mul64(uint64(duration))

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