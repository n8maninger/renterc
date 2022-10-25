package main

import (
	"fmt"
	"math/big"
	"strings"

	"go.sia.tech/siad/types"
)

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

func parseCurrency(s string) (types.Currency, error) {
	hastings, err := types.ParseCurrency(s)
	if err != nil {
		return types.ZeroCurrency, fmt.Errorf("failed to parse currency: %w", err)
	}
	i, _ := new(big.Int).SetString(hastings, 10)
	return types.NewCurrency(i), nil
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
