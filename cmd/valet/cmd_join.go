package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/store"
)

var joinInviteFlag string

var joinCmd = &cobra.Command{
	Use:   "join [git-url]",
	Short: "Join a store",
	Long: `Join a shared store, or use an invite to get immediate access.

  # Join a standalone store (clone from remote)
  valet join github:acme/api-secrets

  # Join an embedded store with an invite (from inside the cloned repo)
  valet join --invite AGE-SECRET-KEY-1XYZ...

  # Join a standalone store with an invite (clone + auto-access)
  valet join github:acme/api-secrets --invite AGE-SECRET-KEY-1XYZ...

  # Join a named store with an invite (already cloned)
  valet join -s acme-secrets --invite AGE-SECRET-KEY-1XYZ...`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if joinInviteFlag != "" {
			// If a URL is also provided, clone first then use invite.
			if len(args) > 0 {
				if err := joinRemoteStore(args[0]); err != nil {
					// Ignore "already exists" — just means it's already cloned.
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

	// openStore respects --store flag, so this works for both
	// embedded stores (current dir) and named stores (-s flag).
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

	// Show what environments were granted.
	project, _ := resolveProject(s)
	scopes, _ := s.ListAllScopes(project)
	envSet := make(map[string]bool)
	for _, sc := range scopes {
		for _, r := range sc.Recipients {
			if r.PublicKey == id.PublicKey {
				env := strings.SplitN(sc.Path, "/", 2)[0]
				envSet[env] = true
			}
		}
	}
	if len(envSet) > 0 {
		envs := make([]string, 0, len(envSet))
		for e := range envSet {
			envs = append(envs, e)
		}
		fmt.Printf("Access granted: %s\n", strings.Join(envs, ", "))
	}

	fmt.Println("\nRun your app:")
	fmt.Println("  valet drive -- <command>")

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

	name := storeNameFromURL(url)
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
		return fmt.Errorf("clone failed: %w", err)
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

	projects, _ := s.ListProjects()
	if len(projects) > 0 {
		fmt.Println("\nProjects in this store:")
		for _, p := range projects {
			fmt.Printf("  %s\n", p.Slug)
		}
	}

	fmt.Println("\nAsk an admin to grant you environment access:")
	fmt.Println("  valet env grant <your-name> -e dev")

	return nil
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
	rootCmd.AddCommand(joinCmd)
	projectCmd.AddCommand(projectCreateCmd)
	projectCmd.AddCommand(projectListCmd)
	rootCmd.AddCommand(projectCmd)
}
