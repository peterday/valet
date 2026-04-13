package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/identity"
	"github.com/peterday/valet/internal/store"
)

var (
	adoptYesFlag       bool
	adoptImportEnvFlag bool
	adoptNoImportFlag  bool
	adoptPersonalFlag  string
)

var adoptCmd = &cobra.Command{
	Use:   "adopt",
	Short: "Adopt an existing project that has a .env.example file",
	Long: `Reads a .env.example (or .env.sample / .env.template / .env.dist) file
and bootstraps a valet project from it. Detected secrets become requirements,
matched providers are linked, and (if a populated .env exists) values can be
imported into the encrypted store.

  valet adopt                          # interactive, creates embedded store
  valet adopt --personal my-keys       # personal only — zero repo changes
  valet adopt --yes                    # skip confirmation
  valet adopt --no-import              # don't offer to import existing .env values

Personal mode (--personal <store>):
  Creates only .valet.local.toml (gitignored), links your personal store.
  No .valet.toml or .valet/ directory — invisible to the team.
  Use 'valet drive -- <cmd>' to inject secrets at runtime.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		result, err := store.AnalyzeForAdopt(cwd)
		if err != nil {
			return err
		}

		isPersonal := adoptPersonalFlag != ""
		if isPersonal {
			fmt.Printf("Personal mode — will write only .valet.local.toml (gitignored)\n")
			fmt.Printf("Store: %s\n\n", adoptPersonalFlag)
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

		if isPersonal {
			if err := applyPersonalAdopt(cwd, id, result, adoptPersonalFlag, importValues); err != nil {
				return err
			}
			fmt.Printf("\nAdopted (personal)! Created .valet.local.toml\n")
			fmt.Printf("Linked store: %s\n", adoptPersonalFlag)
		} else {
			if err := result.Apply(cwd, id, importValues); err != nil {
				return err
			}
			fmt.Printf("\nAdopted! Created .valet/ store and .valet.toml with %d requirement(s).\n", len(result.Requirements))
		}

		if importValues {
			imported := 0
			for _, req := range result.Requirements {
				if v, ok := result.ExistingValues[req.Key]; ok && v != "" {
					imported++
				}
			}
			fmt.Printf("Imported %d value(s) from %s.\n", imported, result.ExistingEnvPath)
		}
		fmt.Println("\nNext: run 'valet drive -- <cmd>' to run with secrets injected.")
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

// applyPersonalAdopt writes only .valet.local.toml with a store link.
// No .valet.toml, no .valet/ directory. Zero repo changes.
func applyPersonalAdopt(projectDir string, id *identity.Identity, result *store.AdoptResult, storeName string, importExisting bool) error {
	// Verify the personal store exists.
	if _, err := store.FindStoreByName(storeName, id); err != nil {
		return fmt.Errorf("personal store %q not found — create it with: valet store create %s", storeName, storeName)
	}

	// Write .valet.local.toml with store link.
	lc, _ := config.LoadLocalConfig(projectDir)
	if lc == nil {
		lc = &domain.LocalConfig{}
	}
	if !store.HasStoreLink(lc.Stores, storeName) {
		lc.Stores = append(lc.Stores, domain.StoreLink{Name: storeName})
	}
	if err := config.WriteLocalConfig(projectDir, lc); err != nil {
		return fmt.Errorf("writing .valet.local.toml: %w", err)
	}

	// Add to .gitignore.
	gitignorePath := filepath.Join(projectDir, ".gitignore")
	ensureLineInFile(gitignorePath, config.ValetLocalToml)

	// Import existing .env values into the personal store.
	if importExisting && result.HasExistingEnv {
		s, err := store.FindStoreByName(storeName, id)
		if err != nil {
			return err
		}
		project, err := s.ResolveDefaultProject()
		if err != nil {
			return fmt.Errorf("personal store %q has no default project — create one with: valet --store %s scope create dev/default", storeName, storeName)
		}
		for _, req := range result.Requirements {
			val, ok := result.ExistingValues[req.Key]
			if !ok || val == "" {
				continue
			}
			scopePath := "dev/default"
			if req.ProviderName != "" {
				_ = s.SetSecretWithProvider(project, scopePath, req.Key, val, req.ProviderName)
			} else {
				_ = s.SetSecret(project, scopePath, req.Key, val)
			}
		}
	}

	return nil
}

// ensureLineInFile appends a line to a file if not already present.
func ensureLineInFile(path, line string) {
	data, _ := os.ReadFile(path)
	for _, existing := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(existing) == line {
			return
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(data) > 0 && data[len(data)-1] != '\n' {
		f.WriteString("\n")
	}
	f.WriteString(line + "\n")
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
	adoptCmd.Flags().StringVar(&adoptPersonalFlag, "personal", "", "personal-only mode: link this store, create only .valet.local.toml (gitignored)")
	rootCmd.AddCommand(adoptCmd)
}
