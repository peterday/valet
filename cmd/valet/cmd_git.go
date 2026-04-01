package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push store changes to remote",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}

		// Auto-prune expired invites before pushing.
		project, _ := resolveProject(s)
		if project != "" {
			if pruned, err := s.PruneExpiredInvites(project); err == nil && pruned > 0 {
				fmt.Printf("Pruned %d expired invite(s).\n", pruned)
			}
		}

		if err := s.Push(""); err != nil {
			return err
		}
		fmt.Println("Pushed.")
		return nil
	},
}

var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull store changes from remote",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		if err := s.Pull(); err != nil {
			return err
		}
		fmt.Println("Pulled.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(pushCmd)
	rootCmd.AddCommand(pullCmd)
}
