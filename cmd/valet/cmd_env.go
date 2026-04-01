package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage environments",
}

var envCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new environment",
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
		if err := s.CreateEnvironment(project, args[0]); err != nil {
			return err
		}
		fmt.Printf("Created environment %q\n", args[0])
		return nil
	},
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List environments",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		project, err := resolveProject(s)
		if err != nil {
			return err
		}
		envs, err := s.ListEnvironments(project)
		if err != nil {
			return err
		}
		if len(envs) == 0 {
			fmt.Println("No environments yet. Create one with: valet env create dev")
			return nil
		}
		for _, e := range envs {
			fmt.Println(e)
		}
		return nil
	},
}

var envGrantCmd = &cobra.Command{
	Use:   "grant <user>",
	Short: "Grant a user access to all scopes in an environment",
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
		env := resolveEnv(s)
		count, err := s.GrantEnvironment(project, env, args[0])
		if err != nil {
			return err
		}
		fmt.Printf("Granted %q access to %d scope(s) in %s\n", args[0], count, env)
		return nil
	},
}

var envRevokeRotateFlag bool

var envRevokeCmd = &cobra.Command{
	Use:   "revoke <user>",
	Short: "Revoke a user's access to all scopes in an environment",
	Long: `Revoke a user from all scopes in an environment, re-encrypting without them.

Use --rotate to also flag all affected secrets as needing rotation:
  valet env revoke bob -e prod --rotate`,
	Args: cobra.ExactArgs(1),
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

		if envRevokeRotateFlag {
			scopeCount, secretCount, err := s.RevokeEnvironmentWithRotation(project, env, args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Revoked %q from %d scope(s) in %s\n", args[0], scopeCount, env)
			if secretCount > 0 {
				fmt.Printf("%d secret(s) flagged for rotation\n", secretCount)
			}
		} else {
			count, err := s.RevokeEnvironment(project, env, args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Revoked %q from %d scope(s) in %s\n", args[0], count, env)
		}
		return nil
	},
}

func init() {
	envRevokeCmd.Flags().BoolVar(&envRevokeRotateFlag, "rotate", false, "flag all affected secrets for rotation")
	envCmd.AddCommand(envCreateCmd)
	envCmd.AddCommand(envListCmd)
	envCmd.AddCommand(envGrantCmd)
	envCmd.AddCommand(envRevokeCmd)
	rootCmd.AddCommand(envCmd)
}
