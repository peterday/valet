package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/identity"
)

// CreateInvite generates a temp keypair, adds the temp pubkey as a recipient
// on all scopes in the specified environments, and returns the invite + temp private key.
func (s *Store) CreateInvite(projectSlug string, envs []string, expiry time.Duration, maxUses int) (*domain.Invite, string, error) {
	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return nil, "", err
	}

	// Generate temp keypair.
	tempKey, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, "", fmt.Errorf("generating temp keypair: %w", err)
	}

	// Generate a short ID.
	idBytes := make([]byte, 4)
	rand.Read(idBytes)
	inviteID := hex.EncodeToString(idBytes)

	// Resolve creator name.
	createdBy := s.Identity.PublicKey
	users, _ := s.ListUsers()
	for _, u := range users {
		if u.PublicKey == s.Identity.PublicKey {
			createdBy = u.Name
			break
		}
	}

	invite := &domain.Invite{
		ID:           inviteID,
		TempPubKey:   tempKey.Recipient().String(),
		Environments: envs,
		CreatedBy:    createdBy,
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    time.Now().UTC().Add(expiry),
		MaxUses:      maxUses,
		Uses:         0,
	}

	// Add temp pubkey as recipient on all scopes in the specified environments.
	tempRecipient := domain.ManifestRecipient{
		Name:      "invite:" + inviteID,
		PublicKey: tempKey.Recipient().String(),
	}

	for _, env := range envs {
		scopes, err := s.ListScopes(slug, env)
		if err != nil {
			continue
		}
		for _, scope := range scopes {
			manifest, err := s.readManifest(slug, scope.Path)
			if err != nil {
				continue
			}
			manifest.Recipients = append(manifest.Recipients, tempRecipient)
			manifest.UpdatedAt = time.Now().UTC()
			if err := s.reencryptVault(slug, scope.Path, manifest); err != nil {
				return nil, "", fmt.Errorf("re-encrypting %s: %w", scope.Path, err)
			}
			if err := s.writeManifest(slug, scope.Path, manifest); err != nil {
				return nil, "", fmt.Errorf("writing manifest %s: %w", scope.Path, err)
			}
		}
	}

	// Save invite file.
	inviteDir := filepath.Join(s.Root, "invites")
	if err := os.MkdirAll(inviteDir, 0755); err != nil {
		return nil, "", err
	}

	data, err := json.MarshalIndent(invite, "", "  ")
	if err != nil {
		return nil, "", err
	}

	if err := os.WriteFile(filepath.Join(inviteDir, inviteID+".json"), data, 0644); err != nil {
		return nil, "", err
	}

	return invite, tempKey.String(), nil
}

// ConsumeInvite is called by the joining user. It uses the temp private key
// to decrypt vaults, replaces the temp recipient with the joiner's real key,
// adds the joiner as a user, and deletes or decrements the invite.
func (s *Store) ConsumeInvite(tempPrivKey string, joinerName string, joinerID *identity.Identity) error {
	// Parse the temp key to find which invite it belongs to.
	tempIdentity, err := age.ParseX25519Identity(tempPrivKey)
	if err != nil {
		return fmt.Errorf("invalid invite key: %w", err)
	}
	tempPubKey := tempIdentity.Recipient().String()

	// Find the invite by temp pubkey.
	invite, invitePath, err := s.findInviteByPubKey(tempPubKey)
	if err != nil {
		return err
	}

	// Check expiry.
	if time.Now().After(invite.ExpiresAt) {
		return fmt.Errorf("invite expired at %s", invite.ExpiresAt.Format("2006-01-02 15:04"))
	}

	// Check uses.
	if invite.MaxUses > 0 && invite.Uses >= invite.MaxUses {
		return fmt.Errorf("invite has been used the maximum number of times")
	}

	slug, err := s.resolveProject("")
	if err != nil {
		return err
	}

	// Add joiner as a user.
	if _, err := s.AddUser(joinerName, "", joinerID.PublicKey); err != nil {
		// User might already exist, that's OK.
		if !strings.Contains(err.Error(), "already exists") {
			return err
		}
	}

	// Replace temp recipient with joiner's real key on all invited scopes.
	joinerRecipient := domain.ManifestRecipient{
		Name:      joinerName,
		PublicKey: joinerID.PublicKey,
	}

	for _, env := range invite.Environments {
		scopes, err := s.ListScopes(slug, env)
		if err != nil {
			continue
		}
		for _, scope := range scopes {
			manifest, err := s.readManifest(slug, scope.Path)
			if err != nil {
				continue
			}

			// Read vault using temp key.
			vaultPath := filepath.Join(s.scopeDir(slug, scope.Path), "vault.age")
			vaultData, err := os.ReadFile(vaultPath)
			if err != nil {
				continue
			}

			content, err := decryptWithIdentity(vaultData, tempIdentity)
			if err != nil {
				continue
			}

			// Replace temp recipient with joiner.
			var newRecipients []domain.ManifestRecipient
			for _, r := range manifest.Recipients {
				if r.PublicKey == tempPubKey {
					continue // remove temp
				}
				newRecipients = append(newRecipients, r)
			}
			newRecipients = append(newRecipients, joinerRecipient)
			manifest.Recipients = newRecipients
			manifest.UpdatedAt = time.Now().UTC()

			// Re-encrypt to new recipient list.
			keys := recipientKeys(manifest.Recipients)
			newVault, err := encryptVaultContent(content, keys)
			if err != nil {
				return fmt.Errorf("re-encrypting %s: %w", scope.Path, err)
			}

			if err := os.WriteFile(vaultPath, newVault, 0644); err != nil {
				return err
			}
			if err := s.writeManifest(slug, scope.Path, manifest); err != nil {
				return err
			}
		}
	}

	// Update or delete invite.
	invite.Uses++
	if invite.MaxUses > 0 && invite.Uses >= invite.MaxUses {
		os.Remove(invitePath)
	} else {
		data, _ := json.MarshalIndent(invite, "", "  ")
		os.WriteFile(invitePath, data, 0644)
	}

	return nil
}

// PruneExpiredInvites removes expired invites and their temp recipients from scopes.
func (s *Store) PruneExpiredInvites(projectSlug string) (int, error) {
	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return 0, err
	}

	inviteDir := filepath.Join(s.Root, "invites")
	entries, err := os.ReadDir(inviteDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	pruned := 0
	now := time.Now()

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(inviteDir, e.Name()))
		if err != nil {
			continue
		}

		var invite domain.Invite
		if err := json.Unmarshal(data, &invite); err != nil {
			continue
		}

		if now.Before(invite.ExpiresAt) {
			continue // not expired
		}

		// Remove temp recipient from all scopes in invited environments.
		for _, env := range invite.Environments {
			scopes, err := s.ListScopes(slug, env)
			if err != nil {
				continue
			}
			for _, scope := range scopes {
				manifest, err := s.readManifest(slug, scope.Path)
				if err != nil {
					continue
				}

				var newRecipients []domain.ManifestRecipient
				changed := false
				for _, r := range manifest.Recipients {
					if r.PublicKey == invite.TempPubKey {
						changed = true
						continue
					}
					newRecipients = append(newRecipients, r)
				}

				if changed && len(newRecipients) > 0 {
					manifest.Recipients = newRecipients
					manifest.UpdatedAt = time.Now().UTC()
					s.reencryptVault(slug, scope.Path, manifest)
					s.writeManifest(slug, scope.Path, manifest)
				}
			}
		}

		// Delete invite file.
		os.Remove(filepath.Join(inviteDir, e.Name()))
		pruned++
	}

	return pruned, nil
}

// ListInvites returns all pending invites.
func (s *Store) ListInvites() ([]domain.Invite, error) {
	inviteDir := filepath.Join(s.Root, "invites")
	entries, err := os.ReadDir(inviteDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var invites []domain.Invite
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(inviteDir, e.Name()))
		if err != nil {
			continue
		}
		var inv domain.Invite
		if err := json.Unmarshal(data, &inv); err != nil {
			continue
		}
		invites = append(invites, inv)
	}
	return invites, nil
}

func (s *Store) findInviteByPubKey(tempPubKey string) (*domain.Invite, string, error) {
	inviteDir := filepath.Join(s.Root, "invites")
	entries, err := os.ReadDir(inviteDir)
	if err != nil {
		return nil, "", fmt.Errorf("no invites found")
	}

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(inviteDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var inv domain.Invite
		if err := json.Unmarshal(data, &inv); err != nil {
			continue
		}
		if inv.TempPubKey == tempPubKey {
			return &inv, path, nil
		}
	}

	return nil, "", fmt.Errorf("no matching invite found for this key")
}

// decryptWithIdentity decrypts vault data using a specific age identity.
func decryptWithIdentity(data []byte, id age.Identity) (*domain.VaultContent, error) {
	return decryptVaultWithIdentity(data, id)
}

// decryptVaultWithIdentity is the low-level decrypt using any age.Identity.
func decryptVaultWithIdentity(data []byte, id age.Identity) (*domain.VaultContent, error) {
	// Import from crypto package to avoid circular dependency.
	// We inline the decrypt logic here.
	r, err := age.Decrypt(bytes.NewReader(data), id)
	if err != nil {
		return nil, err
	}
	plaintext, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var content domain.VaultContent
	if err := json.Unmarshal(plaintext, &content); err != nil {
		return nil, err
	}
	return &content, nil
}

// encryptVaultContent encrypts vault content to the given recipient keys.
func encryptVaultContent(content *domain.VaultContent, recipientKeys []string) ([]byte, error) {
	// Import from crypto package.
	plaintext, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return nil, err
	}

	var recipients []age.Recipient
	for _, k := range recipientKeys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		r, err := age.ParseX25519Recipient(k)
		if err != nil {
			// Try SSH.
			rr, sshErr := age.ParseRecipients(strings.NewReader(k))
			if sshErr != nil {
				return nil, fmt.Errorf("parsing recipient %q: %w", k, err)
			}
			recipients = append(recipients, rr...)
			continue
		}
		recipients = append(recipients, r)
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipients...)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
