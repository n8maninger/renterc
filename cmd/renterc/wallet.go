package main

import (
	"log"

	"github.com/spf13/cobra"
)

var (
	walletCmd = &cobra.Command{
		Use:   "wallet",
		Short: "manage the wallet",
		Run: func(cmd *cobra.Command, args []string) {
			address, err := renterdClient.WalletAddress()
			if err != nil {
				log.Fatal("failed to get wallet address:", err)
			}
			balance, err := renterdClient.WalletBalance()
			if err != nil {
				log.Fatal("failed to get wallet balance:", err)
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
				log.Fatal("failed to get wallet address:", err)
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
				log.Fatal("failed to get wallet balance:", err)
			}
			log.Println("Balance:", balance.HumanString())
		},
	}
)
