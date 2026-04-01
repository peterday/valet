package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/superset-studio/valet/internal/identity"
)

var identityCmd = &cobra.Command{
	Use:   "identity",
	Short: "Manage your valet identity",
}

var identityInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a new age keypair",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := identity.Init()
		if err != nil {
			return err
		}
		fmt.Println("Identity created.")
		fmt.Println("Public key:", id.PublicKey)
		return nil
	},
}

var identityShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show your public key",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := loadIdentity()
		if err != nil {
			return err
		}
		fmt.Println(id.PublicKey)
		return nil
	},
}

var identityExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export your public key for sharing",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := loadIdentity()
		if err != nil {
			return err
		}
		fmt.Print(id.Export())
		return nil
	},
}

func init() {
	identityCmd.AddCommand(identityInitCmd)
	identityCmd.AddCommand(identityShowCmd)
	identityCmd.AddCommand(identityExportCmd)
	rootCmd.AddCommand(identityCmd)
}
