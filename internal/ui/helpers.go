package ui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/store"
)

// readManifestFile reads a manifest.json directly from disk.
func readManifestFile(storeRoot, project, scopePath string) *domain.Manifest {
	path := filepath.Join(storeRoot, "projects", project, scopePath, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m domain.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return &m
}

// urlEncodeStoreName encodes a store name for use in URLs.
// Replaces / with __SLASH__ since Go's router splits on /.
func urlEncodeStoreName(name string) string {
	return strings.ReplaceAll(name, "/", "__SLASH__")
}

// urlDecodeStoreName reverses urlEncodeStoreName.
func urlDecodeStoreName(encoded string) string {
	return strings.ReplaceAll(encoded, "__SLASH__", "/")
}

// storeHasUnpushed checks if a git-backed store has uncommitted or unpushed changes.
func storeHasUnpushed(st *store.Store) bool {
	if !st.IsGitRepo() {
		return false
	}
	// Check for uncommitted changes.
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = st.Root
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return true
	}
	// Check for unpushed commits.
	cmd = exec.Command("git", "log", "--oneline", "@{upstream}..HEAD")
	cmd.Dir = st.Root
	out, err = cmd.Output()
	if err != nil {
		// No upstream set — if there are commits, consider unpushed.
		cmd2 := exec.Command("git", "log", "--oneline", "-1")
		cmd2.Dir = st.Root
		out2, _ := cmd2.Output()
		return len(strings.TrimSpace(string(out2))) > 0
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// storeRemote returns the git remote URL for a store, checking Meta.Remote
// first then falling back to `git remote get-url origin`.
func storeRemote(st *store.Store) string {
	if st.Meta.Remote != "" {
		return st.Meta.Remote
	}
	if !st.IsGitRepo() {
		return ""
	}
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = st.Root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// removeAll removes a directory and its contents.
func removeAll(path string) error {
	return os.RemoveAll(path)
}

// envFromScope extracts the environment name from a scope path like "dev/runtime".
func envFromScope(scopePath string) string {
	parts := strings.SplitN(scopePath, "/", 2)
	return parts[0]
}

// writeManifestFile writes a manifest.json directly to disk.
func writeManifestFile(storeRoot, project, scopePath string, m *domain.Manifest) error {
	m.UpdatedAt = time.Now().UTC()
	path := filepath.Join(storeRoot, "projects", project, scopePath, "manifest.json")
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
