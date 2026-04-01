package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/peterday/valet/internal/crypto"
	"github.com/peterday/valet/internal/domain"
)

// SetSecret sets or updates a secret in a scope's vault.
func (s *Store) SetSecret(projectSlug, scopePath, key, value string) error {
	return s.setSecretInternal(projectSlug, scopePath, key, value, "")
}

// SetSecretWithProvider sets a secret and records its provider metadata.
func (s *Store) SetSecretWithProvider(projectSlug, scopePath, key, value, provider string) error {
	return s.setSecretInternal(projectSlug, scopePath, key, value, provider)
}

func (s *Store) setSecretInternal(projectSlug, scopePath, key, value, provider string) error {
	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return err
	}

	// Auto-create project, environment, and scope if they don't exist.
	if err := s.ensureScope(slug, scopePath); err != nil {
		return err
	}

	manifest, err := s.readManifest(slug, scopePath)
	if err != nil {
		return err
	}

	content, err := s.decryptVault(slug, scopePath)
	if err != nil {
		return err
	}

	// Update or add the secret, preserving history.
	version := 1
	if existing, ok := content.Secrets[key]; ok {
		version = existing.Version + 1
		// Save current version to history.
		if content.History == nil {
			content.History = make(map[string][]domain.VaultSecret)
		}
		content.History[key] = append([]domain.VaultSecret{existing}, content.History[key]...)
		// Keep at most 10 historical versions.
		if len(content.History[key]) > 10 {
			content.History[key] = content.History[key][:10]
		}
	}

	updatedBy := s.Identity.PublicKey
	users, _ := s.ListUsers()
	for _, u := range users {
		if u.PublicKey == s.Identity.PublicKey {
			updatedBy = u.Name
			break
		}
	}

	content.Secrets[key] = domain.VaultSecret{
		Value:     value,
		Version:   version,
		UpdatedAt: time.Now().UTC(),
		UpdatedBy: updatedBy,
	}

	// Update manifest secret list.
	found := false
	for _, name := range manifest.Secrets {
		if name == key {
			found = true
			break
		}
	}
	manifestChanged := false
	if !found {
		manifest.Secrets = append(manifest.Secrets, key)
		manifestChanged = true
	}

	// Update provider metadata if specified.
	if provider != "" {
		if manifest.Providers == nil {
			manifest.Providers = make(map[string]string)
		}
		manifest.Providers[key] = provider
		manifestChanged = true
	}

	if manifestChanged {
		manifest.UpdatedAt = time.Now().UTC()
		if err := s.writeManifest(slug, scopePath, manifest); err != nil {
			return err
		}
	}

	return s.encryptAndWriteVault(slug, scopePath, content, manifest)
}

// GetSecret retrieves a secret value from a scope.
func (s *Store) GetSecret(projectSlug, scopePath, key string) (*domain.VaultSecret, error) {
	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return nil, err
	}

	content, err := s.decryptVault(slug, scopePath)
	if err != nil {
		return nil, err
	}

	secret, ok := content.Secrets[key]
	if !ok {
		return nil, fmt.Errorf("secret %q not found in scope %q", key, scopePath)
	}

	return &secret, nil
}

// GetSecretHistory returns the current value plus previous versions of a secret.
func (s *Store) GetSecretHistory(projectSlug, scopePath, key string) (current *domain.VaultSecret, history []domain.VaultSecret, err error) {
	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return nil, nil, err
	}

	content, err := s.decryptVault(slug, scopePath)
	if err != nil {
		return nil, nil, err
	}

	secret, ok := content.Secrets[key]
	if !ok {
		return nil, nil, fmt.Errorf("secret %q not found in scope %q", key, scopePath)
	}

	var hist []domain.VaultSecret
	if content.History != nil {
		hist = content.History[key]
	}

	return &secret, hist, nil
}

// GetSecretFromEnv searches all scopes in an environment for a secret.
func (s *Store) GetSecretFromEnv(projectSlug, env, key string) (*domain.VaultSecret, string, error) {
	scopes, err := s.ListScopes(projectSlug, env)
	if err != nil {
		return nil, "", err
	}

	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return nil, "", err
	}

	for _, scope := range scopes {
		for _, name := range scope.Secrets {
			if name == key {
				secret, err := s.GetSecret(slug, scope.Path, key)
				if err != nil {
					continue
				}
				return secret, scope.Path, nil
			}
		}
	}

	return nil, "", fmt.Errorf("secret %q not found in environment %q", key, env)
}

// RemoveSecret removes a secret from a scope.
func (s *Store) RemoveSecret(projectSlug, scopePath, key string) error {
	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return err
	}

	manifest, err := s.readManifest(slug, scopePath)
	if err != nil {
		return err
	}

	content, err := s.decryptVault(slug, scopePath)
	if err != nil {
		return err
	}

	if _, ok := content.Secrets[key]; !ok {
		return fmt.Errorf("secret %q not found in scope %q", key, scopePath)
	}

	delete(content.Secrets, key)

	// Remove from manifest.
	var newSecrets []string
	for _, name := range manifest.Secrets {
		if name != key {
			newSecrets = append(newSecrets, name)
		}
	}
	manifest.Secrets = newSecrets
	manifest.UpdatedAt = time.Now().UTC()

	if err := s.writeManifest(slug, scopePath, manifest); err != nil {
		return err
	}

	return s.encryptAndWriteVault(slug, scopePath, content, manifest)
}

// ListSecretsInEnv returns all secret names in an environment, mapped to their scope path.
func (s *Store) ListSecretsInEnv(projectSlug, env string) (map[string]string, error) {
	scopes, err := s.ListScopes(projectSlug, env)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, scope := range scopes {
		for _, name := range scope.Secrets {
			result[name] = scope.Path
		}
	}
	return result, nil
}

// GetAllSecretsInEnv decrypts and returns all secrets in an environment.
func (s *Store) GetAllSecretsInEnv(projectSlug, env string) (map[string]string, error) {
	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return nil, err
	}

	scopes, err := s.ListScopes(slug, env)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, scope := range scopes {
		content, err := s.decryptVault(slug, scope.Path)
		if err != nil {
			return nil, fmt.Errorf("decrypting scope %q: %w", scope.Path, err)
		}
		for k, v := range content.Secrets {
			result[k] = v.Value
		}
	}
	return result, nil
}

// GetAllSecretsInScope decrypts and returns all secrets in a single scope.
func (s *Store) GetAllSecretsInScope(projectSlug, scopePath string) (map[string]string, error) {
	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return nil, err
	}

	content, err := s.decryptVault(slug, scopePath)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for k, v := range content.Secrets {
		result[k] = v.Value
	}
	return result, nil
}

// decryptVault reads and decrypts the vault for a scope.
func (s *Store) decryptVault(projectSlug, scopePath string) (*domain.VaultContent, error) {
	dir := s.scopeDir(projectSlug, scopePath)
	data, err := os.ReadFile(filepath.Join(dir, "vault.age"))
	if err != nil {
		return nil, fmt.Errorf("reading vault for scope %q: %w", scopePath, err)
	}

	return crypto.DecryptVault(data, s.ageIdentity())
}

// encryptAndWriteVault encrypts vault content and writes it to disk.
func (s *Store) encryptAndWriteVault(projectSlug, scopePath string, content *domain.VaultContent, manifest *domain.Manifest) error {
	keys := recipientKeys(manifest.Recipients)
	vaultData, err := crypto.EncryptVault(content, keys)
	if err != nil {
		return fmt.Errorf("encrypting vault: %w", err)
	}

	dir := s.scopeDir(projectSlug, scopePath)
	return os.WriteFile(filepath.Join(dir, "vault.age"), vaultData, 0644)
}
