package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/identity"
)

// ResolvedSecret is a secret with its source store name and resolution metadata.
type ResolvedSecret struct {
	Key       string
	Value     string
	StoreName string
	ScopePath string
	Wildcard  bool // true if resolved from * environment
}

// ResolveAllSecrets merges secrets from multiple stores, in order.
// Later stores override earlier ones (personal → shared → embedded → local).
// Within each store, exact env matches take precedence over wildcard (*).
func ResolveAllSecrets(stores []*Store, env string) (map[string]ResolvedSecret, error) {
	result := make(map[string]ResolvedSecret)

	for _, s := range stores {
		project, err := s.resolveProject("")
		if err != nil {
			continue
		}

		// First, load wildcard (*) secrets as base.
		wildcardSecrets, _ := s.GetAllSecretsInEnv(project, "*")
		wildcardInEnv, _ := s.ListSecretsInEnv(project, "*")
		for k, v := range wildcardSecrets {
			scopePath := ""
			if sp, ok := wildcardInEnv[k]; ok {
				scopePath = sp
			}
			result[k] = ResolvedSecret{
				Key:       k,
				Value:     v,
				StoreName: s.Meta.Name,
				ScopePath: scopePath,
				Wildcard:  true,
			}
		}

		// Then, load exact env secrets — these override wildcards.
		if env != "*" {
			secrets, err := s.GetAllSecretsInEnv(project, env)
			if err != nil {
				continue
			}
			secretsInEnv, _ := s.ListSecretsInEnv(project, env)

			for k, v := range secrets {
				scopePath := ""
				if sp, ok := secretsInEnv[k]; ok {
					scopePath = sp
				}
				result[k] = ResolvedSecret{
					Key:       k,
					Value:     v,
					StoreName: s.Meta.Name,
					ScopePath: scopePath,
				}
			}
		}
	}

	return result, nil
}

// ResolveAllSecretsWithProvenance resolves secrets like ResolveAllSecrets but
// also tracks what each key would have resolved to at each layer. Used by
// valet resolve --verbose.
func ResolveAllSecretsWithProvenance(stores []*Store, env string, overrides map[string]string) (map[string]ResolvedSecret, map[string][]ResolvedSecret, error) {
	// provenance[key] = list of all sources that have this key, in resolution order.
	provenance := make(map[string][]ResolvedSecret)

	result, err := ResolveAllSecrets(stores, env)
	if err != nil {
		return nil, nil, err
	}

	// Build provenance: iterate stores in order, record each source.
	for _, s := range stores {
		project, err := s.resolveProject("")
		if err != nil {
			continue
		}

		// Wildcard secrets.
		wildcardSecrets, _ := s.GetAllSecretsInEnv(project, "*")
		wildcardInEnv, _ := s.ListSecretsInEnv(project, "*")
		for k, v := range wildcardSecrets {
			sp := ""
			if p, ok := wildcardInEnv[k]; ok {
				sp = p
			}
			provenance[k] = append(provenance[k], ResolvedSecret{
				Key: k, Value: v, StoreName: s.Meta.Name, ScopePath: sp, Wildcard: true,
			})
		}

		// Exact env secrets.
		if env != "*" {
			secrets, _ := s.GetAllSecretsInEnv(project, env)
			secretsInEnv, _ := s.ListSecretsInEnv(project, env)
			for k, v := range secrets {
				sp := ""
				if p, ok := secretsInEnv[k]; ok {
					sp = p
				}
				provenance[k] = append(provenance[k], ResolvedSecret{
					Key: k, Value: v, StoreName: s.Meta.Name, ScopePath: sp,
				})
			}
		}
	}

	// Apply overrides on top.
	if len(overrides) > 0 {
		for k, v := range overrides {
			result[k] = ResolvedSecret{
				Key:       k,
				Value:     v,
				StoreName: "--set",
				ScopePath: "(command line)",
			}
			provenance[k] = append(provenance[k], result[k])
		}
	}

	return result, provenance, nil
}

// ResolveAllSecretsFlat returns just key=value from merged stores.
func ResolveAllSecretsFlat(stores []*Store, env string) (map[string]string, error) {
	resolved, err := ResolveAllSecrets(stores, env)
	if err != nil {
		return nil, err
	}

	flat := make(map[string]string, len(resolved))
	for k, v := range resolved {
		flat[k] = v.Value
	}
	return flat, nil
}

// OpenLinkedStores opens all stores linked to the project, in resolution order.
// Later entries override earlier ones. Full resolution order:
//   1. Personal/local linked stores (lowest priority)
//   2. Shared/team linked stores
//   3. Embedded store (.valet/)
//   4. Local override store (.valet.local/) — highest priority
func OpenLinkedStores(localLinks []domain.StoreLink, sharedLinks []domain.StoreLink, embeddedStore *Store, localStore *Store, id *identity.Identity) []*Store {
	var stores []*Store

	// 1. Personal linked stores (lowest priority).
	for _, link := range localLinks {
		ref := link.Name
		if link.URL != "" {
			ref = link.URL
		}
		if s := openStoreRef(ref, id); s != nil {
			stores = append(stores, s)
		}
	}

	// 2. Shared/team linked stores.
	for _, link := range sharedLinks {
		ref := link.Name
		if link.URL != "" {
			ref = link.URL
		}
		if s := openStoreRef(ref, id); s != nil {
			stores = append(stores, s)
		}
	}

	// 3. Embedded store (.valet/).
	if embeddedStore != nil {
		stores = append(stores, embeddedStore)
	}

	// 4. Local override store (.valet.local/) — highest priority.
	if localStore != nil {
		stores = append(stores, localStore)
	}

	return stores
}

// OpenLocalStore opens the .valet.local/ store if it exists.
// Returns nil (no error) if it doesn't exist.
func OpenLocalStore(projectDir string, id *identity.Identity) *Store {
	localPath := filepath.Join(projectDir, ".valet.local")
	if _, err := os.Stat(filepath.Join(localPath, "store.json")); os.IsNotExist(err) {
		return nil
	}
	s, err := Open(localPath, id)
	if err != nil {
		return nil
	}
	s.Meta.Name = ".valet.local"
	return s
}

// CreateLocalStore creates the .valet.local/ store for local developer overrides.
func CreateLocalStore(projectDir string, id *identity.Identity) (*Store, error) {
	localPath := filepath.Join(projectDir, ".valet.local")

	// If it already exists, just open it.
	if _, err := os.Stat(filepath.Join(localPath, "store.json")); err == nil {
		return Open(localPath, id)
	}

	s, err := Create(localPath, ".valet.local", domain.StoreTypeLocal, id)
	if err != nil {
		return nil, err
	}

	// Add .valet.local/ to .gitignore.
	gitignorePath := filepath.Join(projectDir, ".gitignore")
	ensureLineInFile(gitignorePath, ".valet.local/")

	return s, nil
}

func ensureLineInFile(path, line string) {
	data, _ := os.ReadFile(path)
	for _, existing := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(existing) == line {
			return
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(data) > 0 && data[len(data)-1] != '\n' {
		f.WriteString("\n")
	}
	f.WriteString(line + "\n")
}

// StoreLinkNames extracts store names from a slice of StoreLinks.
func StoreLinkNames(links []domain.StoreLink) []string {
	names := make([]string, len(links))
	for i, l := range links {
		names[i] = l.Name
	}
	return names
}

// HasStoreLink checks if a store name exists in a slice of StoreLinks.
func HasStoreLink(links []domain.StoreLink, name string) bool {
	for _, l := range links {
		if l.Name == name {
			return true
		}
	}
	return false
}

// openStoreRef opens a store by name or finds it by remote URL.
// If the ref includes a project (github:org/repo/project), sets DefaultProject.
func openStoreRef(ref string, id *identity.Identity) *Store {
	storesDir := storesBaseDir()
	if storesDir == "" {
		return nil
	}

	uri := ParseStoreURI(ref)

	if uri.IsRemote {
		s := findStoreByRemote(uri.Remote, id)
		if s == nil {
			// Try by inferred local name.
			storePath := filepath.Join(storesDir, uri.StoreName)
			var err error
			s, err = Open(storePath, id)
			if err != nil {
				return nil
			}
		}
		if s != nil && uri.Project != "" {
			s.DefaultProject = uri.Project
		}
		return s
	}

	// Local name.
	storePath := filepath.Join(storesDir, uri.StoreName)
	s, err := Open(storePath, id)
	if err != nil {
		return nil
	}
	if uri.Project != "" {
		s.DefaultProject = uri.Project
	}
	return s
}

// FindStoreByName looks up a store by name in ~/.valet/stores/.
func FindStoreByName(name string, id *identity.Identity) (*Store, error) {
	dir := storesBaseDir()
	if dir == "" {
		return nil, fmt.Errorf("cannot determine home directory")
	}
	storePath := filepath.Join(dir, name)
	if _, err := os.Stat(filepath.Join(storePath, "store.json")); os.IsNotExist(err) {
		return nil, fmt.Errorf("store %q not found", name)
	}
	return Open(storePath, id)
}

// FindStoreByRemoteOrName finds a store by remote URL or name.
// If ref is a remote URL, searches all stores for a matching remote.
// If ref is a name, opens by name.
func FindStoreByRemoteOrName(ref string, id *identity.Identity) (*Store, error) {
	if isRemoteRef(ref) {
		expanded := expandRemoteRef(ref)
		s := findStoreByRemote(expanded, id)
		if s != nil {
			return s, nil
		}
		return nil, fmt.Errorf("no local store found for remote %q — run 'valet link %s' to clone it", ref, ref)
	}
	return FindStoreByName(ref, id)
}

// ListAllStores lists all stores in ~/.valet/stores/.
func ListAllStores(id *identity.Identity) ([]*Store, error) {
	dir := storesBaseDir()
	if dir == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var stores []*Store
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		storePath := filepath.Join(dir, e.Name())
		s, err := Open(storePath, id)
		if err != nil {
			continue
		}
		stores = append(stores, s)
	}
	return stores, nil
}

// SearchStoresForSecret searches all local stores for a secret by name.
func SearchStoresForSecret(key, env string, id *identity.Identity) ([]ResolvedSecret, error) {
	allStores, err := ListAllStores(id)
	if err != nil {
		return nil, err
	}

	var matches []ResolvedSecret
	for _, s := range allStores {
		project, err := s.resolveProject("")
		if err != nil {
			continue
		}

		secret, scopePath, err := s.GetSecretFromEnv(project, env, key)
		if err != nil {
			continue
		}

		matches = append(matches, ResolvedSecret{
			Key:       key,
			Value:     secret.Value,
			StoreName: s.Meta.Name,
			ScopePath: scopePath,
		})
	}

	return matches, nil
}

func findStoreByRemote(remoteURL string, id *identity.Identity) *Store {
	dir := storesBaseDir()
	if dir == "" {
		return nil
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		storePath := filepath.Join(dir, e.Name())
		s, err := Open(storePath, id)
		if err != nil {
			continue
		}
		if s.Meta.Remote == remoteURL {
			return s
		}
	}
	return nil
}

func storesBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".valet", "stores")
}

func isRemoteRef(ref string) bool {
	return strings.Contains(ref, ":") || strings.HasPrefix(ref, "git@") || strings.HasPrefix(ref, "https://")
}

func expandRemoteRef(ref string) string {
	if strings.HasPrefix(ref, "github:") {
		path := strings.TrimPrefix(ref, "github:")
		return fmt.Sprintf("git@github.com:%s.git", path)
	}
	return ref
}
