package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/store"
)

var (
	joinInviteFlag string
	joinAsFlag     string
)

var joinCmd = &cobra.Command{
	Use:   "join [git-url]",
	Short: "Join a store",
	Long: `Join a shared store, or use an invite to get immediate access.

  # Join a standalone store (clone from remote)
  valet join github:acme/api-secrets

  # Join with a custom local name
  valet join github:pday/shared-keys --as team-keys

  # Join an embedded store with an invite (from inside the cloned repo)
  valet join --invite AGE-SECRET-KEY-1XYZ...

  # Join a standalone store with an invite (clone + auto-access)
  valet join github:acme/api-secrets --invite AGE-SECRET-KEY-1XYZ...`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if joinInviteFlag != "" {
			if len(args) > 0 {
				if err := joinRemoteStore(args[0]); err != nil {
					if !strings.Contains(err.Error(), "already exists") {
						return err
					}
				}
			}
			return joinWithInvite(joinInviteFlag)
		}

		if len(args) == 0 {
			return fmt.Errorf("provide a git URL or --invite key")
		}

		return joinRemoteStore(args[0])
	},
}

func joinWithInvite(tempPrivKey string) error {
	id, err := loadIdentityOrInit()
	if err != nil {
		return err
	}

	s, err := openStore()
	if err != nil {
		return fmt.Errorf("no store found — clone the repo first, or use -s <store-name>")
	}

	userName := deriveUserName()
	fmt.Printf("Joining as %q...\n", userName)

	if err := s.ConsumeInvite(tempPrivKey, userName, id); err != nil {
		return err
	}

	fmt.Println("You're in!")
	showAccessibleSecrets(s, id.PublicKey)

	return nil
}

func joinRemoteStore(url string) error {
	id, err := loadIdentityOrInit()
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Use --as name if provided, otherwise derive from URL.
	name := joinAsFlag
	if name == "" {
		name = storeNameFromURL(url)
	}
	storePath := filepath.Join(cfg.StoresDir, name)

	if _, err := os.Stat(storePath); err == nil {
		return fmt.Errorf("store %q already exists at %s", name, storePath)
	}

	if err := os.MkdirAll(cfg.StoresDir, 0755); err != nil {
		return err
	}

	remote := url
	if strings.HasPrefix(url, "github:") {
		remote = fmt.Sprintf("git@github.com:%s.git", strings.TrimPrefix(url, "github:"))
	}

	fmt.Printf("Cloning %s...\n", remote)
	if err := store.Clone(remote, storePath); err != nil {
		// Clone failed — maybe pending GitHub invite. Try to accept.
		if strings.Contains(remote, "github.com") {
			if tryAcceptGitHubInvite(remote) {
				// Retry clone after accepting.
				fmt.Println("Retrying clone...")
				if err := store.Clone(remote, storePath); err != nil {
					return fmt.Errorf("clone failed after accepting invite: %w", err)
				}
			} else {
				return fmt.Errorf("clone failed: %w\n\nIf you were invited, check your GitHub notifications or run:\n  gh api user/repository_invitations", err)
			}
		} else {
			return fmt.Errorf("clone failed: %w", err)
		}
	}

	s, err := store.Open(storePath, id)
	if err != nil {
		return fmt.Errorf("opening cloned store: %w", err)
	}

	// Check if we're already a user.
	users, _ := s.ListUsers()
	alreadyUser := false
	for _, u := range users {
		if u.PublicKey == id.PublicKey {
			alreadyUser = true
			fmt.Printf("You're already registered as %q\n", u.Name)
			break
		}
	}

	if !alreadyUser {
		userName := deriveUserName()
		if _, err := s.AddUser(userName, "", id.PublicKey); err != nil {
			return fmt.Errorf("adding self: %w", err)
		}

		if err := s.Push(fmt.Sprintf("valet: %s joined", userName)); err != nil {
			fmt.Printf("Added self as %q (push manually with 'valet push')\n", userName)
		} else {
			fmt.Printf("Joined as %q\n", userName)
		}
	}

	showAccessibleSecrets(s, id.PublicKey)

	return nil
}

// showAccessibleSecrets prints what the user can decrypt after joining.
func showAccessibleSecrets(s *store.Store, publicKey string) {
	project, err := resolveProjectForStore(s)
	if err != nil {
		return
	}

	scopes, err := s.ListAllScopes(project)
	if err != nil || len(scopes) == 0 {
		fmt.Println("\nNo scopes found. Ask an admin to grant you access:")
		fmt.Println("  valet env grant <your-name> -e dev")
		return
	}

	// Group secrets by env, only for scopes where this user is a recipient.
	type envSecrets struct {
		scopes  int
		secrets []string
	}
	envMap := make(map[string]*envSecrets)

	for _, sc := range scopes {
		isRecipient := false
		for _, r := range sc.Recipients {
			if r.PublicKey == publicKey {
				isRecipient = true
				break
			}
		}
		if !isRecipient {
			continue
		}

		env := strings.SplitN(sc.Path, "/", 2)[0]
		if envMap[env] == nil {
			envMap[env] = &envSecrets{}
		}
		envMap[env].scopes++
		envMap[env].secrets = append(envMap[env].secrets, sc.Secrets...)
	}

	if len(envMap) == 0 {
		fmt.Println("\nYou're registered but don't have access to any secrets yet.")
		fmt.Println("Ask an admin to grant you access:")
		fmt.Println("  valet env grant <your-name> -e dev")
		return
	}

	fmt.Println("\nSecrets you can access:")
	for env, es := range envMap {
		fmt.Printf("  %s: (%d scope(s), %d secret(s))\n", env, es.scopes, len(es.secrets))
		for _, name := range es.secrets {
			fmt.Printf("    %s\n", name)
		}
	}

	fmt.Println("\nLink to a project:")
	fmt.Printf("  cd ~/code/my-project && valet link %s\n", s.Meta.Name)
}

// tryAcceptGitHubInvite checks for pending GitHub repo invitations and
// accepts one matching the given remote URL. Returns true if accepted.
func tryAcceptGitHubInvite(remote string) bool {
	if _, err := exec.LookPath("gh"); err != nil {
		return false
	}

	// Extract org/repo from the remote URL.
	repo := remote
	repo = strings.TrimPrefix(repo, "git@github.com:")
	repo = strings.TrimPrefix(repo, "https://github.com/")
	repo = strings.TrimSuffix(repo, ".git")
	if repo == "" {
		return false
	}

	// List pending invitations.
	out, err := exec.Command("gh", "api", "user/repository_invitations").Output()
	if err != nil {
		return false
	}

	var invitations []struct {
		ID         int `json:"id"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(out, &invitations); err != nil {
		return false
	}

	for _, inv := range invitations {
		if strings.EqualFold(inv.Repository.FullName, repo) {
			fmt.Printf("Found pending invite for %s. Accepting...\n", repo)

			reader := bufio.NewReader(os.Stdin)
			fmt.Print("Accept? [Y/n]: ")
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "" && answer != "y" && answer != "yes" {
				return false
			}

			cmd := exec.Command("gh", "api", fmt.Sprintf("user/repository_invitations/%d", inv.ID), "-X", "PATCH")
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to accept invite: %v\n", err)
				return false
			}
			fmt.Println("Accepted!")
			return true
		}
	}

	return false
}

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage projects",
}

var projectCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new project in the store",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		if _, err := s.CreateProject(args[0]); err != nil {
			return err
		}
		fmt.Printf("Created project %q\n", args[0])
		return nil
	},
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List projects in the store",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		projects, err := s.ListProjects()
		if err != nil {
			return err
		}
		if len(projects) == 0 {
			fmt.Println("No projects.")
			return nil
		}
		for _, p := range projects {
			fmt.Printf("%-20s  %s\n", p.Slug, p.CreatedAt.Format("2006-01-02"))
		}
		return nil
	},
}

func storeNameFromURL(url string) string {
	name := url
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ".git")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return name
}

func deriveUserName() string {
	if name, err := gitConfigValue("user.name"); err == nil && name != "" {
		name = strings.ToLower(strings.ReplaceAll(name, " ", "-"))
		return name
	}
	if user := os.Getenv("USER"); user != "" {
		return user
	}
	return "user"
}

func gitConfigValue(key string) (string, error) {
	out, err := exec.Command("git", "config", key).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func init() {
	joinCmd.Flags().StringVar(&joinInviteFlag, "invite", "", "invite key (AGE-SECRET-KEY-...)")
	joinCmd.Flags().StringVar(&joinAsFlag, "as", "", "custom local name for the store")
	rootCmd.AddCommand(joinCmd)
	projectCmd.AddCommand(projectCreateCmd)
	projectCmd.AddCommand(projectListCmd)
	rootCmd.AddCommand(projectCmd)
}
