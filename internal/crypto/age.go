package crypto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"filippo.io/age"
	"github.com/superset-studio/valet/internal/domain"
)

// ParseRecipients parses a list of public key strings into age recipients.
// Supports age X25519 keys (age1...) and SSH keys (ssh-ed25519, ssh-rsa).
func ParseRecipients(keys []string) ([]age.Recipient, error) {
	var recipients []age.Recipient
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		r, err := age.ParseX25519Recipient(k)
		if err != nil {
			// Try SSH key format
			rr, sshErr := age.ParseRecipients(strings.NewReader(k))
			if sshErr != nil {
				return nil, fmt.Errorf("parsing recipient %q: %w", k, err)
			}
			recipients = append(recipients, rr...)
			continue
		}
		recipients = append(recipients, r)
	}
	return recipients, nil
}

// EncryptVault encrypts vault content to the given recipients.
func EncryptVault(content *domain.VaultContent, recipientKeys []string) ([]byte, error) {
	plaintext, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling vault: %w", err)
	}

	recipients, err := ParseRecipients(recipientKeys)
	if err != nil {
		return nil, err
	}

	if len(recipients) == 0 {
		return nil, fmt.Errorf("no recipients specified")
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipients...)
	if err != nil {
		return nil, fmt.Errorf("creating encryptor: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("writing plaintext: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("closing encryptor: %w", err)
	}

	return buf.Bytes(), nil
}

// DecryptVault decrypts a vault.age blob using the given identity.
func DecryptVault(data []byte, identity age.Identity) (*domain.VaultContent, error) {
	r, err := age.Decrypt(bytes.NewReader(data), identity)
	if err != nil {
		return nil, fmt.Errorf("decrypting vault: %w", err)
	}

	plaintext, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading decrypted vault: %w", err)
	}

	var content domain.VaultContent
	if err := json.Unmarshal(plaintext, &content); err != nil {
		return nil, fmt.Errorf("parsing vault content: %w", err)
	}

	return &content, nil
}

// ReencryptVault decrypts a vault with the given identity and re-encrypts
// it to a new set of recipients.
func ReencryptVault(data []byte, identity age.Identity, newRecipientKeys []string) ([]byte, error) {
	content, err := DecryptVault(data, identity)
	if err != nil {
		return nil, err
	}
	return EncryptVault(content, newRecipientKeys)
}
