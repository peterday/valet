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

var linkSharedFlag bool

var linkCmd = &cobra.Command{
	Use:   "link <store-name-or-remote>",
	Short: "Link a store to this project",
	Long: `Link a store so its secrets are available via drive/sync/status.

  valet link my-keys                          # local personal store
  valet link github:pday/my-keys              # personal store (clones if needed)
  valet link acme-secrets --shared            # team store (committed to .valet.toml)
  valet link github:acme/secrets --shared     # team store (clones + commits)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ref := args[0]

		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		tomlPath, err := config.FindValetToml(cwd)
		if err != nil {
			return fmt.Errorf("no .valet.toml found — run 'valet init' first")
		}
		tomlDir := filepath.Dir(tomlPath)

		id, err := loadIdentityOrInit()
		if err != nil {
			return err
		}

		uri := store.ParseStoreURI(ref)

		if uri.IsRemote {
			_, findErr := store.FindStoreByName(uri.StoreName, id)
			if findErr != nil {
				cfg, err := config.Load()
				if err != nil {
					return err
				}
				destPath := filepath.Join(cfg.StoresDir, uri.StoreName)
				if err := os.MkdirAll(cfg.StoresDir, 0755); err != nil {
					return err
				}
				fmt.Printf("Cloning %s...\n", uri.Remote)
				if err := store.Clone(uri.Remote, destPath); err != nil {
					return fmt.Errorf("clone failed: %w", err)
				}
			}
		} else {
			if _, err := store.FindStoreByName(uri.StoreName, id); err != nil {
				return err
			}
		}

		link := domain.StoreLink{Name: uri.StoreName}
		if uri.IsRemote {
			link.URL = uri.Remote
		}

		if linkSharedFlag {
			// Write to .valet.toml (committed).
			vc, err := config.LoadValetToml(tomlPath)
			if err != nil {
				return err
			}

			if store.HasStoreLink(vc.Stores, link.Name) {
				fmt.Printf("Store %q already linked (shared)\n", link.Name)
				return nil
			}
			vc.Stores = append(vc.Stores, link)
			if err := config.WriteValetToml(tomlPath, vc); err != nil {
				return err
			}
			fmt.Printf("Linked %q (shared, in .valet.toml)\n", link.Name)
		} else {
			// Write to .valet.local.toml (gitignored).
			lc, _ := config.LoadLocalConfig(tomlDir)

			if store.HasStoreLink(lc.Stores, link.Name) {
				fmt.Printf("Store %q already linked (local)\n", link.Name)
				return nil
			}
			lc.Stores = append(lc.Stores, link)
			if err := config.WriteLocalConfig(tomlDir, lc); err != nil {
				return err
			}

			// Ensure .valet.local.toml is in .gitignore.
			ensureInGitignore(tomlDir, config.ValetLocalToml)

			fmt.Printf("Linked %q (local, in .valet.local.toml)\n", link.Name)
		}

		return nil
	},
}

var unlinkCmd = &cobra.Command{
	Use:   "unlink <store-name-or-remote>",
	Short: "Unlink a store from this project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ref := args[0]

		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		tomlPath, err := config.FindValetToml(cwd)
		if err != nil {
			return fmt.Errorf("no .valet.toml found")
		}
		tomlDir := filepath.Dir(tomlPath)

		removed := false

		// Try removing from .valet.local.toml.
		lc, _ := config.LoadLocalConfig(tomlDir)
		if newStores, ok := removeStoreLink(lc.Stores, ref); ok {
			lc.Stores = newStores
			if err := config.WriteLocalConfig(tomlDir, lc); err != nil {
				return err
			}
			fmt.Printf("Unlinked %q from .valet.local.toml\n", ref)
			removed = true
		}

		// Try removing from .valet.toml.
		vc, err := config.LoadValetToml(tomlPath)
		if err != nil {
			if !removed {
				return err
			}
			return nil
		}
		if newStores, ok := removeStoreLink(vc.Stores, ref); ok {
			vc.Stores = newStores
			if err := config.WriteValetToml(tomlPath, vc); err != nil {
				return err
			}
			fmt.Printf("Unlinked %q from .valet.toml\n", ref)
			removed = true
		}

		if !removed {
			return fmt.Errorf("store %q not found in links", ref)
		}

		return nil
	},
}

func removeStoreLink(links []domain.StoreLink, name string) ([]domain.StoreLink, bool) {
	for i, l := range links {
		if l.Name == name {
			return append(links[:i], links[i+1:]...), true
		}
	}
	return links, false
}

func ensureInGitignore(dir, filename string) {
	gitignorePath := filepath.Join(dir, ".gitignore")
	data, _ := os.ReadFile(gitignorePath)

	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == filename {
			return
		}
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	if len(data) > 0 && data[len(data)-1] != '\n' {
		f.WriteString("\n")
	}
	f.WriteString(filename + "\n")
}

func init() {
	linkCmd.Flags().BoolVar(&linkSharedFlag, "shared", false, "commit link to .valet.toml (for team stores)")
	rootCmd.AddCommand(linkCmd)
	rootCmd.AddCommand(unlinkCmd)
}
