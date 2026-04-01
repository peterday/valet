package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/provider"
)

var providersCmd = &cobra.Command{
	Use:   "providers",
	Short: "Manage provider registries",
}

var providersUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update provider definitions from registries",
	Long: `Pull the latest provider definitions from all registered provider
registries. On first run, clones the default registry.

  valet providers update`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := provider.ProvidersBaseDir()
		if err := os.MkdirAll(baseDir, 0755); err != nil {
			return err
		}

		defaultDir := provider.DefaultRegistryDir()

		if _, err := os.Stat(filepath.Join(defaultDir, ".git")); os.IsNotExist(err) {
			// First run — clone the default registry.
			fmt.Printf("Cloning default provider registry...\n")
			gitCmd := exec.Command("git", "clone", "--depth", "1", provider.DefaultRegistry, defaultDir)
			gitCmd.Stdout = os.Stdout
			gitCmd.Stderr = os.Stderr
			if err := gitCmd.Run(); err != nil {
				return fmt.Errorf("clone failed: %w", err)
			}
			fmt.Println("Done.")
			return nil
		}

		// Update all registries.
		entries, err := os.ReadDir(baseDir)
		if err != nil {
			return err
		}

		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			regDir := filepath.Join(baseDir, e.Name())
			if _, err := os.Stat(filepath.Join(regDir, ".git")); os.IsNotExist(err) {
				continue
			}
			fmt.Printf("Updating %s...\n", e.Name())
			gitCmd := exec.Command("git", "-C", regDir, "pull", "--ff-only")
			gitCmd.Stdout = os.Stdout
			gitCmd.Stderr = os.Stderr
			if err := gitCmd.Run(); err != nil {
				fmt.Printf("  warning: %v\n", err)
			}
		}

		fmt.Println("Done.")
		return nil
	},
}

var providersAddCmd = &cobra.Command{
	Use:   "add <github-owner/repo>",
	Short: "Add a custom provider registry",
	Long: `Clone a provider registry into ~/.valet/providers/. Use this for
private/internal provider definitions.

  valet providers add acme/internal-providers`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ref := args[0]

		// Validate owner/repo format.
		if !isValidRegistryRef(ref) {
			return fmt.Errorf("use owner/repo format (e.g. acme/internal-providers)")
		}
		name := filepath.Base(ref)
		gitURL := fmt.Sprintf("https://github.com/%s.git", ref)

		baseDir := provider.ProvidersBaseDir()
		if err := os.MkdirAll(baseDir, 0755); err != nil {
			return err
		}

		destDir := filepath.Join(baseDir, name)
		if _, err := os.Stat(destDir); err == nil {
			return fmt.Errorf("registry %q already exists at %s", name, destDir)
		}

		fmt.Printf("Cloning %s...\n", ref)
		gitCmd := exec.Command("git", "clone", "--depth", "1", gitURL, destDir)
		gitCmd.Stdout = os.Stdout
		gitCmd.Stderr = os.Stderr
		if err := gitCmd.Run(); err != nil {
			return fmt.Errorf("clone failed: %w", err)
		}

		fmt.Printf("Added registry %q\n", name)
		return nil
	},
}

var providersListCmd = &cobra.Command{
	Use:   "list [QUERY]",
	Short: "List or search available providers",
	Long: `List providers from the registry, optionally filtered by search query.

  valet providers list                                 # all providers
  valet providers list payments                        # search by category/keyword
  valet providers list "vector database"               # search by use case`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reg := provider.NewRegistry(provider.ProvidersBaseDir())

		var providers map[string]*provider.Provider
		if len(args) == 1 {
			results := reg.Search(args[0])
			if len(results) == 0 {
				fmt.Printf("No providers matching %q. Run 'valet providers update' to fetch the latest registry.\n", args[0])
				return nil
			}
			providers = make(map[string]*provider.Provider)
			for _, p := range results {
				providers[p.Name] = p
			}
		} else {
			providers = reg.All()
		}

		if len(providers) == 0 {
			fmt.Println("No providers loaded. Run 'valet providers update' to fetch the registry.")
			return nil
		}

		fmt.Printf("%-18s %-15s %-15s %-25s %s\n", "NAME", "DISPLAY", "CATEGORY", "ENV VARS", "FREE TIER")
		fmt.Printf("%-18s %-15s %-15s %-25s %s\n", "----", "-------", "--------", "--------", "---------")
		for _, p := range providers {
			var envNames []string
			for _, ev := range p.EnvVars {
				envNames = append(envNames, ev.Name)
			}
			envStr := ""
			if len(envNames) > 0 {
				envStr = envNames[0]
				if len(envNames) > 1 {
					envStr += fmt.Sprintf(" (+%d)", len(envNames)-1)
				}
			}
			cat := p.Category
			if cat == "" {
				cat = "-"
			}
			free := p.FreeTier
			if free == "" {
				free = "-"
			}
			fmt.Printf("%-18s %-15s %-15s %-25s %s\n", p.Name, p.DisplayName, cat, envStr, free)
		}
		return nil
	},
}

var providersRemoveCmd = &cobra.Command{
	Use:   "remove <registry-name>",
	Short: "Remove a custom provider registry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if name == provider.DefaultRegistryName {
			return fmt.Errorf("cannot remove the default registry")
		}

		dir := filepath.Join(provider.ProvidersBaseDir(), name)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return fmt.Errorf("registry %q not found", name)
		}

		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		fmt.Printf("Removed registry %q\n", name)
		return nil
	},
}

var registryRefPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+$`)

// isValidRegistryRef validates that ref is a safe owner/repo format.
func isValidRegistryRef(ref string) bool {
	return registryRefPattern.MatchString(ref)
}

func init() {
	providersCmd.AddCommand(providersUpdateCmd)
	providersCmd.AddCommand(providersAddCmd)
	providersCmd.AddCommand(providersListCmd)
	providersCmd.AddCommand(providersRemoveCmd)
	rootCmd.AddCommand(providersCmd)
}
