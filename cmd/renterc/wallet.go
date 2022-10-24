package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"github.com/spf13/cobra"
	"go.sia.tech/siad/types"
)

var (
	walletCmd = &cobra.Command{
		Use:   "wallet",
		Short: "manage the wallet",
		Run: func(cmd *cobra.Command, args []string) {
			address, err := renterdClient.WalletAddress()
			if err != nil {
				log.Fatalln("failed to get wallet address:", err)
			}
			balance, err := renterdClient.WalletBalance()
			if err != nil {
				log.Fatalln("failed to get wallet balance:", err)
			}
			log.Println("Address:", address)
			log.Println("Balance:", balance.HumanString())
		},
	}

	addressCmd = &cobra.Command{
		Use:   "address",
		Short: "get the wallet address",
		Run: func(cmd *cobra.Command, args []string) {
			address, err := renterdClient.WalletAddress()
			if err != nil {
				log.Fatalln("failed to get wallet address:", err)
			}
			log.Println("Address:", address)
		},
	}

	balanceCmd = &cobra.Command{
		Use:   "balance",
		Short: "get the renter's balance",
		Run: func(cmd *cobra.Command, args []string) {
			balance, err := renterdClient.WalletBalance()
			if err != nil {
				log.Fatalln("failed to get wallet balance:", err)
			}
			log.Println("Balance:", balance.HumanString())
		},
	}

	fragCmd = &cobra.Command{
		Use:   "frag",
		Short: "splits the wallet's balance into <n> utxos worth <amt>",
		Long:  "renterc wallet frag <n> <amt>",
		Args: func(cm *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("expected 2 arguments <n> <amt>, got %d", len(args))
			}

			n, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("expected integer, got %s", args[0])
			} else if n > 20 {
				return fmt.Errorf("n must be less than 20, got %d", n)
			}

			if _, err := types.ParseCurrency(args[1]); err != nil {
				return fmt.Errorf("expected currency, got %s", args[1])
			}

			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			count, err := strconv.Atoi(args[0])
			if err != nil {
				log.Fatalln("failed to parse count:", err)
			}

			amount, err := parseCurrency(args[1])
			if err != nil {
				log.Fatalln("failed to parse amount:", err)
			}

			address, err := renterdClient.WalletAddress()
			if err != nil {
				log.Fatalln("failed to get wallet address:", err)
			}

			fragTxn := types.Transaction{
				// high miner fees to guarantee acceptance
				MinerFees:      []types.Currency{types.SiacoinPrecision.Div64(2)},
				SiacoinOutputs: make([]types.SiacoinOutput, count),
			}

			if dryRun {
				log.Printf("dry run: sending %v outputs worth %v each to %v", count, amount.HumanString(), address)
			} else {
				log.Printf("Sending %v outputs worth %v each to %v", count, amount.HumanString(), address)
			}

			for i := range fragTxn.SiacoinOutputs {
				fragTxn.SiacoinOutputs[i] = types.SiacoinOutput{
					Value:      amount,
					UnlockHash: address,
				}
			}

			fundAmount := amount.Mul64(uint64(count))
			toSign, _, err := renterdClient.WalletFund(&fragTxn, fundAmount)
			if err != nil {
				log.Fatalln("failed to fund transaction:", err)
			} else if err := renterdClient.WalletSign(&fragTxn, toSign, types.FullCoveredFields); err != nil {
				renterdClient.WalletDiscard(fragTxn)
				log.Fatalln("failed to sign transaction:", err)
			}

			if dryRun {
				renterdClient.WalletDiscard(fragTxn)
				buf, _ := json.MarshalIndent(fragTxn, "", "  ")
				log.Println(string(buf))
				return
			}

			if err := renterdClient.BroadcastTransaction([]types.Transaction{fragTxn}); err != nil {
				renterdClient.WalletDiscard(fragTxn)
				log.Fatalln("failed to broadcast transaction:", err)
			}

			log.Printf("Successfully broadcast transaction %v", fragTxn.ID())
		},
	}
)
