package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/peterday/valet/internal/crypto"
	"github.com/peterday/valet/internal/domain"
)

// Push stages all changes, commits, and pushes the store repo.
// If push fails due to remote changes, it pulls, merges vault conflicts,
// and retries the push.
func (s *Store) Push(message string) error {
	if message == "" {
		message = "valet: update secrets"
	}

	if err := s.gitExec("add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	// Check if there's anything to commit.
	if err := s.gitExec("diff", "--cached", "--quiet"); err == nil {
		// Nothing staged. Try pull + push in case there are remote changes.
		if s.hasRemote() {
			if err := s.Pull(); err != nil {
				return err
			}
		}
		return fmt.Errorf("nothing to commit")
	}

	if err := s.gitExec("commit", "-m", message); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	if !s.hasRemote() {
		return nil
	}

	// Try push. If it fails, pull-merge-push.
	if err := s.gitExecQuiet("push"); err != nil {
		fmt.Println("Remote has changes, pulling and merging...")
		if err := s.pullAndMerge(); err != nil {
			return err
		}
		// Retry push.
		if err := s.gitExec("push"); err != nil {
			return fmt.Errorf("git push (retry): %w", err)
		}
	}

	return nil
}

// Pull fetches and merges changes from the remote.
// If there are vault.age conflicts, it merges them at the secret level.
func (s *Store) Pull() error {
	return s.pullAndMerge()
}

func (s *Store) pullAndMerge() error {
	// Stash any uncommitted changes.
	hadStash := false
	if s.gitExecQuiet("diff", "--quiet") != nil {
		if err := s.gitExec("stash"); err != nil {
			return fmt.Errorf("git stash: %w", err)
		}
		hadStash = true
	}

	// Try a regular pull first.
	err := s.gitExecQuiet("pull", "--rebase")
	if err == nil {
		if hadStash {
			s.gitExec("stash", "pop")
		}
		return nil
	}

	// Rebase failed — likely conflicts. Abort and try merge approach.
	s.gitExecQuiet("rebase", "--abort")

	// Fetch and merge instead.
	if err := s.gitExec("fetch", "origin"); err != nil {
		if hadStash {
			s.gitExec("stash", "pop")
		}
		return fmt.Errorf("git fetch: %w", err)
	}

	// Try merge.
	if mergeErr := s.gitExecQuiet("merge", "origin/"+s.currentBranch()); mergeErr != nil {
		// There are conflicts. Resolve vault.age files.
		if err := s.resolveVaultConflicts(); err != nil {
			s.gitExecQuiet("merge", "--abort")
			if hadStash {
				s.gitExec("stash", "pop")
			}
			return fmt.Errorf("resolving conflicts: %w", err)
		}

		// Stage resolved files and complete the merge.
		s.gitExec("add", "-A")
		if err := s.gitExec("commit", "--no-edit"); err != nil {
			s.gitExecQuiet("merge", "--abort")
			if hadStash {
				s.gitExec("stash", "pop")
			}
			return fmt.Errorf("completing merge: %w", err)
		}
	}

	if hadStash {
		s.gitExec("stash", "pop")
	}
	return nil
}

// resolveVaultConflicts finds conflicted vault.age files and merges them
// at the secret level using latest-timestamp-wins.
func (s *Store) resolveVaultConflicts() error {
	// Find conflicted files.
	out, err := s.gitOutput("diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return err
	}

	for _, file := range strings.Split(strings.TrimSpace(out), "\n") {
		if file == "" {
			continue
		}

		if !strings.HasSuffix(file, "vault.age") {
			// Non-vault conflicts: accept theirs (remote wins for manifests, etc).
			s.gitExec("checkout", "--theirs", file)
			continue
		}

		// For vault.age: decrypt both versions, merge secrets, re-encrypt.
		if err := s.mergeVaultFile(file); err != nil {
			// If merge fails, accept theirs as fallback.
			fmt.Fprintf(os.Stderr, "warning: could not merge %s, accepting remote version\n", file)
			s.gitExec("checkout", "--theirs", file)
		}
	}

	return nil
}

// mergeVaultFile decrypts both sides of a conflicted vault.age,
// merges the secret maps with latest-timestamp-wins, and re-encrypts.
func (s *Store) mergeVaultFile(relPath string) error {
	absPath := filepath.Join(s.Root, relPath)
	identity := s.ageIdentity()

	// Get ours.
	oursData, err := s.gitBytesOutput("show", "HEAD:"+relPath)
	if err != nil {
		return fmt.Errorf("reading ours: %w", err)
	}

	// Get theirs.
	theirsData, err := s.gitBytesOutput("show", "MERGE_HEAD:"+relPath)
	if err != nil {
		return fmt.Errorf("reading theirs: %w", err)
	}

	oursContent, err := crypto.DecryptVault(oursData, identity)
	if err != nil {
		return fmt.Errorf("decrypting ours: %w", err)
	}

	theirsContent, err := crypto.DecryptVault(theirsData, identity)
	if err != nil {
		return fmt.Errorf("decrypting theirs: %w", err)
	}

	// Merge: start with theirs, overlay ours where our timestamp is newer.
	merged := &domain.VaultContent{
		Secrets: make(map[string]domain.VaultSecret),
	}

	// Add all theirs.
	for k, v := range theirsContent.Secrets {
		merged.Secrets[k] = v
	}

	// Overlay ours where timestamp is newer or key doesn't exist in theirs.
	for k, v := range oursContent.Secrets {
		if existing, ok := merged.Secrets[k]; ok {
			if v.UpdatedAt.After(existing.UpdatedAt) {
				merged.Secrets[k] = v
			}
		} else {
			merged.Secrets[k] = v
		}
	}

	// Merge history.
	if oursContent.History != nil || theirsContent.History != nil {
		merged.History = make(map[string][]domain.VaultSecret)
		if theirsContent.History != nil {
			for k, v := range theirsContent.History {
				merged.History[k] = v
			}
		}
		if oursContent.History != nil {
			for k, v := range oursContent.History {
				if _, ok := merged.History[k]; !ok {
					merged.History[k] = v
				}
				// If both have history for the same key, keep the longer one.
				if len(v) > len(merged.History[k]) {
					merged.History[k] = v
				}
			}
		}
	}

	// Read the manifest to get recipients.
	manifestPath := filepath.Join(filepath.Dir(absPath), "manifest.json")
	// Accept theirs for the manifest.
	manifestRel, _ := filepath.Rel(s.Root, manifestPath)
	s.gitExecQuiet("checkout", "--theirs", manifestRel)

	manifest, err := s.readManifestFromPath(manifestPath)
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}

	keys := recipientKeys(manifest.Recipients)
	encData, err := crypto.EncryptVault(merged, keys)
	if err != nil {
		return fmt.Errorf("re-encrypting merged vault: %w", err)
	}

	return os.WriteFile(absPath, encData, 0644)
}

func (s *Store) readManifestFromPath(path string) (*domain.Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m domain.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Clone clones a remote repo to the store root.
func Clone(url, destPath string) error {
	cmd := exec.Command("git", "clone", url, destPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// InitRepo initializes a git repo at the store root.
func (s *Store) InitRepo() error {
	return s.gitExec("init")
}

// SetRemote sets the origin remote URL.
func (s *Store) SetRemote(url string) error {
	if err := s.gitExec("remote", "add", "origin", url); err != nil {
		return s.gitExec("remote", "set-url", "origin", url)
	}
	return nil
}

// IsGitRepo checks if the store root is inside a git repo.
func (s *Store) IsGitRepo() bool {
	return s.gitExecQuiet("rev-parse", "--git-dir") == nil
}

func (s *Store) hasRemote() bool {
	out, err := s.gitOutput("remote")
	return err == nil && strings.TrimSpace(out) != ""
}

func (s *Store) currentBranch() string {
	out, err := s.gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "main"
	}
	return strings.TrimSpace(out)
}

func (s *Store) gitExec(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = s.Root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (s *Store) gitExecQuiet(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = s.Root
	return cmd.Run()
}

func (s *Store) gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = s.Root
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	return buf.String(), err
}

func (s *Store) gitBytesOutput(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = s.Root
	var buf bytes.Buffer
	cmd.Stdout = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}
