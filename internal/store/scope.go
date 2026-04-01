package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/peterday/valet/internal/crypto"
	"github.com/peterday/valet/internal/domain"
)

// CreateScope creates a scope directory with an empty manifest and vault.
// scopePath is like "dev/runtime" or "prod/integrations/stripe".
func (s *Store) CreateScope(projectSlug, scopePath string) error {
	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return err
	}

	dir := s.scopeDir(slug, scopePath)
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err == nil {
		return fmt.Errorf("scope %q already exists", scopePath)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// The creator is the first recipient.
	manifest := &domain.Manifest{
		Secrets: []string{},
		Recipients: []domain.ManifestRecipient{
			{Name: s.Identity.PublicKey, PublicKey: s.Identity.PublicKey},
		},
		UpdatedAt: time.Now().UTC(),
	}

	// Try to use a friendly name from the users directory.
	users, _ := s.ListUsers()
	for _, u := range users {
		if u.PublicKey == s.Identity.PublicKey {
			manifest.Recipients[0].Name = u.Name
			break
		}
	}

	if err := s.writeManifest(slug, scopePath, manifest); err != nil {
		return err
	}

	// Create empty vault.
	content := &domain.VaultContent{Secrets: map[string]domain.VaultSecret{}}
	keys := recipientKeys(manifest.Recipients)
	vaultData, err := crypto.EncryptVault(content, keys)
	if err != nil {
		return fmt.Errorf("encrypting initial vault: %w", err)
	}

	return os.WriteFile(filepath.Join(dir, "vault.age"), vaultData, 0644)
}

// ListScopes lists all scopes under an environment for a project.
func (s *Store) ListScopes(projectSlug, env string) ([]domain.Scope, error) {
	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return nil, err
	}

	envDir := filepath.Join(s.projectRoot(slug), env)
	var scopes []domain.Scope

	err = filepath.Walk(envDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() != "manifest.json" {
			return nil
		}

		dir := filepath.Dir(path)
		rel, _ := filepath.Rel(s.projectRoot(slug), dir)
		manifest, mErr := s.readManifest(slug, rel)
		if mErr != nil {
			return nil
		}

		scopes = append(scopes, domain.Scope{
			Path:       rel,
			Secrets:    manifest.Secrets,
			Recipients: manifest.Recipients,
		})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return scopes, nil
}

// ListAllScopes lists all scopes across all environments.
func (s *Store) ListAllScopes(projectSlug string) ([]domain.Scope, error) {
	envs, err := s.ListEnvironments(projectSlug)
	if err != nil {
		return nil, err
	}
	var all []domain.Scope
	for _, env := range envs {
		scopes, err := s.ListScopes(projectSlug, env)
		if err != nil {
			continue
		}
		all = append(all, scopes...)
	}
	return all, nil
}

// AddRecipient adds a user as a recipient on a scope and re-encrypts the vault.
func (s *Store) AddRecipient(projectSlug, scopePath, userName string) error {
	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return err
	}

	user, err := s.GetUser(userName)
	if err != nil {
		return err
	}

	manifest, err := s.readManifest(slug, scopePath)
	if err != nil {
		return err
	}

	// Check if already a recipient.
	for _, r := range manifest.Recipients {
		if r.Name == userName {
			return fmt.Errorf("user %q is already a recipient on scope %q", userName, scopePath)
		}
	}

	manifest.Recipients = append(manifest.Recipients, domain.ManifestRecipient{
		Name:      user.Name,
		PublicKey: user.PublicKey,
	})
	manifest.UpdatedAt = time.Now().UTC()

	// Re-encrypt vault with new recipient list.
	if err := s.reencryptVault(slug, scopePath, manifest); err != nil {
		return err
	}

	return s.writeManifest(slug, scopePath, manifest)
}

// RemoveRecipient removes a user from a scope and re-encrypts the vault.
func (s *Store) RemoveRecipient(projectSlug, scopePath, userName string) error {
	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return err
	}

	manifest, err := s.readManifest(slug, scopePath)
	if err != nil {
		return err
	}

	found := false
	var newRecipients []domain.ManifestRecipient
	for _, r := range manifest.Recipients {
		if r.Name == userName {
			found = true
			continue
		}
		newRecipients = append(newRecipients, r)
	}
	if !found {
		return fmt.Errorf("user %q is not a recipient on scope %q", userName, scopePath)
	}
	if len(newRecipients) == 0 {
		return fmt.Errorf("cannot remove last recipient from scope %q", scopePath)
	}

	manifest.Recipients = newRecipients
	manifest.UpdatedAt = time.Now().UTC()

	if err := s.reencryptVault(slug, scopePath, manifest); err != nil {
		return err
	}

	return s.writeManifest(slug, scopePath, manifest)
}

// GrantEnvironment adds a user as a recipient on all scopes in an environment.
func (s *Store) GrantEnvironment(projectSlug, env, userName string) (int, error) {
	scopes, err := s.ListScopes(projectSlug, env)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, scope := range scopes {
		// Skip if already a recipient.
		alreadyRecipient := false
		for _, r := range scope.Recipients {
			if r.Name == userName {
				alreadyRecipient = true
				break
			}
		}
		if alreadyRecipient {
			continue
		}

		if err := s.AddRecipient(projectSlug, scope.Path, userName); err != nil {
			return count, fmt.Errorf("granting %q on scope %q: %w", userName, scope.Path, err)
		}
		count++
	}
	return count, nil
}

// RevokeEnvironment removes a user from all scopes in an environment.
func (s *Store) RevokeEnvironment(projectSlug, env, userName string) (int, error) {
	scopes, err := s.ListScopes(projectSlug, env)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, scope := range scopes {
		isRecipient := false
		for _, r := range scope.Recipients {
			if r.Name == userName {
				isRecipient = true
				break
			}
		}
		if !isRecipient {
			continue
		}

		if err := s.RemoveRecipient(projectSlug, scope.Path, userName); err != nil {
			return count, fmt.Errorf("revoking %q from scope %q: %w", userName, scope.Path, err)
		}
		count++
	}
	return count, nil
}

// FlagSecretsForRotation flags all secrets in a scope as needing rotation.
func (s *Store) FlagSecretsForRotation(projectSlug, scopePath, reason string) (int, error) {
	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return 0, err
	}

	manifest, err := s.readManifest(slug, scopePath)
	if err != nil {
		return 0, err
	}

	if manifest.RotationFlags == nil {
		manifest.RotationFlags = make(map[string]domain.RotationFlag)
	}

	count := 0
	for _, name := range manifest.Secrets {
		if _, already := manifest.RotationFlags[name]; !already {
			manifest.RotationFlags[name] = domain.RotationFlag{
				FlaggedAt: time.Now().UTC(),
				Reason:    reason,
			}
			count++
		}
	}

	if count > 0 {
		manifest.UpdatedAt = time.Now().UTC()
		if err := s.writeManifest(slug, scopePath, manifest); err != nil {
			return 0, err
		}
	}

	return count, nil
}

// RevokeEnvironmentWithRotation revokes a user and flags all affected secrets for rotation.
func (s *Store) RevokeEnvironmentWithRotation(projectSlug, env, userName string) (scopeCount, secretCount int, err error) {
	scopes, err := s.ListScopes(projectSlug, env)
	if err != nil {
		return 0, 0, err
	}

	for _, scope := range scopes {
		isRecipient := false
		for _, r := range scope.Recipients {
			if r.Name == userName {
				isRecipient = true
				break
			}
		}
		if !isRecipient {
			continue
		}

		if err := s.RemoveRecipient(projectSlug, scope.Path, userName); err != nil {
			return scopeCount, secretCount, fmt.Errorf("revoking %q from scope %q: %w", userName, scope.Path, err)
		}
		scopeCount++

		reason := fmt.Sprintf("user %q revoked", userName)
		n, err := s.FlagSecretsForRotation(projectSlug, scope.Path, reason)
		if err != nil {
			return scopeCount, secretCount, err
		}
		secretCount += n
	}

	return scopeCount, secretCount, nil
}

// readManifest reads the manifest.json for a scope.
func (s *Store) readManifest(projectSlug, scopePath string) (*domain.Manifest, error) {
	dir := s.scopeDir(projectSlug, scopePath)
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("reading manifest for scope %q: %w", scopePath, err)
	}

	var m domain.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// writeManifest writes the manifest.json for a scope.
func (s *Store) writeManifest(projectSlug, scopePath string, manifest *domain.Manifest) error {
	dir := s.scopeDir(projectSlug, scopePath)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0644)
}

// reencryptVault re-encrypts the vault with the current manifest recipients.
func (s *Store) reencryptVault(projectSlug, scopePath string, manifest *domain.Manifest) error {
	dir := s.scopeDir(projectSlug, scopePath)
	vaultPath := filepath.Join(dir, "vault.age")

	data, err := os.ReadFile(vaultPath)
	if err != nil {
		return fmt.Errorf("reading vault: %w", err)
	}

	keys := recipientKeys(manifest.Recipients)
	newData, err := crypto.ReencryptVault(data, s.ageIdentity(), keys)
	if err != nil {
		return err
	}

	return os.WriteFile(vaultPath, newData, 0644)
}

// ensureScope auto-creates the project, environment, and scope if they don't exist.
func (s *Store) ensureScope(projectSlug, scopePath string) error {
	// Ensure project exists.
	projDir := s.projectRoot(projectSlug)
	if _, err := os.Stat(filepath.Join(projDir, "project.json")); os.IsNotExist(err) {
		if _, err := s.CreateProject(projectSlug); err != nil {
			return err
		}
	}

	// Ensure environment directory exists.
	env := envFromScopePath(scopePath)
	envDir := filepath.Join(projDir, env)
	if _, err := os.Stat(envDir); os.IsNotExist(err) {
		if err := os.MkdirAll(envDir, 0755); err != nil {
			return err
		}
	}

	// Ensure scope exists.
	dir := s.scopeDir(projectSlug, scopePath)
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); os.IsNotExist(err) {
		return s.CreateScope(projectSlug, scopePath)
	}

	return nil
}

// envFromScopePath extracts the environment name from a scope path like "dev/runtime".
func envFromScopePath(scopePath string) string {
	parts := strings.SplitN(scopePath, "/", 2)
	return parts[0]
}
