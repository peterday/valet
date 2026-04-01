package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/superset-studio/valet/internal/identity"
)

// ResolvedSecret is a secret with its source store name.
type ResolvedSecret struct {
	Key       string
	Value     string
	StoreName string
	ScopePath string
}

// ResolveAllSecrets merges secrets from multiple stores, in order.
// Later stores override earlier ones (project > shared > personal).
func ResolveAllSecrets(stores []*Store, env string) (map[string]ResolvedSecret, error) {
	result := make(map[string]ResolvedSecret)

	for _, s := range stores {
		project, err := s.resolveProject("")
		if err != nil {
			continue
		}

		secrets, err := s.GetAllSecretsInEnv(project, env)
		if err != nil {
			continue
		}

		for k, v := range secrets {
			scopePath := ""
			secretsInEnv, _ := s.ListSecretsInEnv(project, env)
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

	return result, nil
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

// OpenLinkedStores opens all stores linked to the project, in resolution order:
// local stores (from .valet.local.toml) → shared stores (from .valet.toml) → embedded store.
// Returns them in that order so later entries override earlier ones.
func OpenLinkedStores(localStoreRefs []string, sharedStoreRefs []string, embeddedStore *Store, id *identity.Identity) []*Store {
	var stores []*Store

	// 1. Local/personal stores (lowest priority).
	for _, ref := range localStoreRefs {
		if s := openStoreRef(ref, id); s != nil {
			stores = append(stores, s)
		}
	}

	// 2. Shared/team stores (middle priority).
	for _, ref := range sharedStoreRefs {
		if s := openStoreRef(ref, id); s != nil {
			stores = append(stores, s)
		}
	}

	// 3. Embedded store (highest priority — overrides everything).
	if embeddedStore != nil {
		stores = append(stores, embeddedStore)
	}

	return stores
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
