package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/store"
)

var migrateYesFlag bool

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate .valet.toml to use .env.example as the source of truth",
	Long: `If your project has both .env.example and .valet.toml requirements,
this command analyzes them and removes the redundant entries — keeping
.valet.toml as a list of overrides.

After migration:
  • .env.example  →  the requirements list (universal, edited normally)
  • .valet.toml   →  overrides only (custom providers, optional flags, etc.)

Adding a new env var to .env.example will be picked up automatically without
having to update .valet.toml.

  valet migrate          # interactive preview + confirm
  valet migrate --yes    # skip confirmation`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		tomlPath, err := config.FindValetToml(cwd)
		if err != nil {
			return fmt.Errorf("no .valet.toml found")
		}

		vc, err := config.LoadValetToml(tomlPath)
		if err != nil {
			return err
		}

		plan := store.PlanMigration(cwd, vc)

		if !plan.HasEnvExample {
			fmt.Println("No .env.example found in this project.")
			fmt.Println("Migration is only useful when you have a .env.example to use as the source of truth.")
			return nil
		}

		printMigrationPlan(plan)

		if len(plan.Redundant) == 0 {
			fmt.Println("\nNothing to migrate — .valet.toml has no redundant entries.")
			return nil
		}

		if !migrateYesFlag {
			fmt.Print("\nApply migration? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			ans, _ := reader.ReadString('\n')
			ans = strings.ToLower(strings.TrimSpace(ans))
			if ans != "y" && ans != "yes" {
				fmt.Println("Cancelled.")
				return nil
			}
		}

		updated := plan.Apply(vc)
		if err := config.WriteValetToml(tomlPath, updated); err != nil {
			return fmt.Errorf("writing .valet.toml: %w", err)
		}

		fmt.Printf("\nMigrated! Removed %d redundant entr(ies) from .valet.toml.\n", len(plan.Redundant))
		fmt.Println(".env.example is now the source of truth for requirements.")
		if len(plan.Overrides) > 0 {
			fmt.Printf("Kept %d override(s) in .valet.toml.\n", len(plan.Overrides))
		}
		return nil
	},
}

func printMigrationPlan(plan *store.MigratePlan) {
	fmt.Printf("Found %s\n\n", plan.ExamplePath)

	if len(plan.Redundant) > 0 {
		fmt.Printf("REDUNDANT in .valet.toml — auto-detected from .env.example (%d)\n", len(plan.Redundant))
		for _, e := range plan.Redundant {
			fmt.Printf("  ✓ %-32s will be removed\n", e.Key)
		}
	}

	if len(plan.Overrides) > 0 {
		fmt.Printf("\nOVERRIDES in .valet.toml — different from auto-detection (%d)\n", len(plan.Overrides))
		for _, e := range plan.Overrides {
			fmt.Printf("  ⚠ %-32s kept (%s)\n", e.Key, e.Reason)
		}
	}

	if len(plan.MissingFromExample) > 0 {
		fmt.Printf("\nNOT IN .env.example — kept in .valet.toml (%d)\n", len(plan.MissingFromExample))
		for _, e := range plan.MissingFromExample {
			fmt.Printf("  ! %-32s only in .valet.toml\n", e.Key)
		}
	}

	if len(plan.NewlyDetected) > 0 {
		fmt.Printf("\nNEWLY DETECTED — auto-tracked from .env.example after migration (%d)\n", len(plan.NewlyDetected))
		for _, key := range plan.NewlyDetected {
			fmt.Printf("  + %s\n", key)
		}
	}
}

func init() {
	migrateCmd.Flags().BoolVarP(&migrateYesFlag, "yes", "y", false, "skip confirmation")
	rootCmd.AddCommand(migrateCmd)
}
