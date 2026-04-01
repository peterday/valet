package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/store"
)

var (
	resolveShowFlag    bool
	resolveVerboseFlag bool
	resolveSetFlags    []string
)

var resolveCmd = &cobra.Command{
	Use:   "resolve [KEY]",
	Short: "Show resolved secrets and where they come from",
	Long: `Show what secrets would be injected and their source in the resolution chain.
Values are masked by default. Use --show to reveal them.

  valet resolve                                        # all secrets, masked
  valet resolve --show                                 # all secrets, values shown
  valet resolve DATABASE_URL --show                    # single key, raw value (pipeable)
  valet resolve --verbose DATABASE_URL                 # full provenance chain
  valet resolve --set CACHE_URL=redis://localhost      # preview with overrides

Resolution order (highest priority first):
  1. --set overrides (command line, ephemeral)
  2. .valet.local/{env} (local developer overrides)
  3. .valet.local/* (local wildcard)
  4. .valet/{env} (shared project values)
  5. .valet/* (shared project wildcard)
  6. linked stores/{env} (team/personal)
  7. linked stores/* (team/personal wildcard)`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := "dev"
		if envFlag != "" {
			env = envFlag
		}

		stores, err := openAllStores()
		if err != nil {
			return err
		}

		overrides := parseSetFlags(resolveSetFlags)
		resolved, provenance, err := store.ResolveAllSecretsWithProvenance(stores, env, overrides)
		if err != nil {
			return err
		}

		// Single key mode.
		if len(args) == 1 {
			key := args[0]
			rs, found := resolved[key]
			if !found {
				return fmt.Errorf("%s: not set in %s", key, env)
			}

			if resolveVerboseFlag {
				// Show full provenance.
				fmt.Printf("%s resolved from %s\n", key, rs.StoreName)
				if chain, ok := provenance[key]; ok {
					// Show in reverse (highest priority first).
					for i := len(chain) - 1; i >= 0; i-- {
						src := chain[i]
						label := src.StoreName
						if src.ScopePath != "" {
							label += "/" + src.ScopePath
						}
						if src.Wildcard {
							label += " (*)"
						}
						marker := "  overridden"
						if src.StoreName == rs.StoreName && src.ScopePath == rs.ScopePath {
							marker = "  ← winner"
						}
						val := maskValue(src.Value)
						if resolveShowFlag {
							val = src.Value
						}
						fmt.Printf("  %-35s %s%s\n", label, val, marker)
					}
				}
				return nil
			}

			// Single key, --show: raw value (pipeable).
			if resolveShowFlag {
				fmt.Print(rs.Value)
				return nil
			}

			// Single key, no --show: table row.
			fmt.Printf("%-30s %-20s %s\n", key, maskValue(rs.Value), rs.StoreName)
			return nil
		}

		// All keys mode.
		if len(resolved) == 0 {
			fmt.Printf("No secrets resolved in %s\n", env)
			return nil
		}

		// Header.
		if resolveShowFlag {
			fmt.Printf("%-30s %-40s %s\n", "SECRET", "VALUE", "SOURCE")
			fmt.Printf("%-30s %-40s %s\n", "------", "-----", "------")
		} else {
			fmt.Printf("%-30s %-20s %s\n", "SECRET", "VALUE", "SOURCE")
			fmt.Printf("%-30s %-20s %s\n", "------", "-----", "------")
		}

		for key, rs := range resolved {
			source := rs.StoreName
			if rs.ScopePath != "" {
				source += "/" + rs.ScopePath
			}
			if rs.Wildcard {
				source += " (*)"
			}

			// Check if this overrides something from a lower layer.
			override := ""
			if chain, ok := provenance[key]; ok && len(chain) > 1 {
				override = " (overrides)"
			}

			if resolveShowFlag {
				fmt.Printf("%-30s %-40s %s%s\n", key, rs.Value, source, override)
			} else {
				fmt.Printf("%-30s %-20s %s%s\n", key, maskValue(rs.Value), source, override)
			}
		}

		return nil
	},
}

func maskValue(v string) string {
	if len(v) <= 8 {
		return "****"
	}
	return v[:4] + "..." + v[len(v)-4:]
}

func init() {
	resolveCmd.Flags().BoolVar(&resolveShowFlag, "show", false, "show actual values (masked by default)")
	resolveCmd.Flags().BoolVar(&resolveVerboseFlag, "verbose", false, "show full resolution chain for a key")
	resolveCmd.Flags().StringArrayVar(&resolveSetFlags, "set", nil, "preview with override (KEY=VALUE)")
	rootCmd.AddCommand(resolveCmd)
}
