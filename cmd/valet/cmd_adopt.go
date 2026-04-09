package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/identity"
	"github.com/peterday/valet/internal/store"
)

var (
	adoptYesFlag       bool
	adoptImportEnvFlag bool
	adoptNoImportFlag  bool
)

var adoptCmd = &cobra.Command{
	Use:   "adopt",
	Short: "Adopt an existing project that has a .env.example file",
	Long: `Reads a .env.example (or .env.sample / .env.template / .env.dist) file
and bootstraps a valet project from it. Detected secrets become requirements,
matched providers are linked, and (if a populated .env exists) values can be
imported into the encrypted store.

  valet adopt              # interactive preview + confirm
  valet adopt --yes        # skip confirmation
  valet adopt --no-import  # don't offer to import existing .env values`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		result, err := store.AnalyzeForAdopt(cwd)
		if err != nil {
			return err
		}

		printAdoptPreview(result)

		if len(result.Requirements) == 0 {
			fmt.Println("\nNo secrets detected — nothing to adopt.")
			return nil
		}

		// Confirm.
		importValues := false
		if !adoptYesFlag {
			fmt.Print("\nProceed with adoption? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			ans, _ := reader.ReadString('\n')
			ans = strings.ToLower(strings.TrimSpace(ans))
			if ans != "y" && ans != "yes" {
				fmt.Println("Cancelled.")
				return nil
			}

			if result.HasExistingEnv && !adoptNoImportFlag {
				fmt.Printf("\nFound existing %s. Import values into encrypted store? [y/N] ", result.ExistingEnvPath)
				ans, _ := reader.ReadString('\n')
				ans = strings.ToLower(strings.TrimSpace(ans))
				importValues = (ans == "y" || ans == "yes")
			}
		} else {
			importValues = adoptImportEnvFlag && result.HasExistingEnv
		}

		id, err := identity.LoadOrInit()
		if err != nil {
			return fmt.Errorf("loading identity: %w", err)
		}

		if err := result.Apply(cwd, id, importValues); err != nil {
			return err
		}

		fmt.Printf("\nAdopted! Created .valet/ store and .valet.toml with %d requirement(s).\n", len(result.Requirements))
		if importValues {
			imported := 0
			for _, req := range result.Requirements {
				if v, ok := result.ExistingValues[req.Key]; ok && v != "" {
					imported++
				}
			}
			fmt.Printf("Imported %d value(s) from %s.\n", imported, result.ExistingEnvPath)
		}
		fmt.Println("\nNext: run 'valet ui' to see what's still missing, or 'valet status' to check.")
		return nil
	},
}

func printAdoptPreview(result *store.AdoptResult) {
	fmt.Printf("Found %s\n\n", result.SourceFile)

	if len(result.Requirements) > 0 {
		fmt.Printf("REQUIREMENTS DETECTED (%d)\n", len(result.Requirements))
		for _, req := range result.Requirements {
			marker := "✓"
			provider := req.ProviderDisplay
			if provider == "" {
				provider = "—"
				marker = "?"
			}
			fmt.Printf("  %s %-32s %s", marker, req.Key, provider)
			if req.Section != "" {
				fmt.Printf("  [%s]", req.Section)
			}
			fmt.Println()
			if req.Description != "" {
				fmt.Printf("      %s\n", truncate(req.Description, 70))
			}
		}
	}

	if len(result.NonSecrets) > 0 {
		fmt.Printf("\nNON-SECRETS (will not be tracked, %d)\n", len(result.NonSecrets))
		for _, c := range result.NonSecrets {
			fmt.Printf("  - %-32s %s\n", c.Key, c.Reason)
		}
	}

	if result.HasExistingEnv {
		filled := 0
		for _, req := range result.Requirements {
			if v, ok := result.ExistingValues[req.Key]; ok && v != "" {
				filled++
			}
		}
		fmt.Printf("\nFOUND EXISTING %s with %d secret value(s) available to import\n", result.ExistingEnvPath, filled)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// runAdoptFromInit runs the adopt flow with confirmation prompts.
// Used by `valet init` when it detects a .env.example.
func runAdoptFromInit(projectDir string) error {
	result, err := store.AnalyzeForAdopt(projectDir)
	if err != nil {
		return err
	}

	printAdoptPreview(result)

	if len(result.Requirements) == 0 {
		fmt.Println("\nNo secrets detected — falling back to standard init.")
		return nil
	}

	fmt.Print("\nProceed with adoption? [Y/n] ")
	reader := bufio.NewReader(os.Stdin)
	ans, _ := reader.ReadString('\n')
	ans = strings.ToLower(strings.TrimSpace(ans))
	if ans != "" && ans != "y" && ans != "yes" {
		fmt.Println("Cancelled.")
		return nil
	}

	importValues := false
	if result.HasExistingEnv {
		fmt.Printf("\nFound existing %s. Import values into encrypted store? [Y/n] ", result.ExistingEnvPath)
		ans, _ := reader.ReadString('\n')
		ans = strings.ToLower(strings.TrimSpace(ans))
		importValues = (ans == "" || ans == "y" || ans == "yes")
	}

	id, err := identity.LoadOrInit()
	if err != nil {
		return fmt.Errorf("loading identity: %w", err)
	}

	if err := result.Apply(projectDir, id, importValues); err != nil {
		return err
	}

	fmt.Printf("\nAdopted! Created .valet/ store and .valet.toml with %d requirement(s).\n", len(result.Requirements))
	if importValues {
		imported := 0
		for _, req := range result.Requirements {
			if v, ok := result.ExistingValues[req.Key]; ok && v != "" {
				imported++
			}
		}
		fmt.Printf("Imported %d value(s) from %s.\n", imported, result.ExistingEnvPath)
	}
	fmt.Println("\nNext: run 'valet ui' to see what's still missing, or 'valet status' to check.")
	return nil
}

func init() {
	adoptCmd.Flags().BoolVarP(&adoptYesFlag, "yes", "y", false, "skip confirmation")
	adoptCmd.Flags().BoolVar(&adoptImportEnvFlag, "import-env", false, "with --yes, import values from existing .env")
	adoptCmd.Flags().BoolVar(&adoptNoImportFlag, "no-import", false, "skip the import-from-.env prompt")
	rootCmd.AddCommand(adoptCmd)
}
