package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var importScopeFlag string

var importCmd = &cobra.Command{
	Use:   "import <file>",
	Short: "Import secrets from a .env file",
	Long: `Import key=value pairs from a .env file into the current store.
Existing secrets with the same name are skipped unless --overwrite is set.

  valet import .env                          # import into dev/default
  valet import .env -e prod                  # import into prod/default
  valet import .env --scope dev/runtime      # import into specific scope
  valet import .env.production -e prod       # import prod secrets`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]

		s, err := openStore()
		if err != nil {
			return err
		}
		project, err := resolveProject(s)
		if err != nil {
			return err
		}

		env := resolveEnv(s)
		scope := importScopeFlag
		if scope == "" {
			scope = env + "/default"
		}

		// Parse .env file.
		secrets, err := parseDotenv(filePath)
		if err != nil {
			return err
		}

		if len(secrets) == 0 {
			fmt.Println("No secrets found in file.")
			return nil
		}

		// Get existing secrets to detect conflicts.
		existing, _ := s.ListSecretsInEnv(project, env)

		imported := 0
		skipped := 0
		overwrite, _ := cmd.Flags().GetBool("overwrite")

		for key, value := range secrets {
			if _, found := existing[key]; found && !overwrite {
				fmt.Printf("  %-30s skipped (already exists)\n", key)
				skipped++
				continue
			}
			if err := s.SetSecret(project, scope, key, value); err != nil {
				return fmt.Errorf("setting %s: %w", key, err)
			}
			fmt.Printf("  %-30s imported\n", key)
			imported++
		}

		fmt.Printf("\nDone. %d imported, %d skipped.\n", imported, skipped)
		if skipped > 0 && !overwrite {
			fmt.Println("Use --overwrite to replace existing secrets.")
		}
		return nil
	},
}

// parseDotenv reads a .env file and returns key=value pairs.
func parseDotenv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	secrets := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split on first =.
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		// Strip surrounding quotes.
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		// Skip export prefix.
		key = strings.TrimPrefix(key, "export ")

		if key != "" {
			secrets[key] = value
		}
	}

	return secrets, scanner.Err()
}

func init() {
	importCmd.Flags().StringVar(&importScopeFlag, "scope", "", "target scope (default: <env>/default)")
	importCmd.Flags().Bool("overwrite", false, "overwrite existing secrets")
	rootCmd.AddCommand(importCmd)
}
