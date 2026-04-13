package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/store"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show project requirements and what's configured",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		configPath, isLocalOnly, err := config.FindValetConfig(cwd)
		if err != nil {
			return fmt.Errorf("no .valet.toml or .valet.local.toml found — run 'valet init' or 'valet adopt --personal <store>'")
		}
		configDir := filepath.Dir(configPath)

		var vc *domain.ValetConfig
		if isLocalOnly {
			vc = &domain.ValetConfig{DefaultEnv: "dev"}
		} else {
			vc, err = config.LoadValetToml(configPath)
			if err != nil {
				return err
			}
		}

		if !isLocalOnly {
			fmt.Printf("Project: %s\n", vc.Project)
			fmt.Printf("Store:   %s\n", vc.Store)
			if len(vc.Stores) > 0 {
				fmt.Printf("Linked:  %s\n", strings.Join(store.StoreLinkNames(vc.Stores), ", "))
			}
		} else {
			fmt.Println("Mode: personal (only .valet.local.toml)")
		}

		env := vc.DefaultEnv
		if envFlag != "" {
			env = envFlag
		}

		lc, _ := config.LoadLocalConfig(configDir)
		if isLocalOnly && lc != nil && len(lc.Stores) > 0 {
			fmt.Printf("Store:   %s (personal)\n", lc.Stores[0].Name)
		}

		// Resolve requirements from .env.example + .valet.toml + .valet.local.toml.
		requirements := store.ResolveRequirements(configDir, vc, lc)

		if len(requirements) == 0 {
			fmt.Println("\nNo requirements found.")
			fmt.Println("Add a .env.example file or run: valet require OPENAI_API_KEY --provider openai")
			return nil
		}

		// Resolve secrets across all linked stores.
		stores, err := openAllStores()
		if err != nil {
			return err
		}

		resolved, err := store.ResolveAllSecrets(stores, env)
		if err != nil {
			return err
		}

		fmt.Printf("\nRequirements (%s):\n", env)
		fmt.Printf("  %-30s %-30s %s\n", "SECRET", "SOURCE", "STATUS")
		fmt.Printf("  %-30s %-30s %s\n", "------", "------", "------")

		missing := 0
		for _, req := range requirements {
			if rs, found := resolved[req.Key]; found {
				fmt.Printf("  %-30s %-30s %s\n", req.Key, rs.StoreName+"/"+rs.ScopePath, green("ok"))
			} else if req.Optional {
				fmt.Printf("  %-30s %-30s %s\n", req.Key, "-", yellow("optional"))
			} else {
				fmt.Printf("  %-30s %-30s %s\n", req.Key, "-", red("missing"))
				missing++
			}
		}

		if missing > 0 {
			fmt.Printf("\n%d required secret(s) missing. Run 'valet setup' to configure.\n", missing)
		}

		return nil
	},
}

func green(s string) string  { return "\033[32m" + s + "\033[0m" }
func yellow(s string) string { return "\033[33m" + s + "\033[0m" }
func red(s string) string    { return "\033[31m" + s + "\033[0m" }

func init() {
	rootCmd.AddCommand(statusCmd)
}
