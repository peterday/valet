package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/store"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive setup — bind all required secrets from .valet.toml",
	Long: `Walk through each requirement in .valet.toml and configure it.
Secrets already found in linked stores are auto-resolved.
Unlinked stores with matching secrets are offered for linking.`,
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

		if len(vc.Requires) == 0 {
			fmt.Println("No requirements declared in .valet.toml")
			fmt.Println("Add requirements with: valet require OPENAI_API_KEY --provider openai")
			return nil
		}

		env := "dev"
		if envFlag != "" {
			env = envFlag
		}

		id, err := loadIdentityOrInit()
		if err != nil {
			return err
		}

		// Resolve what's already available across all linked stores.
		stores, err := openAllStores()
		if err != nil {
			return err
		}

		resolved, err := store.ResolveAllSecrets(stores, env)
		if err != nil {
			return err
		}

		// We need the primary store for writing new secrets.
		primary, err := openStore()
		if err != nil {
			return err
		}
		project, err := resolveProject(primary)
		if err != nil {
			return err
		}

		reader := bufio.NewReader(os.Stdin)
		autoResolved := 0
		linked := 0
		set := 0
		skipped := 0

		for name, req := range vc.Requires {
			// Check if already resolved from any linked store.
			if rs, found := resolved[name]; found {
				fmt.Printf("  %-30s %s from %s\n", name, green("found"), rs.StoreName)
				autoResolved++
				continue
			}

			// Search all local stores (not just linked ones) for this secret.
			matches, _ := store.SearchStoresForSecret(name, env, id)
			if len(matches) > 0 {
				if len(matches) == 1 {
					m := matches[0]
					fmt.Printf("  %-30s found in %s. Link? [Y/n]: ", name, m.StoreName)
					answer, _ := reader.ReadString('\n')
					answer = strings.TrimSpace(strings.ToLower(answer))
					if answer == "" || answer == "y" || answer == "yes" {
						// Link the store.
						lc, _ := config.LoadLocalConfig(tomlDir)
						alreadyLinked := false
						for _, s := range lc.Stores {
							if s == m.StoreName {
								alreadyLinked = true
								break
							}
						}
						if !alreadyLinked {
							lc.Stores = append(lc.Stores, m.StoreName)
							config.WriteLocalConfig(tomlDir, lc)
							ensureInGitignore(tomlDir, config.ValetLocalToml)
							fmt.Printf("    Linked %s\n", m.StoreName)
							linked++
						}
						autoResolved++
						continue
					}
				} else {
					fmt.Printf("  %-30s found in multiple stores:\n", name)
					for i, m := range matches {
						preview := m.Value
						if len(preview) > 12 {
							preview = preview[:4] + "..." + preview[len(preview)-4:]
						}
						fmt.Printf("    %d. %-20s %s\n", i+1, m.StoreName, preview)
					}
					fmt.Printf("  Choose [1-%d / enter value]: ", len(matches))
					answer, _ := reader.ReadString('\n')
					answer = strings.TrimSpace(answer)

					if idx := parseChoice(answer, len(matches)); idx >= 0 {
						m := matches[idx]
						lc, _ := config.LoadLocalConfig(tomlDir)
						alreadyLinked := false
						for _, s := range lc.Stores {
							if s == m.StoreName {
								alreadyLinked = true
								break
							}
						}
						if !alreadyLinked {
							lc.Stores = append(lc.Stores, m.StoreName)
							config.WriteLocalConfig(tomlDir, lc)
							ensureInGitignore(tomlDir, config.ValetLocalToml)
							fmt.Printf("    Linked %s\n", m.StoreName)
							linked++
						}
						autoResolved++
						continue
					}
					// If they typed a value instead of a number, use it below.
					if answer != "" {
						scope := env + "/default"
						if req.Scope != "" {
							scope = env + "/" + req.Scope
						}
						if err := primary.SetSecret(project, scope, name, answer); err != nil {
							return fmt.Errorf("setting %s: %w", name, err)
						}
						set++
						continue
					}
				}
			}

			// Not found anywhere. Prompt for value.
			scope := env + "/default"
			if req.Scope != "" {
				scope = env + "/" + req.Scope
			}

			label := name
			if req.Description != "" {
				label = fmt.Sprintf("%s (%s)", name, req.Description)
			}
			if req.Provider != "" {
				label = fmt.Sprintf("%s [%s]", label, req.Provider)
			}

			if req.Optional {
				fmt.Printf("  %s (optional, enter to skip): ", label)
			} else {
				fmt.Printf("  %s: ", label)
			}

			value, _ := reader.ReadString('\n')
			value = strings.TrimSpace(value)

			if value == "" {
				if req.Optional {
					skipped++
					continue
				}
				fmt.Printf("    skipped (set later: valet secret set %s --scope %s)\n", name, scope)
				skipped++
				continue
			}

			if req.Provider != "" {
				if err := primary.SetSecretWithProvider(project, scope, name, value, req.Provider); err != nil {
					return fmt.Errorf("setting %s: %w", name, err)
				}
			} else {
				if err := primary.SetSecret(project, scope, name, value); err != nil {
					return fmt.Errorf("setting %s: %w", name, err)
				}
			}
			set++
		}

		fmt.Printf("\nDone. %d found, %d linked, %d set, %d skipped.\n", autoResolved, linked, set, skipped)
		return nil
	},
}

func parseChoice(s string, max int) int {
	if len(s) != 1 {
		return -1
	}
	n := int(s[0] - '0')
	if n >= 1 && n <= max {
		return n - 1
	}
	return -1
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
