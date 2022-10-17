package main

import (
	"fmt"
	"strings"

	"go.sia.tech/renterd/api"
	"lukechampine.com/frand"
)

// getUsableContracts returns a list of contracts that can be used for storage
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
	usable := contracts[:0]
	for _, contract := range contracts {
		// if the contract has expired, remove it. If it's is too close to the
		// proof window start or if no renter funds remain, skip it
		if tip.Height > contract.EndHeight() {
			renterdClient.DeleteContract(contract.ID())
			continue
		} else if tip.Height >= uint64(contract.Revision.NewWindowStart)-144 || contract.Revision.NewValidProofOutputs[0].Value.IsZero() {
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

// parseByteStr parses a string representing a byte size into a uint64
func parseByteStr(s string) (uint64, error) {
	var (
		size uint64
		unit string
	)
	if _, err := fmt.Sscanf(s, "%d%s", &size, &unit); err != nil {
		return 0, fmt.Errorf("failed to parse byte string: %w", err)
	}

	switch strings.ToLower(unit) {
	case "b":
	case "kib":
		size *= 1024
	case "mib":
		size *= 1 << 20
	case "gib":
		size *= 1 << 30
	case "tib":
		size *= 1 << 40
	case "kb":
		size *= 1000
	case "mb":
		size *= 1e6
	case "gb":
		size *= 1e9
	case "tb":
		size *= 1e12
	default:
		return 0, fmt.Errorf("unknown unit: %v", unit)
	}
	return size, nil
}

func parseBlockDurStr(s string) (uint64, error) {
	var (
		dur  uint64
		unit string
	)
	if _, err := fmt.Sscanf(s, "%d%s", &dur, &unit); err != nil {
		return 0, fmt.Errorf("failed to parse block duration string: %w", err)
	}

	switch strings.ToLower(unit) {
	case "d":
		dur *= 144
	case "w":
		dur *= 1008
	case "m":
		dur *= 4320
	case "y":
		dur *= 52560
	default:
		return 0, fmt.Errorf("unknown unit: %v", unit)
	}
	return dur, nil
}
