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
	Category    string      `toml:"category,omitempty"`
	Description string      `toml:"description,omitempty"`
	SetupURL    string      `toml:"setup_url"`
	RevokeURL   string      `toml:"revoke_url,omitempty"` // defaults to setup_url if empty
	FreeTier    string      `toml:"free_tier,omitempty"`
	EnvVars     []EnvVar    `toml:"env_vars"`
	Validate    *Validation `toml:"validate,omitempty"`
	Rotation    Rotation    `toml:"rotation"`
	MasterKey   *MasterKey  `toml:"master_key,omitempty"`
}

// GetRevokeURL returns the revoke URL, falling back to setup URL.
func (p *Provider) GetRevokeURL() string {
	if p.RevokeURL != "" {
		return p.RevokeURL
	}
	return p.SetupURL
}

// EnvVar is a single environment variable declared by a provider.
type EnvVar struct {
	Name      string   `toml:"name"`
	Prefix    string   `toml:"prefix,omitempty"`    // single prefix (convenience)
	Prefixes  []string `toml:"prefixes,omitempty"`  // multiple valid prefixes
	Sensitive bool     `toml:"sensitive,omitempty"`
}

// AllPrefixes returns all valid prefixes for this env var.
// Merges prefix (singular) and prefixes (array).
func (ev *EnvVar) AllPrefixes() []string {
	var all []string
	if ev.Prefix != "" {
		all = append(all, ev.Prefix)
	}
	all = append(all, ev.Prefixes...)
	return all
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

// Search finds providers matching a query string. Matches against name,
// display name, category, description, and env var names (case-insensitive).
func (r *Registry) Search(query string) []*Provider {
	r.load()
	q := strings.ToLower(query)
	var results []*Provider
	for _, p := range r.providers {
		if matchesProvider(p, q) {
			results = append(results, p)
		}
	}
	return results
}

// FindByCategory returns all providers in a given category.
func (r *Registry) FindByCategory(category string) []*Provider {
	r.load()
	cat := strings.ToLower(category)
	var results []*Provider
	for _, p := range r.providers {
		if strings.ToLower(p.Category) == cat {
			results = append(results, p)
		}
	}
	return results
}

func matchesProvider(p *Provider, query string) bool {
	if strings.Contains(strings.ToLower(p.Name), query) {
		return true
	}
	if strings.Contains(strings.ToLower(p.DisplayName), query) {
		return true
	}
	if strings.Contains(strings.ToLower(p.Category), query) {
		return true
	}
	if strings.Contains(strings.ToLower(p.Description), query) {
		return true
	}
	for _, ev := range p.EnvVars {
		if strings.Contains(strings.ToLower(ev.Name), query) {
			return true
		}
	}
	return false
}

// CheckPrefix validates that a value matches an expected prefix for an env var.
func (r *Registry) CheckPrefix(envVar, value string) (providerName, expectedPrefix string, ok bool) {
	p := r.FindByEnvVar(envVar)
	if p == nil {
		return "", "", true
	}
	for _, ev := range p.EnvVars {
		if ev.Name != envVar {
			continue
		}
		prefixes := ev.AllPrefixes()
		if len(prefixes) == 0 {
			return "", "", true // no prefix defined
		}
		for _, pfx := range prefixes {
			if len(value) >= len(pfx) && value[:len(pfx)] == pfx {
				return p.DisplayName, pfx, true
			}
		}
		// None matched — report first prefix as expected.
		return p.DisplayName, prefixes[0], false
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
			fmt.Fprintf(os.Stderr, "warning: skipping provider file %s: %v\n", e.Name(), err)
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

// Search finds providers matching a query using the default registry.
func Search(query string) []*Provider {
	return getDefault().Search(query)
}

// FindByCategory returns providers in a category using the default registry.
func FindByCategory(category string) []*Provider {
	return getDefault().FindByCategory(category)
}

// CheckPrefix validates key format using the default registry.
func CheckPrefix(envVar, value string) (providerName, expectedPrefix string, ok bool) {
	return getDefault().CheckPrefix(envVar, value)
}
