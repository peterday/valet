package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/identity"
)

// Store is the main handle to a valet store on disk.
type Store struct {
	Root     string
	Meta     *domain.StoreMeta
	Identity *identity.Identity

	// For single-project (in-repo) stores, this is set automatically.
	DefaultProject string
}

// Create initializes a new store at the given path.
func Create(root string, name string, storeType domain.StoreType, id *identity.Identity) (*Store, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, err
	}

	meta := &domain.StoreMeta{
		Version:   1,
		Name:      name,
		Type:      storeType,
		CreatedAt: time.Now().UTC(),
		CreatedBy: id.PublicKey,
	}

	s := &Store{Root: root, Meta: meta, Identity: id}

	if err := s.writeMeta(); err != nil {
		return nil, err
	}

	// Create users directory and add creator.
	if err := os.MkdirAll(filepath.Join(root, "users"), 0755); err != nil {
		return nil, err
	}

	return s, nil
}

// Open opens an existing store at the given path.
func Open(root string, id *identity.Identity) (*Store, error) {
	data, err := os.ReadFile(filepath.Join(root, "store.json"))
	if err != nil {
		return nil, fmt.Errorf("reading store.json: %w", err)
	}

	var meta domain.StoreMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parsing store.json: %w", err)
	}

	s := &Store{Root: root, Meta: &meta, Identity: id}

	// Auto-detect single project as default.
	if projects, err := s.ListProjects(); err == nil && len(projects) == 1 {
		s.DefaultProject = projects[0].Slug
	}

	return s, nil
}

// Resolve finds and opens a store from the current working directory.
// It walks up looking for .valet/store.json (in-repo) or .valet.toml (linked).
func Resolve(id *identity.Identity) (*Store, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	dir := cwd
	for {
		// Check for in-repo store.
		storeJSON := filepath.Join(dir, ".valet", "store.json")
		if _, err := os.Stat(storeJSON); err == nil {
			return Open(filepath.Join(dir, ".valet"), id)
		}

		// Check for .valet.toml link to a named store.
		tomlPath := filepath.Join(dir, ".valet.toml")
		if _, err := os.Stat(tomlPath); err == nil {
			cfg, err := loadValetToml(tomlPath)
			if err != nil {
				return nil, err
			}
			if cfg.Store != "" && cfg.Store != "." {
				storePath, err := namedStorePath(cfg.Store)
				if err != nil {
					return nil, err
				}
				s, err := Open(storePath, id)
				if err != nil {
					return nil, fmt.Errorf("opening store %q: %w", cfg.Store, err)
				}
				if cfg.Project != "" {
					s.DefaultProject = cfg.Project
				}
				return s, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return nil, fmt.Errorf("no valet store found — run 'valet init' to create one")
}

func (s *Store) writeMeta() error {
	data, err := json.MarshalIndent(s.Meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Root, "store.json"), data, 0644)
}

// projectRoot returns the path to a project's directory.
func (s *Store) projectRoot(slug string) string {
	return filepath.Join(s.Root, "projects", slug)
}

// scopeDir returns the path to a scope's directory.
// scopePath is like "dev/runtime" or "prod/db".
func (s *Store) scopeDir(projectSlug, scopePath string) string {
	return filepath.Join(s.projectRoot(projectSlug), scopePath)
}

// resolveProject returns the project slug, using DefaultProject if empty.
func (s *Store) resolveProject(slug string) (string, error) {
	if slug != "" {
		return slug, nil
	}
	if s.DefaultProject != "" {
		return s.DefaultProject, nil
	}
	return "", fmt.Errorf("no project specified and no default project set")
}

// ResolveDefaultProject returns the default project slug.
func (s *Store) ResolveDefaultProject() (string, error) {
	return s.resolveProject("")
}

// ageIdentity returns the age.Identity for decryption.
func (s *Store) ageIdentity() age.Identity {
	return s.Identity.AgeIdentity()
}

// recipientKeys extracts the public key strings from manifest recipients.
func recipientKeys(recipients []domain.ManifestRecipient) []string {
	keys := make([]string, len(recipients))
	for i, r := range recipients {
		keys[i] = r.PublicKey
	}
	return keys
}

func namedStorePath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".valet", "stores", name), nil
}

func loadValetToml(path string) (*domain.ValetConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Simple TOML parsing for our limited config format.
	cfg := &domain.ValetConfig{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "'\"")
		switch key {
		case "store":
			cfg.Store = val
		case "project":
			cfg.Project = val
		case "default_env":
			cfg.DefaultEnv = val
		}
	}
	return cfg, nil
}
