package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/identity"
)

var botCmd = &cobra.Command{
	Use:   "bot",
	Short: "Manage bot identities (CI runners, deploy bots, etc.)",
}

var botCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a bot identity and grant it environment access",
	Long: `Generate an age keypair for a bot, add it as a store user, and grant
access to the specified environments.

  valet bot create ci-runner --grant dev
  valet bot create deploy-bot --grant prod
  valet bot create ci --grant dev --grant staging

The printed VALET_KEY should be stored as a secret in your CI/deployment platform.
Any process with VALET_KEY set can decrypt secrets in the granted environments.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		botName := args[0]
		envs, _ := cmd.Flags().GetStringSlice("grant")

		s, err := openStore()
		if err != nil {
			return err
		}
		project, err := resolveProject(s)
		if err != nil {
			return err
		}

		// Generate a keypair for the bot.
		botID, err := identity.GenerateKeypair()
		if err != nil {
			return err
		}

		// Add as user.
		if _, err := s.AddUser(botName, "", botID.PublicKey); err != nil {
			return err
		}

		// Grant environment access.
		totalScopes := 0
		for _, env := range envs {
			count, err := s.GrantEnvironment(project, env, botName)
			if err != nil {
				return fmt.Errorf("granting %s access to %s: %w", botName, env, err)
			}
			totalScopes += count
		}

		// Print results.
		fmt.Printf("Created bot %q\n", botName)
		if len(envs) > 0 {
			fmt.Printf("Granted access to %d scope(s) across environments: %v\n", totalScopes, envs)
		}
		fmt.Println()
		fmt.Println("Set this env var wherever the bot runs:")
		fmt.Printf("  VALET_KEY=%s\n", botID.PrivateKey)
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Printf("  GitHub Actions:  gh secret set VALET_KEY --body '%s'\n", botID.PrivateKey)
		fmt.Printf("  Vercel:          vercel env add VALET_KEY\n")
		fmt.Printf("  Docker:          docker run --env VALET_KEY=%s my-image\n", botID.PrivateKey)
		fmt.Println()
		fmt.Println("Don't forget to push: valet push")

		return nil
	},
}

var botListCmd = &cobra.Command{
	Use:   "list",
	Short: "List bot identities",
	Long:  `Lists all users in the store. Bots and humans are both users — this is a convenience alias for "valet user list".`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}

		users, err := s.ListUsers()
		if err != nil {
			return err
		}

		if len(users) == 0 {
			fmt.Println("No users or bots.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPUBLIC KEY\tCREATED")
		for _, u := range users {
			pubKeyShort := u.PublicKey
			if len(pubKeyShort) > 20 {
				pubKeyShort = pubKeyShort[:20] + "..."
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", u.Name, pubKeyShort, u.CreatedAt.Format("2006-01-02"))
		}
		w.Flush()
		return nil
	},
}

var botRevokeCmd = &cobra.Command{
	Use:   "revoke <name>",
	Short: "Revoke a bot from all environments and remove it",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		botName := args[0]
		rotate, _ := cmd.Flags().GetBool("rotate")

		s, err := openStore()
		if err != nil {
			return err
		}
		project, err := resolveProject(s)
		if err != nil {
			return err
		}

		// Revoke from all environments.
		envs, err := s.ListEnvironments(project)
		if err != nil {
			return err
		}

		totalScopes := 0
		totalSecrets := 0
		for _, env := range envs {
			if rotate {
				sc, sec, err := s.RevokeEnvironmentWithRotation(project, env, botName)
				if err != nil {
					continue // may not be a recipient in this env
				}
				totalScopes += sc
				totalSecrets += sec
			} else {
				count, err := s.RevokeEnvironment(project, env, botName)
				if err != nil {
					continue
				}
				totalScopes += count
			}
		}

		// Remove user.
		if err := s.RemoveUser(botName); err != nil {
			return err
		}

		fmt.Printf("Revoked bot %q from %d scope(s)\n", botName, totalScopes)
		if rotate && totalSecrets > 0 {
			fmt.Printf("%d secret(s) flagged for rotation\n", totalSecrets)
		}
		fmt.Println("Removed user. Don't forget to push: valet push")

		return nil
	},
}

func init() {
	botCreateCmd.Flags().StringSlice("grant", nil, "environments to grant access to (e.g. --grant dev --grant staging)")
	botRevokeCmd.Flags().Bool("rotate", false, "flag affected secrets for rotation")
	botCmd.AddCommand(botCreateCmd)
	botCmd.AddCommand(botListCmd)
	botCmd.AddCommand(botRevokeCmd)
	rootCmd.AddCommand(botCmd)
}
