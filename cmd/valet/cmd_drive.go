package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/store"
)

var driveCmd = &cobra.Command{
	Use:   "drive [-- command ...]",
	Short: "Run a command with secrets injected as environment variables",
	Long: `Drive a command with all secrets from the current environment injected.
Secrets are merged from all linked stores (personal → team → project).

  valet drive -- uv run app.py
  valet drive -e prod -- node server.js
  valet drive --scope dev/runtime -- npm start`,
	DisableFlagParsing: false,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("usage: valet drive -- <command> [args...]")
		}

		env := "dev"
		if envFlag != "" {
			env = envFlag
		}

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

		if len(secrets) == 0 {
			fmt.Fprintf(os.Stderr, "warning: no secrets found in %s\n", env)
		}

		environ := os.Environ()
		for k, v := range secrets {
			environ = append(environ, k+"="+v)
		}

		binary, err := exec.LookPath(args[0])
		if err != nil {
			return fmt.Errorf("command not found: %s", args[0])
		}

		return syscall.Exec(binary, args, environ)
	},
}

func init() {
	rootCmd.AddCommand(driveCmd)
}
