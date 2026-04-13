package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/provider"
)

// writeRequires writes either .valet.toml (shared) or .valet.local.toml (personal),
// based on which config object is non-nil.
func writeRequires(tomlPath, tomlDir string, vc *domain.ValetConfig, lc *domain.LocalConfig) error {
	if lc != nil {
		if err := config.WriteLocalConfig(tomlDir, lc); err != nil {
			return err
		}
		ensureInGitignore(tomlDir, config.ValetLocalToml)
		return nil
	}
	return config.WriteValetToml(tomlPath, vc)
}

var (
	requireProviderFlag    string
	requireDescriptionFlag string
	requireOptionalFlag    bool
	requireScopeFlag       string
	requirePersonalFlag    bool
)

var requireCmd = &cobra.Command{
	Use:   "require [KEY]",
	Short: "Declare that this project needs a secret",
	Long: `Add a requirement to .valet.toml. This declares what the project needs
without storing any values. Teammates see requirements when they run 'valet setup'.

Single key:
  valet require OPENAI_API_KEY --provider openai
  valet require DATABASE_URL --description "Postgres connection string"
  valet require SENTRY_DSN --optional

All keys from a provider:
  valet require --provider stripe                        # all Stripe env vars
  valet require --provider supabase --optional           # all Supabase env vars, optional`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		tomlPath, err := config.FindValetToml(cwd)
		if err != nil {
			return fmt.Errorf("no .valet.toml found — run 'valet init' first")
		}
		tomlDir := filepath.Dir(tomlPath)

		vc, err := config.LoadValetToml(tomlPath)
		if err != nil {
			return err
		}

		// In personal mode, write to .valet.local.toml instead of .valet.toml.
		var localCfg *domain.LocalConfig
		var requires map[string]domain.Requirement
		if requirePersonalFlag {
			localCfg, _ = config.LoadLocalConfig(tomlDir)
			if localCfg == nil {
				localCfg = &domain.LocalConfig{}
			}
			if localCfg.Requires == nil {
				localCfg.Requires = make(map[string]domain.Requirement)
			}
			requires = localCfg.Requires
		} else {
			if vc.Requires == nil {
				vc.Requires = make(map[string]domain.Requirement)
			}
			requires = vc.Requires
		}

		// Provider-only mode: declare all env vars from the provider.
		if len(args) == 0 {
			if requireProviderFlag == "" {
				return fmt.Errorf("provide a KEY or use --provider to require all keys from a provider")
			}
			p := provider.Get(requireProviderFlag)
			if p == nil {
				return fmt.Errorf("unknown provider %q — run 'valet providers list' to see available providers", requireProviderFlag)
			}
			for _, ev := range p.EnvVars {
				req := domain.Requirement{
					Provider: requireProviderFlag,
					Optional: requireOptionalFlag,
				}
				if requireScopeFlag != "" {
					req.Scope = requireScopeFlag
				}
				mergeRequirement(requires, ev.Name, req)
			}
			if err := writeRequires(tomlPath, tomlDir, vc, localCfg); err != nil {
				return err
			}
			for _, ev := range p.EnvVars {
				label := ev.Name + fmt.Sprintf(" [%s]", requireProviderFlag)
				if requireOptionalFlag {
					label += " (optional)"
				}
				fmt.Printf("Required: %s\n", label)
			}
			return nil
		}

		// Single key mode.
		key := args[0]
		req := domain.Requirement{
			Provider:    requireProviderFlag,
			Description: requireDescriptionFlag,
			Optional:    requireOptionalFlag,
			Scope:       requireScopeFlag,
		}
		mergeRequirement(requires, key, req)

		if err := writeRequires(tomlPath, tomlDir, vc, localCfg); err != nil {
			return err
		}

		label := key
		if req.Provider != "" {
			label += fmt.Sprintf(" [%s]", req.Provider)
		}
		if requireOptionalFlag {
			label += " (optional)"
		}
		fmt.Printf("Required: %s\n", label)
		return nil
	},
}

// mergeRequirement adds or updates a requirement, preserving existing fields.
func mergeRequirement(requires map[string]domain.Requirement, key string, req domain.Requirement) {
	if existing, ok := requires[key]; ok {
		if req.Provider == "" {
			req.Provider = existing.Provider
		}
		if req.Description == "" {
			req.Description = existing.Description
		}
		if !req.Optional {
			req.Optional = existing.Optional
		}
		if req.Scope == "" {
			req.Scope = existing.Scope
		}
	}
	requires[key] = req
}

func init() {
	requireCmd.Flags().StringVar(&requireProviderFlag, "provider", "", "provider name (e.g. openai, stripe, aws)")
	requireCmd.Flags().StringVar(&requireDescriptionFlag, "description", "", "human-readable description")
	requireCmd.Flags().BoolVar(&requireOptionalFlag, "optional", false, "mark as optional")
	requireCmd.Flags().StringVar(&requireScopeFlag, "scope", "", "default scope for this secret (e.g. db, runtime)")
	requireCmd.Flags().BoolVar(&requirePersonalFlag, "personal", false, "save to .valet.local.toml (gitignored) instead of .valet.toml")
	rootCmd.AddCommand(requireCmd)
}
