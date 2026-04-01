package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/domain"
)

var (
	requireProviderFlag    string
	requireDescriptionFlag string
	requireOptionalFlag    bool
	requireScopeFlag       string
)

var requireCmd = &cobra.Command{
	Use:   "require <KEY>",
	Short: "Declare that this project needs a secret",
	Long: `Add a requirement to .valet.toml. This declares what the project needs
without storing any values. Teammates see requirements when they run 'valet setup'.

  valet secret require OPENAI_API_KEY --provider openai
  valet secret require DATABASE_URL --description "Postgres connection string"
  valet secret require SENTRY_DSN --optional`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]

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

		if vc.Requires == nil {
			vc.Requires = make(map[string]domain.Requirement)
		}

		req := domain.Requirement{
			Provider:    requireProviderFlag,
			Description: requireDescriptionFlag,
			Optional:    requireOptionalFlag,
			Scope:       requireScopeFlag,
		}

		// Merge with existing if already declared.
		if existing, ok := vc.Requires[key]; ok {
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

		vc.Requires[key] = req

		if err := config.WriteValetToml(tomlPath, vc); err != nil {
			return err
		}

		label := key
		if req.Provider != "" {
			label += fmt.Sprintf(" [%s]", req.Provider)
		}
		if req.Optional {
			label += " (optional)"
		}
		fmt.Printf("Required: %s\n", label)
		return nil
	},
}

func init() {
	requireCmd.Flags().StringVar(&requireProviderFlag, "provider", "", "provider name (e.g. openai, stripe, aws)")
	requireCmd.Flags().StringVar(&requireDescriptionFlag, "description", "", "human-readable description")
	requireCmd.Flags().BoolVar(&requireOptionalFlag, "optional", false, "mark as optional")
	requireCmd.Flags().StringVar(&requireScopeFlag, "scope", "", "default scope for this secret (e.g. db, runtime)")
	secretCmd.AddCommand(requireCmd)
}
