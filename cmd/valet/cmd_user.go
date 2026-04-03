package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/store"
)

var userGitHubFlag string
var userKeyFlag string

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage store users",
}

var userAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a user to the store",
	Long: `Add a user by providing their public key directly or fetching it from GitHub.

  valet user add bob --github bob-smith
  valet user add alice --key age1...`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		s, err := openStore()
		if err != nil {
			return err
		}

		var keys []domain.UserKey

		if userGitHubFlag != "" && userKeyFlag == "" {
			ghKeys, err := store.FetchGitHubKeys(userGitHubFlag)
			if err != nil {
				return fmt.Errorf("fetching GitHub keys for %q: %w", userGitHubFlag, err)
			}
			keys = ghKeys
			for _, k := range keys {
				label := k.Label
				if label == "" {
					label = "(unlabeled)"
				}
				fmt.Printf("  %s  %s\n", truncateKey(k.Key), label)
			}
			fmt.Printf("Fetched %d SSH key(s) for github.com/%s\n", len(keys), userGitHubFlag)
		} else if userKeyFlag != "" {
			keys = []domain.UserKey{{Key: userKeyFlag}}
		} else {
			return fmt.Errorf("provide --github or --key")
		}

		if _, err := s.AddUserWithKeys(name, userGitHubFlag, keys); err != nil {
			return err
		}

		fmt.Printf("Added user %q (%d key(s))\n", name, len(keys))

		// If the store has a GitHub remote and we know the user's GitHub handle,
		// invite them as a collaborator so they can clone the repo.
		if userGitHubFlag != "" && s.Meta.Remote != "" {
			addGitHubCollaborator(s.Meta.Remote, userGitHubFlag)
			fmt.Printf("\nTell %s to run:\n", name)
			fmt.Printf("  valet join %s\n", remoteToShorthand(s.Meta.Remote))
		}

		return nil
	},
}

var userListCmd = &cobra.Command{
	Use:   "list",
	Short: "List users in the store",
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
			fmt.Println("No users.")
			return nil
		}

		fmt.Printf("%-15s  %-28s  %s\n", "NAME", "KEY", "GITHUB")
		fmt.Printf("%-15s  %-28s  %s\n", "----", "---", "------")
		for _, u := range users {
			github := ""
			if u.GitHub != "" {
				github = u.GitHub
			}
			keyPreview := u.PublicKey
			if len(keyPreview) > 24 {
				keyPreview = keyPreview[:24] + "..."
			}
			fmt.Printf("%-15s  %-28s  %s\n", u.Name, keyPreview, github)
		}
		return nil
	},
}

var userRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a user from the store",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		if err := s.RemoveUser(args[0]); err != nil {
			return err
		}
		fmt.Printf("Removed user %q\n", args[0])
		fmt.Println("Note: user's access to existing scopes is not revoked. Run 'valet env revoke' to re-encrypt.")
		return nil
	},
}

var userRefreshCmd = &cobra.Command{
	Use:   "refresh <name>",
	Short: "Refresh a user's public key from GitHub",
	Long: `Re-fetches the user's SSH key from GitHub and re-encrypts all vaults
where they are a recipient. The user must have a GitHub handle stored.

  valet user refresh alice`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		s, err := openStore()
		if err != nil {
			return err
		}

		user, err := s.GetUser(name)
		if err != nil {
			return err
		}

		if user.GitHub == "" {
			return fmt.Errorf("user %q has no GitHub handle stored — use 'valet user add' with --github", name)
		}

		fmt.Printf("Syncing keys for %s (@%s)...\n", name, user.GitHub)

		ghKeys, err := store.FetchGitHubKeys(user.GitHub)
		if err != nil {
			return fmt.Errorf("fetching keys for @%s: %w", user.GitHub, err)
		}

		// Separate existing keys: GitHub-sourced SSH keys vs everything else (age, manual).
		var oldGHKeys []domain.UserKey
		var otherKeys []domain.UserKey
		for _, k := range user.AllUserKeys() {
			if k.Source == "github" || k.Source == "ssh" || (k.Source == "" && isSSHKey(k.Key)) {
				oldGHKeys = append(oldGHKeys, k)
			} else {
				if strings.Contains(k.Label, "removed from GitHub") {
					k.Label = ""
				}
				otherKeys = append(otherKeys, k)
			}
		}

		oldGHSet := make(map[string]bool)
		for _, k := range oldGHKeys {
			oldGHSet[k.Key] = true
		}
		newGHSet := make(map[string]bool)
		for _, k := range ghKeys {
			newGHSet[k.Key] = true
		}

		// Show non-GitHub keys — not touched by refresh.
		for _, k := range otherKeys {
			source := k.Source
			if source == "" {
				source = "local"
			}
			fmt.Printf("  ✓ %s  (%s — not synced)\n", truncateKey(k.Key), source)
		}

		// Show GitHub key diff.
		for _, k := range ghKeys {
			label := ""
			if k.Label != "" {
				label = "  (" + k.Label + ")"
			}
			if oldGHSet[k.Key] {
				fmt.Printf("  ✓ %s%s  (unchanged)\n", truncateKey(k.Key), label)
			} else {
				fmt.Printf("  + %s%s  (new)\n", truncateKey(k.Key), label)
			}
		}
		var removedGHKeys []domain.UserKey
		for _, k := range oldGHKeys {
			if !newGHSet[k.Key] {
				label := ""
				if k.Label != "" {
					label = "  (" + k.Label + ")"
				}
				fmt.Printf("  ! %s%s  (removed from GitHub)\n", truncateKey(k.Key), label)
				removedGHKeys = append(removedGHKeys, k)
			}
		}

		// Build full key set: non-GitHub keys + GitHub keys + preserved removed GitHub keys.
		var syncKeys []domain.UserKey
		syncKeys = append(syncKeys, otherKeys...)
		syncKeys = append(syncKeys, ghKeys...)
		for _, k := range removedGHKeys {
			k.Label = k.Label + " (removed from GitHub)"
			syncKeys = append(syncKeys, k)
		}

		added, _, scopes, err := s.SyncUserKeys(name, syncKeys)
		if err != nil {
			return err
		}

		if added == 0 {
			fmt.Printf("Keys already up to date.\n")
		} else {
			fmt.Printf("Added %d key(s), updated %d scope(s).\n", added, scopes)
		}

		if len(removedGHKeys) > 0 {
			fmt.Printf("\n⚠ %d SSH key(s) no longer on GitHub. To revoke:\n", len(removedGHKeys))
			for _, k := range removedGHKeys {
				fmt.Printf("  valet user revoke-key %s %s\n", name, truncateKey(k.Key))
			}
		}

		return nil
	},
}

// remoteToShorthand converts a git URL back to github: shorthand.
func remoteToShorthand(remote string) string {
	r := remote
	r = strings.TrimPrefix(r, "git@github.com:")
	r = strings.TrimPrefix(r, "https://github.com/")
	r = strings.TrimSuffix(r, ".git")
	if r != remote {
		return "github:" + r
	}
	return remote
}

// addGitHubCollaborator invites a GitHub user as a collaborator on the store repo.
// Best-effort — prints a hint if gh is not available or the invite fails.
func addGitHubCollaborator(remote, githubUsername string) {
	repo := remote
	repo = strings.TrimPrefix(repo, "git@github.com:")
	repo = strings.TrimPrefix(repo, "https://github.com/")
	repo = strings.TrimSuffix(repo, ".git")

	if repo == "" || !strings.Contains(repo, "/") {
		return
	}

	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Printf("Tip: grant %s access to the repo:\n", githubUsername)
		fmt.Printf("  gh api repos/%s/collaborators/%s -X PUT\n", repo, githubUsername)
		return
	}

	cmd := exec.Command("gh", "api", fmt.Sprintf("repos/%s/collaborators/%s", repo, githubUsername), "-X", "PUT")
	if err := cmd.Run(); err != nil {
		fmt.Printf("Tip: grant %s access to the repo:\n", githubUsername)
		fmt.Printf("  gh api repos/%s/collaborators/%s -X PUT\n", repo, githubUsername)
		return
	}

	fmt.Printf("Invited %s as collaborator on %s\n", githubUsername, repo)
}

var userUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update a user's metadata",
	Long: `Update a user's GitHub handle or other metadata.

  valet user update me --github peterday`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		s, err := openStore()
		if err != nil {
			return err
		}

		updates := make(map[string]string)
		if cmd.Flags().Changed("github") {
			updates["github"] = userGitHubFlag
		}
		if len(updates) == 0 {
			return fmt.Errorf("nothing to update — use --github")
		}

		if err := s.UpdateUser(name, updates); err != nil {
			return err
		}

		fmt.Printf("Updated user %q\n", name)
		return nil
	},
}

var userAddKeyCmd = &cobra.Command{
	Use:   "add-key <name> --key <public-key>",
	Short: "Add an additional key to an existing user",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if userKeyFlag == "" {
			return fmt.Errorf("provide --key")
		}

		s, err := openStore()
		if err != nil {
			return err
		}

		source := "manual"
		if strings.HasPrefix(userKeyFlag, "age1") {
			source = "age-identity"
		}
		if err := s.AddUserKey(name, userKeyFlag, "", source); err != nil {
			return err
		}

		fmt.Printf("Added key to user %q\n", name)
		return nil
	},
}

var userRevokeKeyCmd = &cobra.Command{
	Use:   "revoke-key <name> <key-prefix>",
	Short: "Remove a specific key from a user and re-encrypt vaults",
	Long: `Removes a specific SSH or age key from a user and re-encrypts all vaults
where that key was a recipient. The key can no longer decrypt new vault versions.

Provide enough of the key to uniquely identify it (e.g. the type + first few chars).`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		keyPrefix := args[1]

		s, err := openStore()
		if err != nil {
			return err
		}

		user, err := s.GetUser(name)
		if err != nil {
			return err
		}

		// Find the full key matching the prefix.
		var matchedKey string
		for _, k := range user.AllKeys() {
			if strings.HasPrefix(k, keyPrefix) || strings.Contains(k, keyPrefix) {
				if matchedKey != "" {
					return fmt.Errorf("prefix %q matches multiple keys — be more specific", keyPrefix)
				}
				matchedKey = k
			}
		}
		if matchedKey == "" {
			return fmt.Errorf("no key matching %q found on user %q", keyPrefix, name)
		}

		fmt.Printf("Revoking key %s from %s...\n", truncateKey(matchedKey), name)

		count, err := s.RemoveUserKey(name, matchedKey)
		if err != nil {
			return err
		}

		fmt.Printf("Revoked key — re-encrypted %d scope(s).\n", count)
		return nil
	},
}

// isSSHKey returns true if the key looks like an SSH public key.
func isSSHKey(key string) bool {
	return strings.HasPrefix(key, "ssh-") || strings.HasPrefix(key, "ecdsa-")
}

// truncateKey shows the key type + first few chars for display.
func truncateKey(key string) string {
	parts := strings.SplitN(key, " ", 3)
	if len(parts) >= 2 {
		hash := parts[1]
		if len(hash) > 12 {
			hash = hash[:12] + "..."
		}
		return parts[0] + " " + hash
	}
	if len(key) > 30 {
		return key[:30] + "..."
	}
	return key
}

func init() {
	userAddCmd.Flags().StringVar(&userGitHubFlag, "github", "", "GitHub username (fetches SSH public keys)")
	userAddCmd.Flags().StringVar(&userKeyFlag, "key", "", "public key (age or SSH format)")
	userAddKeyCmd.Flags().StringVar(&userKeyFlag, "key", "", "public key to add")
	userUpdateCmd.Flags().StringVar(&userGitHubFlag, "github", "", "set GitHub username")
	userCmd.AddCommand(userAddCmd)
	userCmd.AddCommand(userAddKeyCmd)
	userCmd.AddCommand(userUpdateCmd)
	userCmd.AddCommand(userListCmd)
	userCmd.AddCommand(userRemoveCmd)
	userCmd.AddCommand(userRefreshCmd)
	userCmd.AddCommand(userRevokeKeyCmd)
	rootCmd.AddCommand(userCmd)
}
