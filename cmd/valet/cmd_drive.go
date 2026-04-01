package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/store"
)

var driveSetFlags []string

var driveCmd = &cobra.Command{
	Use:     "drive [-- command ...]",
	Aliases: []string{"run"},
	Short:   "Run a command with secrets injected as environment variables",
	Long: `Drive a command with all secrets from the current environment injected.
Secrets are merged from all linked stores, with local overrides on top.

  valet drive -- uv run app.py
  valet drive -e prod -- node server.js
  valet drive --set DATABASE_URL=postgres://test -- npm start
  valet drive --scope dev/runtime -- npm start

--set overrides take highest priority, above all stores.

Alias: valet run`,
	DisableFlagParsing: false,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("usage: valet drive -- <command> [args...]")
		}

		env := "dev"
		if envFlag != "" {
			env = envFlag
		}

		// Parse --set overrides.
		overrides := parseSetFlags(driveSetFlags)

		var secrets map[string]string

		if scopeFlag != "" {
			// Single scope from the primary store only.
			s, err := openStore()
			if err != nil {
				return err
			}
			project, err := resolveProject(s)
			if err != nil {
				return err
			}
			secrets, err = s.GetAllSecretsInScope(project, scopeFlag)
			if err != nil {
				return err
			}
		} else {
			// Merge from all linked stores.
			stores, err := openAllStores()
			if err != nil {
				return err
			}
			secrets, err = store.ResolveAllSecretsFlat(stores, env)
			if err != nil {
				return err
			}
		}

		// Apply --set overrides (highest priority).
		for k, v := range overrides {
			secrets[k] = v
		}

		if len(secrets) == 0 {
			fmt.Fprintf(os.Stderr, "warning: no secrets found in %s\n", env)
		}

		environ := os.Environ()
		for k, v := range secrets {
			if err := store.ValidateEnvVarName(k); err != nil {
				return fmt.Errorf("invalid secret name %q: %w", k, err)
			}
			environ = append(environ, k+"="+v)
		}

		binary, err := exec.LookPath(args[0])
		if err != nil {
			return fmt.Errorf("command not found: %s", args[0])
		}

		return syscall.Exec(binary, args, environ)
	},
}

// parseSetFlags parses KEY=VALUE pairs from --set flags.
func parseSetFlags(flags []string) map[string]string {
	result := make(map[string]string)
	for _, f := range flags {
		if i := strings.IndexByte(f, '='); i > 0 {
			result[f[:i]] = f[i+1:]
		}
	}
	return result
}

func init() {
	driveCmd.Flags().StringArrayVar(&driveSetFlags, "set", nil, "override a secret (KEY=VALUE, repeatable)")
	rootCmd.AddCommand(driveCmd)
}
