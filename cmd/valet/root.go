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
func openStore() (*store.Store, error) {
	id, err := loadIdentity()
	if err != nil {
		return nil, err
	}
	if storeFlag != "" {
		return store.FindStoreByName(storeFlag, id)
	}
	return store.Resolve(id)
}

// openAllStores returns all linked stores in resolution order
// (local/personal → shared/team → embedded). Used by drive/sync/status.
func openAllStores() ([]*store.Store, error) {
	id, err := loadIdentity()
	if err != nil {
		return nil, err
	}

	// Try to open the embedded/primary store.
	primary, err := store.Resolve(id)
	if err != nil {
		return nil, err
	}

	// Load .valet.toml and .valet.local.toml.
	cwd, err := os.Getwd()
	if err != nil {
		return []*store.Store{primary}, nil
	}

	tomlPath, err := config.FindValetToml(cwd)
	if err != nil {
		return []*store.Store{primary}, nil
	}

	vc, err := config.LoadValetToml(tomlPath)
	if err != nil {
		return []*store.Store{primary}, nil
	}

	tomlDir := filepath.Dir(tomlPath)
	lc, _ := config.LoadLocalConfig(tomlDir)

	return store.OpenLinkedStores(lc.Stores, vc.Stores, primary, id), nil
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
