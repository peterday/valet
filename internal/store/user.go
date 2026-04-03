package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/peterday/valet/internal/domain"
)

// AddUser adds a user to the store with a single public key.
func (s *Store) AddUser(name, github, publicKey string) (*domain.User, error) {
	source := "manual"
	if strings.HasPrefix(publicKey, "age1") {
		source = "age-identity"
	}
	return s.AddUserWithKeys(name, github, []domain.UserKey{{Key: publicKey, Source: source}})
}

// AddUserWithKeys adds a user to the store with multiple labeled keys.
func (s *Store) AddUserWithKeys(name, github string, keys []domain.UserKey) (*domain.User, error) {
	if err := ValidateName(name, "user"); err != nil {
		return nil, err
	}

	// Validate and clean.
	var cleaned []domain.UserKey
	for _, k := range keys {
		k.Key = strings.TrimSpace(k.Key)
		if k.Key != "" {
			cleaned = append(cleaned, k)
		}
	}
	if len(cleaned) == 0 {
		return nil, fmt.Errorf("at least one public key required")
	}

	dir := filepath.Join(s.Root, "users")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	path := filepath.Join(dir, name+".json")
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("user %q already exists", name)
	}

	u := &domain.User{
		Name:      name,
		GitHub:    github,
		Keys:      cleaned,
		CreatedAt: time.Now().UTC(),
	}

	data, err := json.MarshalIndent(u, "", "  ")
	if err != nil {
		return nil, err
	}

	return u, os.WriteFile(path, data, 0644)
}

// AddUserKey adds an additional public key to an existing user.
func (s *Store) AddUserKey(name, newKey, label, source string) error {
	newKey = strings.TrimSpace(newKey)
	if newKey == "" {
		return fmt.Errorf("public key cannot be empty")
	}

	user, err := s.GetUser(name)
	if err != nil {
		return err
	}

	if user.HasKey(newKey) {
		return nil // already has this key
	}

	// Migrate to Keys format.
	user.Keys = append(user.AllUserKeys(), domain.UserKey{Key: newKey, Label: label, Source: source})
	user.PublicKey = ""
	user.PublicKeys = nil
	return s.writeUser(user)
}

// RemoveUserKey removes a specific public key from a user and re-encrypts
// all vaults where that key was a recipient. Returns the number of scopes re-encrypted.
func (s *Store) RemoveUserKey(name, keyToRemove string) (int, error) {
	user, err := s.GetUser(name)
	if err != nil {
		return 0, err
	}

	userKeys := user.AllUserKeys()
	var newKeys []domain.UserKey
	found := false
	for _, k := range userKeys {
		if k.Key == keyToRemove {
			found = true
			continue
		}
		newKeys = append(newKeys, k)
	}
	if !found {
		return 0, fmt.Errorf("key not found on user %q", name)
	}
	if len(newKeys) == 0 {
		return 0, fmt.Errorf("cannot remove last key from user %q", name)
	}

	user.Keys = newKeys
	user.PublicKey = ""
	user.PublicKeys = nil
	if err := s.writeUser(user); err != nil {
		return 0, err
	}

	// Remove the old key from manifests and re-encrypt.
	slug, err := s.resolveProject("")
	if err != nil {
		return 0, nil
	}

	allScopes, _ := s.ListAllScopes(slug)
	count := 0
	for _, scope := range allScopes {
		manifest, err := s.readManifest(slug, scope.Path)
		if err != nil {
			continue
		}

		changed := false
		var newRecipients []domain.ManifestRecipient
		for _, r := range manifest.Recipients {
			if r.PublicKey == keyToRemove {
				changed = true
				continue // remove this key
			}
			newRecipients = append(newRecipients, r)
		}
		if !changed {
			continue
		}

		manifest.Recipients = newRecipients
		manifest.UpdatedAt = time.Now().UTC()
		if err := s.reencryptVault(slug, scope.Path, manifest); err != nil {
			return count, fmt.Errorf("re-encrypting %s: %w", scope.Path, err)
		}
		if err := s.writeManifest(slug, scope.Path, manifest); err != nil {
			return count, fmt.Errorf("writing manifest %s: %w", scope.Path, err)
		}
		count++
	}

	return count, nil
}

// UpdateUser updates a user's metadata (GitHub handle, name).
// Does not change keys — use SyncUserKeys or AddUserKey for that.
func (s *Store) UpdateUser(name string, updates map[string]string) error {
	user, err := s.GetUser(name)
	if err != nil {
		return err
	}
	if gh, ok := updates["github"]; ok {
		user.GitHub = gh
	}
	return s.writeUser(user)
}

// writeUser writes a user JSON file.
func (s *Store) writeUser(u *domain.User) error {
	data, err := json.MarshalIndent(u, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Root, "users", u.Name+".json"), data, 0644)
}

// GetUser reads a user by name.
func (s *Store) GetUser(name string) (*domain.User, error) {
	data, err := os.ReadFile(filepath.Join(s.Root, "users", name+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("user %q not found", name)
		}
		return nil, err
	}

	var u domain.User
	if err := json.Unmarshal(data, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// ListUsers lists all users in the store.
func (s *Store) ListUsers() ([]domain.User, error) {
	dir := filepath.Join(s.Root, "users")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var users []domain.User
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		u, err := s.GetUser(name)
		if err != nil {
			continue
		}
		users = append(users, *u)
	}
	return users, nil
}

// SyncUserKeys replaces a user's key set and updates all manifests/vaults accordingly.
// New keys are added as recipients, removed keys are revoked. Returns counts.
func (s *Store) SyncUserKeys(name string, newKeys []domain.UserKey) (added, removed, scopesUpdated int, err error) {
	user, err := s.GetUser(name)
	if err != nil {
		return 0, 0, 0, err
	}

	oldKeys := user.AllKeys()
	oldSet := make(map[string]bool)
	for _, k := range oldKeys {
		oldSet[k] = true
	}
	newSet := make(map[string]bool)
	for _, k := range newKeys {
		newSet[k.Key] = true
	}

	// Compute diffs.
	var keysToAdd []string
	for _, k := range newKeys {
		if !oldSet[k.Key] {
			keysToAdd = append(keysToAdd, k.Key)
		}
	}
	var keysToRemove []string
	for _, k := range oldKeys {
		if !newSet[k] {
			keysToRemove = append(keysToRemove, k)
		}
	}

	// Always update user file (labels/sources may have changed even if keys haven't).
	user.Keys = newKeys
	user.PublicKey = ""
	user.PublicKeys = nil
	if err := s.writeUser(user); err != nil {
		return 0, 0, 0, err
	}

	if len(keysToAdd) == 0 && len(keysToRemove) == 0 {
		return 0, 0, 0, nil
	}

	// Update manifests: add new keys, remove old keys.
	slug, err := s.resolveProject("")
	if err != nil {
		return len(keysToAdd), len(keysToRemove), 0, nil
	}

	allScopes, _ := s.ListAllScopes(slug)
	for _, scope := range allScopes {
		manifest, err := s.readManifest(slug, scope.Path)
		if err != nil {
			continue
		}

		// Check if this user has any recipient entry on this scope.
		userOnScope := false
		for _, r := range manifest.Recipients {
			if r.Name == name || oldSet[r.PublicKey] {
				userOnScope = true
				break
			}
		}
		if !userOnScope {
			continue
		}

		changed := false

		// Remove old keys.
		var newRecipients []domain.ManifestRecipient
		for _, r := range manifest.Recipients {
			if r.Name == name && !newSet[r.PublicKey] {
				changed = true
				continue // drop this old key
			}
			newRecipients = append(newRecipients, r)
		}

		// Add new keys.
		existingKeys := make(map[string]bool)
		for _, r := range newRecipients {
			existingKeys[r.PublicKey] = true
		}
		for _, k := range keysToAdd {
			if !existingKeys[k] {
				newRecipients = append(newRecipients, domain.ManifestRecipient{
					Name:      name,
					PublicKey: k,
				})
				changed = true
			}
		}

		if !changed {
			continue
		}

		manifest.Recipients = newRecipients
		manifest.UpdatedAt = time.Now().UTC()
		if err := s.reencryptVault(slug, scope.Path, manifest); err != nil {
			return len(keysToAdd), len(keysToRemove), scopesUpdated, fmt.Errorf("re-encrypting %s: %w", scope.Path, err)
		}
		if err := s.writeManifest(slug, scope.Path, manifest); err != nil {
			return len(keysToAdd), len(keysToRemove), scopesUpdated, fmt.Errorf("writing manifest %s: %w", scope.Path, err)
		}
		scopesUpdated++
	}

	return len(keysToAdd), len(keysToRemove), scopesUpdated, nil
}

// RemoveUser removes a user from the store.
func (s *Store) RemoveUser(name string) error {
	path := filepath.Join(s.Root, "users", name+".json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("user %q not found", name)
	}
	return os.Remove(path)
}
