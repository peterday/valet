package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/superset-studio/valet/internal/config"
	"github.com/superset-studio/valet/internal/domain"
	"github.com/superset-studio/valet/internal/store"
)

var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "Manage stores",
}

var storeCreateCmd = &cobra.Command{
	Use:   "create <name-or-remote>",
	Short: "Create a new store",
	Long: `Create a named store for managing secrets.

Local store:
  valet store create my-secrets

Remote (git-backed) store — name is inferred from the URL:
  valet store create github:acme/api-secrets`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		arg := args[0]

		id, err := loadIdentityOrInit()
		if err != nil {
			return err
		}

		cfg, err := config.Load()
		if err != nil {
			return err
		}

		uri := store.ParseStoreURI(arg)

		var storeType domain.StoreType
		if uri.IsRemote {
			storeType = domain.StoreTypeGit
		} else {
			storeType = domain.StoreTypeLocal
		}

		storePath := filepath.Join(cfg.StoresDir, uri.StoreName)
		if _, err := os.Stat(storePath); err == nil {
			return fmt.Errorf("store %q already exists at %s", uri.StoreName, storePath)
		}

		s, err := store.Create(storePath, uri.StoreName, storeType, id)
		if err != nil {
			return err
		}

		// Add creator as first user.
		if _, err := s.AddUser("me", "", id.PublicKey); err != nil {
			return err
		}

		// Auto-create a default project with dev env + default scope.
		projectName := uri.EffectiveProject()
		if _, err := s.CreateProject(projectName); err != nil {
			return err
		}
		if err := s.CreateEnvironment(projectName, "dev"); err != nil {
			return err
		}
		if err := s.CreateScope(projectName, "dev/default"); err != nil {
			return err
		}
		s.DefaultProject = projectName

		if uri.IsRemote {
			s.Meta.Remote = uri.Remote
			if err := s.InitRepo(); err != nil {
				return fmt.Errorf("git init: %w", err)
			}
			if err := s.SetRemote(uri.Remote); err != nil {
				return fmt.Errorf("setting remote: %w", err)
			}
		}

		fmt.Printf("Created store %q", uri.StoreName)
		if uri.IsRemote {
			fmt.Printf(" (%s)", uri.Remote)
		}
		fmt.Println()

		return nil
	},
}

var storeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List known stores",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		entries, err := os.ReadDir(cfg.StoresDir)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("No stores yet. Create one with: valet store create <name>")
				return nil
			}
			return err
		}

		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			storePath := filepath.Join(cfg.StoresDir, e.Name())
			storeJSON := filepath.Join(storePath, "store.json")
			if _, err := os.Stat(storeJSON); err != nil {
				continue
			}

			// Try to read meta for type/remote info.
			id, _ := loadIdentity()
			if id != nil {
				if s, err := store.Open(storePath, id); err == nil {
					remote := "-"
					if s.Meta.Remote != "" {
						remote = s.Meta.Remote
					}
					fmt.Printf("%-20s %-8s %s\n", e.Name(), s.Meta.Type, remote)
					continue
				}
			}
			fmt.Println(e.Name())
		}
		return nil
	},
}

func init() {
	storeCmd.AddCommand(storeCreateCmd)
	storeCmd.AddCommand(storeListCmd)
	rootCmd.AddCommand(storeCmd)
}
