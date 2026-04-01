package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var scopeCmd = &cobra.Command{
	Use:   "scope",
	Short: "Manage scopes (access + encryption boundaries)",
}

var scopeCreateCmd = &cobra.Command{
	Use:   "create <env/path>",
	Short: "Create a new scope",
	Long:  `Create a scope like "dev/runtime" or "prod/integrations/stripe".`,
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
		if err := s.CreateScope(project, args[0]); err != nil {
			return err
		}
		fmt.Printf("Created scope %q\n", args[0])
		return nil
	},
}

var scopeListCmd = &cobra.Command{
	Use:   "list [env]",
	Short: "List scopes in an environment",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		project, err := resolveProject(s)
		if err != nil {
			return err
		}

		env := resolveEnv(s)
		if len(args) > 0 {
			env = args[0]
		}

		scopes, err := s.ListScopes(project, env)
		if err != nil {
			return err
		}
		if len(scopes) == 0 {
			fmt.Printf("No scopes in %s. Create one with: valet scope create %s/runtime\n", env, env)
			return nil
		}
		fmt.Printf("%-30s  %-10s  %s\n", "SCOPE", "SECRETS", "RECIPIENTS")
		fmt.Printf("%-30s  %-10s  %s\n", "-----", "-------", "----------")
		for _, sc := range scopes {
			fmt.Printf("%-30s  %-10d  %d\n", sc.Path, len(sc.Secrets), len(sc.Recipients))
		}
		return nil
	},
}

var scopeAddRecipientCmd = &cobra.Command{
	Use:   "add-recipient <user>",
	Short: "Add a user as a recipient on a scope",
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
		if err := s.AddRecipient(project, scopeFlag, args[0]); err != nil {
			return err
		}
		fmt.Printf("Added %q to scope %q\n", args[0], scopeFlag)
		return nil
	},
}

var scopeRemoveRecipientCmd = &cobra.Command{
	Use:   "remove-recipient <user>",
	Short: "Remove a user from a scope",
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
		if err := s.RemoveRecipient(project, scopeFlag, args[0]); err != nil {
			return err
		}
		fmt.Printf("Removed %q from scope %q\n", args[0], scopeFlag)
		return nil
	},
}

func init() {
	scopeCmd.AddCommand(scopeCreateCmd)
	scopeCmd.AddCommand(scopeListCmd)
	scopeCmd.AddCommand(scopeAddRecipientCmd)
	scopeCmd.AddCommand(scopeRemoveRecipientCmd)
	rootCmd.AddCommand(scopeCmd)
}
