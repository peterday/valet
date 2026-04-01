package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/store"
)

var (
	initStoreFlag    string
	initEmbeddedFlag bool
	initLocalFlag    string
	initSharedFlag   string
)

var initCmd = &cobra.Command{
	Use:   "init [project-name]",
	Short: "Initialize valet for the current project",
	Long: `Initialize valet in the current directory. Choose a store mode:

  valet init                                    # embedded store in .valet/ (default)
  valet init --local my-keys                    # link a personal store
  valet init --local github:pday/my-keys        # link personal store (clones if needed)
  valet init --shared github:acme/secrets       # link a shared team store
  valet init --shared acme-secrets              # link shared store by name

Embedded stores keep secrets in the repo (encrypted). Local and shared
stores keep secrets separate — only .valet.toml lives in the repo.

Combine modes to layer stores:
  valet init --shared github:acme/secrets --local my-keys`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		// For embedded stores, use a fixed internal name.
		// For linked stores, project comes from the URI.
		projectName := "default"
		if len(args) > 0 {
			projectName = args[0]
		}

		// Legacy: --store flag
		if initStoreFlag != "" {
			return initLinked(cwd, projectName, initStoreFlag)
		}

		// Determine mode from flags.
		hasLocal := initLocalFlag != ""
		hasShared := initSharedFlag != ""
		hasEmbedded := initEmbeddedFlag || (!hasLocal && !hasShared)

		tomlPath := filepath.Join(cwd, ".valet.toml")
		vc := &domain.ValetConfig{
			Project:    projectName,
			DefaultEnv: "dev",
		}

		id, err := loadIdentityOrInit()
		if err != nil {
			return err
		}

		// Embedded store.
		if hasEmbedded {
			storeRoot := filepath.Join(cwd, ".valet")
			if _, err := os.Stat(filepath.Join(storeRoot, "store.json")); err == nil {
				if !hasLocal && !hasShared {
					return fmt.Errorf("valet already initialized in this directory")
				}
				// Already has embedded, just adding links.
			} else {
				s, err := store.Create(storeRoot, projectName, domain.StoreTypeEmbedded, id)
				if err != nil {
					return err
				}
				if _, err := s.AddUser("me", "", id.PublicKey); err != nil {
					return err
				}
				if _, err := s.CreateProject(projectName); err != nil {
					return err
				}
				if err := s.CreateEnvironment(projectName, "dev"); err != nil {
					return err
				}
				if err := s.CreateScope(projectName, "dev/default"); err != nil {
					return err
				}
				vc.Store = "."
			}
		}

		// Shared store link.
		if hasShared {
			uri := store.ParseStoreURI(initSharedFlag)

			if uri.IsRemote {
				// Clone if not already local.
				if _, err := store.FindStoreByName(uri.StoreName, id); err != nil {
					cfg, cfgErr := config.Load()
					if cfgErr != nil {
						return cfgErr
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
			} else if _, err := store.FindStoreByName(uri.StoreName, id); err != nil {
				return err
			}

			// Store the full ref (with project if specified) in .valet.toml.
			vc.Stores = append(vc.Stores, initSharedFlag)

			// If no embedded store, use the shared store as primary.
			if !hasEmbedded {
				vc.Store = initSharedFlag
			}
		}

		// Write .valet.toml.
		if err := config.WriteValetToml(tomlPath, vc); err != nil {
			return err
		}

		// Local store link (gitignored).
		if hasLocal {
			tomlDir := filepath.Dir(tomlPath)
			lc, _ := config.LoadLocalConfig(tomlDir)

			uri := store.ParseStoreURI(initLocalFlag)

			if uri.IsRemote {
				// Clone if not already local.
				if _, err := store.FindStoreByName(uri.StoreName, id); err != nil {
					cfg, cfgErr := config.Load()
					if cfgErr != nil {
						return cfgErr
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
			} else if _, err := store.FindStoreByName(uri.StoreName, id); err != nil {
				return fmt.Errorf("personal store %q not found — create it with: valet store create %s", uri.StoreName, uri.StoreName)
			}

			// Store the full ref (with project if specified).
			alreadyLinked := false
			for _, s := range lc.Stores {
				if s == initLocalFlag {
					alreadyLinked = true
					break
				}
			}
			if !alreadyLinked {
				lc.Stores = append(lc.Stores, initLocalFlag)
				if err := config.WriteLocalConfig(tomlDir, lc); err != nil {
					return err
				}
				ensureInGitignore(tomlDir, config.ValetLocalToml)
			}
		}

		// Print summary.
		if hasEmbedded {
			fmt.Printf("Initialized embedded store for %q\n", projectName)
		}
		if hasShared {
			fmt.Printf("Linked shared store: %s\n", initSharedFlag)
		}
		if hasLocal {
			fmt.Printf("Linked personal store: %s\n", initLocalFlag)
		}

		// Detect project type and write CLAUDE.md snippet.
		hint := detectProject(cwd)
		wrote, _ := writeClaudeMDSnippet(cwd, hint)
		printClaudeMDHint(wrote, cwd)

		fmt.Println("\nReady to go:")
		fmt.Println("  valet secret set MY_KEY --value secret123")
		fmt.Println("  valet drive -- your-command")

		return nil
	},
}

func initLinked(cwd, projectName, storeName string) error {
	id, err := loadIdentity()
	if err != nil {
		return fmt.Errorf("no identity found — run 'valet identity init' first")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	storePath := filepath.Join(cfg.StoresDir, storeName)
	if _, err := os.Stat(filepath.Join(storePath, "store.json")); os.IsNotExist(err) {
		return fmt.Errorf("store %q not found at %s — create it with 'valet store create %s'", storeName, storePath, storeName)
	}

	if _, err := store.Open(storePath, id); err != nil {
		return err
	}

	tomlPath := filepath.Join(cwd, ".valet.toml")
	vc := &domain.ValetConfig{
		Store:      storeName,
		Project:    projectName,
		DefaultEnv: "dev",
	}
	if err := config.WriteValetToml(tomlPath, vc); err != nil {
		return err
	}

	fmt.Printf("Linked project %q to store %q\n", projectName, storeName)
	return nil
}

func init() {
	initCmd.Flags().StringVar(&initStoreFlag, "store", "", "link to existing named store")
	initCmd.Flags().BoolVar(&initEmbeddedFlag, "embedded", false, "create embedded store in .valet/ (default)")
	initCmd.Flags().StringVar(&initLocalFlag, "local", "", "link personal store (name or remote URL)")
	initCmd.Flags().StringVar(&initSharedFlag, "shared", "", "link shared/team store (name or remote URL)")
	rootCmd.AddCommand(initCmd)
}
