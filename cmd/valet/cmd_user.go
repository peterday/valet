package main

import (
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
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

		publicKey := userKeyFlag

		if userGitHubFlag != "" && publicKey == "" {
			key, err := fetchGitHubSSHKey(userGitHubFlag)
			if err != nil {
				return fmt.Errorf("fetching GitHub key for %q: %w", userGitHubFlag, err)
			}
			publicKey = key
			fmt.Printf("Fetched SSH key for github.com/%s\n", userGitHubFlag)
		}

		if publicKey == "" {
			return fmt.Errorf("provide --github or --key")
		}

		if _, err := s.AddUser(name, userGitHubFlag, publicKey); err != nil {
			return err
		}

		fmt.Printf("Added user %q\n", name)

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

		for _, u := range users {
			github := ""
			if u.GitHub != "" {
				github = fmt.Sprintf("  (github:%s)", u.GitHub)
			}
			// Show first 20 chars of public key.
			keyPreview := u.PublicKey
			if len(keyPreview) > 24 {
				keyPreview = keyPreview[:24] + "..."
			}
			fmt.Printf("%-15s  %s%s\n", u.Name, keyPreview, github)
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

// fetchGitHubSSHKey fetches the first ed25519 or RSA SSH key for a GitHub user.
func fetchGitHubSSHKey(username string) (string, error) {
	url := fmt.Sprintf("https://github.com/%s.keys", username)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub returned %d for %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	lines := strings.Split(strings.TrimSpace(string(body)), "\n")

	// Prefer ed25519 keys.
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ssh-ed25519 ") {
			return line, nil
		}
	}

	// Fall back to RSA.
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ssh-rsa ") {
			return line, nil
		}
	}

	return "", fmt.Errorf("no SSH keys found for github.com/%s — user needs to add an SSH key to GitHub", username)
}

func init() {
	userAddCmd.Flags().StringVar(&userGitHubFlag, "github", "", "GitHub username (fetches SSH public key)")
	userAddCmd.Flags().StringVar(&userKeyFlag, "key", "", "public key (age or SSH format)")
	userCmd.AddCommand(userAddCmd)
	userCmd.AddCommand(userListCmd)
	userCmd.AddCommand(userRemoveCmd)
	rootCmd.AddCommand(userCmd)
}
