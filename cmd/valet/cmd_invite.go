package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var (
	inviteEnvFlag     []string
	inviteExpiresFlag string
	inviteMaxUsesFlag int
)

var inviteCmd = &cobra.Command{
	Use:   "invite",
	Short: "Manage invitations",
}

var inviteCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an invite for a new teammate",
	Long: `Generate a temporary key that lets someone join and get access.
Share the key securely (DM, not a public channel).

  valet invite create -e dev
  valet invite create -e dev -e staging --expires 3d
  valet invite create -e dev --max-uses 1`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(inviteEnvFlag) == 0 {
			return fmt.Errorf("-e (environment) is required")
		}

		s, err := openStore()
		if err != nil {
			return err
		}
		project, err := resolveProject(s)
		if err != nil {
			return err
		}

		expiry, err := parseDuration(inviteExpiresFlag)
		if err != nil {
			return fmt.Errorf("invalid --expires: %w", err)
		}

		maxUses := inviteMaxUsesFlag
		if maxUses == 0 {
			maxUses = 1
		}

		invite, tempPrivKey, err := s.CreateInvite(project, inviteEnvFlag, expiry, maxUses)
		if err != nil {
			return err
		}

		fmt.Println("Invite created.")
		fmt.Printf("Environments: %v\n", invite.Environments)
		fmt.Printf("Expires:      %s\n", invite.ExpiresAt.Format("2006-01-02 15:04"))
		fmt.Printf("Max uses:     %d\n", invite.MaxUses)
		fmt.Println()
		fmt.Println("Share this key with your teammate (keep it secret):")
		fmt.Println()
		fmt.Printf("  %s\n", tempPrivKey)
		fmt.Println()
		fmt.Println("They run:")
		fmt.Printf("  valet join --invite %s\n", tempPrivKey)

		return nil
	},
}

var inviteListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pending invites",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}

		invites, err := s.ListInvites()
		if err != nil {
			return err
		}

		if len(invites) == 0 {
			fmt.Println("No pending invites.")
			return nil
		}

		fmt.Printf("%-10s  %-20s  %-8s  %-12s  %s\n", "ID", "ENVIRONMENTS", "USES", "EXPIRES", "STATUS")
		fmt.Printf("%-10s  %-20s  %-8s  %-12s  %s\n", "--", "------------", "----", "-------", "------")
		now := time.Now()
		for _, inv := range invites {
			status := "active"
			if now.After(inv.ExpiresAt) {
				status = "expired"
			}
			if inv.MaxUses > 0 && inv.Uses >= inv.MaxUses {
				status = "used"
			}
			fmt.Printf("%-10s  %-20v  %d/%-5d  %-12s  %s\n",
				inv.ID,
				inv.Environments,
				inv.Uses, inv.MaxUses,
				inv.ExpiresAt.Format("2006-01-02"),
				status,
			)
		}
		return nil
	},
}

var invitePruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove expired invites and their temp keys",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		project, err := resolveProject(s)
		if err != nil {
			return err
		}

		pruned, err := s.PruneExpiredInvites(project)
		if err != nil {
			return err
		}

		if pruned == 0 {
			fmt.Println("No expired invites to prune.")
		} else {
			fmt.Printf("Pruned %d expired invite(s).\n", pruned)
		}
		return nil
	},
}

// parseDuration parses human-friendly durations like "7d", "24h", "30m".
func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 7 * 24 * time.Hour, nil // default 7 days
	}

	n := len(s)
	if n < 2 {
		return 0, fmt.Errorf("invalid duration %q", s)
	}

	suffix := s[n-1]
	numStr := s[:n-1]

	var num int
	if _, err := fmt.Sscanf(numStr, "%d", &num); err != nil {
		return 0, fmt.Errorf("invalid duration %q", s)
	}

	switch suffix {
	case 'm':
		return time.Duration(num) * time.Minute, nil
	case 'h':
		return time.Duration(num) * time.Hour, nil
	case 'd':
		return time.Duration(num) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown duration suffix %q (use m, h, or d)", string(suffix))
	}
}

func init() {
	inviteCreateCmd.Flags().StringSliceVarP(&inviteEnvFlag, "env", "e", nil, "environments to grant (required, repeatable)")
	inviteCreateCmd.Flags().StringVar(&inviteExpiresFlag, "expires", "7d", "expiry duration (e.g. 7d, 24h)")
	inviteCreateCmd.Flags().IntVar(&inviteMaxUsesFlag, "max-uses", 1, "maximum number of times this invite can be used")
	inviteCmd.AddCommand(inviteCreateCmd)
	inviteCmd.AddCommand(inviteListCmd)
	inviteCmd.AddCommand(invitePruneCmd)
	rootCmd.AddCommand(inviteCmd)
}
