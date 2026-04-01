package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/store"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show project status and missing secrets",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		tomlPath, err := config.FindValetToml(cwd)
		if err != nil {
			return fmt.Errorf("no .valet.toml found — run 'valet init' first")
		}

		vc, err := config.LoadValetToml(tomlPath)
		if err != nil {
			return err
		}

		fmt.Printf("Project: %s\n", vc.Project)
		fmt.Printf("Store:   %s\n", vc.Store)
		if len(vc.Stores) > 0 {
			fmt.Printf("Linked:  %v\n", vc.Stores)
		}

		env := "dev"
		if envFlag != "" {
			env = envFlag
		}

		if len(vc.Requires) == 0 {
			fmt.Println("\nNo requirements declared in .valet.toml")
			fmt.Println("Add requirements with: valet require OPENAI_API_KEY --provider openai")
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

		missing := 0
		for name, req := range vc.Requires {
			if rs, found := resolved[name]; found {
				fmt.Printf("  %-30s %-20s %s\n", name, rs.StoreName+"/"+rs.ScopePath, green("ok"))
			} else if req.Optional {
				fmt.Printf("  %-30s %s\n", name, yellow("optional, not set"))
			} else {
				fmt.Printf("  %-30s %s\n", name, red("missing"))
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
