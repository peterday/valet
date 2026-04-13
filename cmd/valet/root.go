package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/identity"
	"github.com/peterday/valet/internal/store"
)

// Set by -ldflags at build time.
var version = "dev"

var (
	envFlag     string
	scopeFlag   string
	projectFlag string
	storeFlag   string
)

var rootCmd = &cobra.Command{
	Use:     "valet",
	Short:   "API key management for developers and teams",
	Long:    "Valet manages secrets in encrypted stores — locally, in git repos, or in the cloud.",
	Version: version,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Don't check for updates when running MCP server or update itself.
		name := cmd.Name()
		if name == "serve" || name == "update" {
			return
		}
		startUpdateCheck()
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		printUpdateNotice()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&envFlag, "env", "e", "", "environment override")
	rootCmd.PersistentFlags().StringVar(&scopeFlag, "scope", "", "scope path (e.g. dev/runtime)")
	rootCmd.PersistentFlags().StringVarP(&projectFlag, "project", "p", "", "project override")
	rootCmd.PersistentFlags().StringVarP(&storeFlag, "store", "s", "", "target a named store (e.g. my-keys)")
}

// loadIdentity loads the local identity, or returns nil with an error.
func loadIdentity() (*identity.Identity, error) {
	return identity.Load()
}

// loadIdentityOrInit loads or creates an identity.
func loadIdentityOrInit() (*identity.Identity, error) {
	return identity.LoadOrInit()
}

// openStore resolves and opens the current store (embedded or linked).
// If --store flag is set, opens that named store instead.
// In personal-only mode (only .valet.local.toml), opens the first linked store.
func openStore() (*store.Store, error) {
	id, err := loadIdentity()
	if err != nil {
		return nil, err
	}
	if storeFlag != "" {
		return store.FindStoreByName(storeFlag, id)
	}

	// Try normal resolution first (embedded store or .valet.toml link).
	s, err := store.Resolve(id)
	if err == nil {
		return s, nil
	}

	// Fallback: personal-only mode — open the first linked store from .valet.local.toml.
	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		return nil, err // return original error
	}
	configPath, isLocalOnly, findErr := config.FindValetConfig(cwd)
	if findErr != nil || !isLocalOnly {
		return nil, err
	}
	configDir := filepath.Dir(configPath)
	lc, lcErr := config.LoadLocalConfig(configDir)
	if lcErr != nil || len(lc.Stores) == 0 {
		return nil, err
	}
	return store.FindStoreByName(lc.Stores[0].Name, id)
}

// openAllStores returns all linked stores in resolution order
// (local/personal → shared/team → embedded → local overrides). Used by drive/sync/status.
// Supports personal-only mode (only .valet.local.toml, no .valet.toml).
func openAllStores() ([]store.LinkedStore, error) {
	id, err := loadIdentity()
	if err != nil {
		return nil, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	configPath, isLocalOnly, err := config.FindValetConfig(cwd)
	if err != nil {
		// Last resort: try to resolve a store directly.
		primary, resolveErr := store.Resolve(id)
		if resolveErr != nil {
			return nil, err
		}
		return []store.LinkedStore{{Store: primary}}, nil
	}

	configDir := filepath.Dir(configPath)

	if isLocalOnly {
		// Personal-only mode: only .valet.local.toml exists.
		// No embedded store, just linked personal stores.
		lc, err := config.LoadLocalConfig(configDir)
		if err != nil {
			return nil, err
		}
		return store.OpenLinkedStores(lc.Stores, nil, nil, nil, id), nil
	}

	// Normal mode: .valet.toml exists.
	vc, err := config.LoadValetToml(configPath)
	if err != nil {
		return nil, err
	}

	// Try to open the embedded/primary store.
	primary, _ := store.Resolve(id)

	lc, _ := config.LoadLocalConfig(configDir)
	localStore := store.OpenLocalStore(configDir, id)

	return store.OpenLinkedStores(lc.Stores, vc.Stores, primary, localStore, id), nil
}

// resolveEnv returns the environment to use (flag override or default).
func resolveEnv(s *store.Store) string {
	if envFlag != "" {
		return envFlag
	}
	return "dev"
}

// resolveProject returns the project slug to use.
func resolveProject(s *store.Store) (string, error) {
	if projectFlag != "" {
		return projectFlag, nil
	}
	if s.DefaultProject != "" {
		return s.DefaultProject, nil
	}
	return "", fmt.Errorf("no project specified — use --project or link to a store with 'valet init --store'")
}
