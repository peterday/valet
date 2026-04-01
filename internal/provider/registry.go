package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	toml "github.com/pelletier/go-toml/v2"
)

const (
	// DefaultRegistry is the default provider registry repo.
	DefaultRegistry = "https://github.com/peterday/valet-providers.git"
	// DefaultRegistryName is the directory name for the default registry.
	DefaultRegistryName = "default"
)

// Provider describes an API key provider.
type Provider struct {
	Name        string      `toml:"name"`
	DisplayName string      `toml:"display_name"`
	SetupURL    string      `toml:"setup_url"`
	FreeTier    string      `toml:"free_tier,omitempty"`
	EnvVars     []EnvVar    `toml:"env_vars"`
	Validate    *Validation `toml:"validate,omitempty"`
	Rotation    Rotation    `toml:"rotation"`
	MasterKey   *MasterKey  `toml:"master_key,omitempty"`
}

// EnvVar is a single environment variable declared by a provider.
type EnvVar struct {
	Name      string `toml:"name"`
	Prefix    string `toml:"prefix,omitempty"`
	Sensitive bool   `toml:"sensitive,omitempty"`
}

// Validation describes how to test if a key works.
type Validation struct {
	Method       string `toml:"method"`
	URL          string `toml:"url"`
	Auth         string `toml:"auth"`
	CostsCredits bool   `toml:"costs_credits,omitempty"`
}

// Rotation describes how key rotation works for this provider.
type Rotation struct {
	Strategy     string `toml:"strategy"`
	Programmatic bool   `toml:"programmatic,omitempty"`
	Warning      string `toml:"warning,omitempty"`
}

// MasterKey describes programmatic key creation from a master/admin key.
type MasterKey struct {
	EnvVar string `toml:"env_var"`
	Prefix string `toml:"prefix,omitempty"`
	Note   string `toml:"note,omitempty"`
}

// Registry loads and caches providers from TOML files on disk.
type Registry struct {
	mu        sync.Once
	providers map[string]*Provider
	dirs      []string // directories to scan (e.g. ~/.valet/providers/default/providers/)
}

// NewRegistry creates a registry that loads from the given base directory.
// It scans all subdirectories (each is a registry/tap) for provider TOML files.
func NewRegistry(baseDir string) *Registry {
	return &Registry{
		dirs: discoverRegistryDirs(baseDir),
	}
}

// Get returns a provider by canonical name, or nil if not found.
func (r *Registry) Get(name string) *Provider {
	r.load()
	return r.providers[name]
}

// FindByEnvVar searches for a provider that declares the given env var.
func (r *Registry) FindByEnvVar(envVar string) *Provider {
	r.load()
	for _, p := range r.providers {
		for _, ev := range p.EnvVars {
			if ev.Name == envVar {
				return p
			}
		}
	}
	return nil
}

// All returns all loaded providers.
func (r *Registry) All() map[string]*Provider {
	r.load()
	return r.providers
}

// CheckPrefix validates that a value matches the expected prefix for an env var.
func (r *Registry) CheckPrefix(envVar, value string) (providerName, expectedPrefix string, ok bool) {
	p := r.FindByEnvVar(envVar)
	if p == nil {
		return "", "", true
	}
	for _, ev := range p.EnvVars {
		if ev.Name == envVar && ev.Prefix != "" {
			if len(value) >= len(ev.Prefix) && value[:len(ev.Prefix)] == ev.Prefix {
				return p.DisplayName, ev.Prefix, true
			}
			return p.DisplayName, ev.Prefix, false
		}
	}
	return "", "", true
}

// load reads all TOML files from registry directories. Called once.
func (r *Registry) load() {
	r.mu.Do(func() {
		r.providers = make(map[string]*Provider)
		for _, dir := range r.dirs {
			r.loadDir(dir)
		}
	})
}

func (r *Registry) loadDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		p, err := loadProviderFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		r.providers[p.Name] = p
	}
}

func loadProviderFile(path string) (*Provider, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Provider
	if err := toml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if p.Name == "" {
		return nil, fmt.Errorf("%s: missing name", path)
	}
	return &p, nil
}

// discoverRegistryDirs finds all registry subdirectories under baseDir.
// Each subdirectory is a registry (like a Homebrew tap).
func discoverRegistryDirs(baseDir string) []string {
	var dirs []string
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return dirs
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Each registry has a providers/ subdirectory with TOML files.
		provDir := filepath.Join(baseDir, e.Name(), "providers")
		if info, err := os.Stat(provDir); err == nil && info.IsDir() {
			dirs = append(dirs, provDir)
		}
	}
	return dirs
}

// ProvidersBaseDir returns ~/.valet/providers/.
func ProvidersBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".valet", "providers")
}

// DefaultRegistryDir returns ~/.valet/providers/default/.
func DefaultRegistryDir() string {
	return filepath.Join(ProvidersBaseDir(), DefaultRegistryName)
}

// --- Package-level convenience functions using a default registry ---

var defaultRegistry *Registry
var defaultOnce sync.Once

func getDefault() *Registry {
	defaultOnce.Do(func() {
		defaultRegistry = NewRegistry(ProvidersBaseDir())
	})
	return defaultRegistry
}

// Get returns a provider by name from the default registry.
func Get(name string) *Provider {
	return getDefault().Get(name)
}

// FindByEnvVar searches the default registry for a provider by env var name.
func FindByEnvVar(envVar string) *Provider {
	return getDefault().FindByEnvVar(envVar)
}

// CheckPrefix validates key format using the default registry.
func CheckPrefix(envVar, value string) (providerName, expectedPrefix string, ok bool) {
	return getDefault().CheckPrefix(envVar, value)
}
