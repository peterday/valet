package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/superset-studio/valet/internal/store"
)

var secretValueFlag string

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage secrets",
}

var secretProviderFlag string

var secretSetCmd = &cobra.Command{
	Use:   "set <KEY>",
	Short: "Set a secret value in a scope",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		scope := scopeFlag
		if scope == "" {
			// Default to <env>/default scope.
			env := envFlag
			if env == "" {
				env = "dev"
			}
			scope = env + "/default"
		}

		s, err := openStore()
		if err != nil {
			return err
		}
		project, err := resolveProject(s)
		if err != nil {
			return err
		}

		value := secretValueFlag
		if value == "" {
			// Check if stdin is piped.
			stat, _ := os.Stdin.Stat()
			if (stat.Mode() & os.ModeCharDevice) == 0 {
				scanner := bufio.NewScanner(os.Stdin)
				if scanner.Scan() {
					value = scanner.Text()
				}
			} else {
				fmt.Printf("Value for %s: ", args[0])
				reader := bufio.NewReader(os.Stdin)
				value, _ = reader.ReadString('\n')
				value = strings.TrimSpace(value)
			}
		}

		if value == "" {
			return fmt.Errorf("value is required")
		}

		if secretProviderFlag != "" {
			if err := s.SetSecretWithProvider(project, scope, args[0], value, secretProviderFlag); err != nil {
				return err
			}
		} else {
			if err := s.SetSecret(project, scope, args[0], value); err != nil {
				return err
			}
		}
		fmt.Printf("Set %s in %s\n", args[0], scope)
		return nil
	},
}

var secretGetCmd = &cobra.Command{
	Use:   "get <KEY>",
	Short: "Get a secret value",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		project, err := resolveProject(s)
		if err != nil {
			return err
		}

		if scopeFlag != "" {
			secret, err := s.GetSecret(project, scopeFlag, args[0])
			if err != nil {
				return err
			}
			fmt.Print(secret.Value)
			return nil
		}

		// Search across all scopes in the environment.
		env := resolveEnv(s)
		secret, scope, err := s.GetSecretFromEnv(project, env, args[0])
		if err != nil {
			return err
		}
		_ = scope
		fmt.Print(secret.Value)
		return nil
	},
}

var secretListCmd = &cobra.Command{
	Use:   "list",
	Short: "List secrets",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		project, err := resolveProject(s)
		if err != nil {
			return err
		}

		if scopeFlag != "" {
			scopes, err := s.ListScopes(project, envFromScope(scopeFlag))
			if err != nil {
				return err
			}
			for _, sc := range scopes {
				if sc.Path == scopeFlag {
					for _, name := range sc.Secrets {
						fmt.Println(name)
					}
					return nil
				}
			}
			return fmt.Errorf("scope %q not found", scopeFlag)
		}

		env := resolveEnv(s)
		secrets, err := s.ListSecretsInEnv(project, env)
		if err != nil {
			return err
		}
		if len(secrets) == 0 {
			fmt.Printf("No secrets in %s\n", env)
			return nil
		}
		for name, scope := range secrets {
			fmt.Printf("%-30s  %s\n", name, scope)
		}
		return nil
	},
}

var secretRemoveCmd = &cobra.Command{
	Use:   "remove <KEY>",
	Short: "Remove a secret from a scope",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if scopeFlag == "" {
			return fmt.Errorf("--scope is required")
		}
		s, err := openStore()
		if err != nil {
			return err
		}
		project, err := resolveProject(s)
		if err != nil {
			return err
		}
		if err := s.RemoveSecret(project, scopeFlag, args[0]); err != nil {
			return err
		}
		fmt.Printf("Removed %s from %s\n", args[0], scopeFlag)
		return nil
	},
}

var secretHistoryCmd = &cobra.Command{
	Use:   "history <KEY>",
	Short: "Show version history for a secret",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		project, err := resolveProject(s)
		if err != nil {
			return err
		}

		var scopePath string
		if scopeFlag != "" {
			scopePath = scopeFlag
		} else {
			// Search for the secret across all scopes in the env.
			env := resolveEnv(s)
			_, sp, err := s.GetSecretFromEnv(project, env, args[0])
			if err != nil {
				return err
			}
			scopePath = sp
		}

		current, history, err := s.GetSecretHistory(project, scopePath, args[0])
		if err != nil {
			return err
		}

		fmt.Printf("v%d (current)  %s  by %s  %s\n",
			current.Version,
			maskHistoryValue(current.Value),
			current.UpdatedBy,
			current.UpdatedAt.Format("2006-01-02 15:04"))

		for _, h := range history {
			fmt.Printf("v%d            %s  by %s  %s\n",
				h.Version,
				maskHistoryValue(h.Value),
				h.UpdatedBy,
				h.UpdatedAt.Format("2006-01-02 15:04"))
		}

		if len(history) == 0 {
			fmt.Println("(no previous versions)")
		}

		return nil
	},
}

func maskHistoryValue(v string) string {
	if len(v) <= 8 {
		return "****"
	}
	return v[:4] + "..." + v[len(v)-4:]
}

func envFromScope(scopePath string) string {
	parts := strings.SplitN(scopePath, "/", 2)
	return parts[0]
}

var secretSyncToFlag string

var secretSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync resolved secrets into a target store",
	Long: `Resolve all secrets from linked stores, then copy any that are missing
into the target store. Use "." to target the embedded project store.

  valet secret sync --to acme-secrets       # promote to team store
  valet secret sync --to .                  # promote into embedded store
  valet secret sync --to acme-secrets -e prod`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if secretSyncToFlag == "" {
			return fmt.Errorf("--to is required (store name, or \".\" for embedded store)")
		}

		env := "dev"
		if envFlag != "" {
			env = envFlag
		}

		// Open the target store.
		var target *store.Store
		if secretSyncToFlag == "." {
			// Target is the embedded store.
			var err error
			target, err = openStore()
			if err != nil {
				return err
			}
		} else {
			id, err := loadIdentity()
			if err != nil {
				return err
			}
			target, err = store.FindStoreByName(secretSyncToFlag, id)
			if err != nil {
				return err
			}
		}

		// Resolve all secrets from linked stores.
		allStores, err := openAllStores()
		if err != nil {
			return err
		}
		resolved, err := store.ResolveAllSecrets(allStores, env)
		if err != nil {
			return err
		}

		if len(resolved) == 0 {
			fmt.Println("No secrets found to sync.")
			return nil
		}

		// Get what's already in the target store.
		targetProject, err := resolveProjectForStore(target)
		if err != nil {
			return err
		}
		existing, _ := target.GetAllSecretsInEnv(targetProject, env)

		targetName := secretSyncToFlag
		if targetName == "." {
			targetName = target.Meta.Name
		}

		copied := 0
		skipped := 0
		for key, rs := range resolved {
			if _, found := existing[key]; found {
				fmt.Printf("  %-30s already in %s, skipped\n", key, targetName)
				skipped++
				continue
			}
			scope := env + "/default"
			if err := target.SetSecret(targetProject, scope, key, rs.Value); err != nil {
				return fmt.Errorf("copying %s: %w", key, err)
			}
			fmt.Printf("  %-30s copied from %s\n", key, rs.StoreName)
			copied++
		}

		fmt.Printf("\nDone. %d copied, %d skipped.\n", copied, skipped)
		return nil
	},
}

func resolveProjectForStore(s *store.Store) (string, error) {
	if s.DefaultProject != "" {
		return s.DefaultProject, nil
	}
	projects, err := s.ListProjects()
	if err != nil || len(projects) == 0 {
		return "", fmt.Errorf("target store has no projects — create one first")
	}
	return projects[0].Slug, nil
}

func init() {
	secretSetCmd.Flags().StringVar(&secretValueFlag, "value", "", "secret value (prompted if not provided)")
	secretSetCmd.Flags().StringVar(&secretProviderFlag, "provider", "", "provider name (e.g. openai, stripe, aws)")
	secretSyncCmd.Flags().StringVar(&secretSyncToFlag, "to", "", "target store name")
	secretCmd.AddCommand(secretSetCmd)
	secretCmd.AddCommand(secretGetCmd)
	secretCmd.AddCommand(secretListCmd)
	secretCmd.AddCommand(secretRemoveCmd)
	secretCmd.AddCommand(secretHistoryCmd)
	secretCmd.AddCommand(secretSyncCmd)
	rootCmd.AddCommand(secretCmd)
}
