package crypto

import (
	"testing"

	"filippo.io/age"
	"github.com/peterday/valet/internal/domain"
)

func generateTestKeypair(t *testing.T) *age.X25519Identity {
	t.Helper()
	k, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestParseRecipients(t *testing.T) {
	k := generateTestKeypair(t)

	recipients, err := ParseRecipients([]string{k.Recipient().String()})
	if err != nil {
		t.Fatal(err)
	}
	if len(recipients) != 1 {
		t.Fatalf("expected 1 recipient, got %d", len(recipients))
	}
}

func TestParseRecipientsEmpty(t *testing.T) {
	recipients, err := ParseRecipients([]string{""})
	if err != nil {
		t.Fatal(err)
	}
	if len(recipients) != 0 {
		t.Fatalf("expected 0 recipients, got %d", len(recipients))
	}
}

func TestParseRecipientsInvalidKey(t *testing.T) {
	_, err := ParseRecipients([]string{"not-a-valid-key"})
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestEncryptDecryptVaultRoundTrip(t *testing.T) {
	k := generateTestKeypair(t)

	content := &domain.VaultContent{
		Secrets: map[string]domain.VaultSecret{
			"API_KEY": {Value: "sk-test-123", Version: 1},
			"DB_URL":  {Value: "postgres://localhost/db", Version: 1},
		},
	}

	encrypted, err := EncryptVault(content, []string{k.Recipient().String()})
	if err != nil {
		t.Fatal(err)
	}

	decrypted, err := DecryptVault(encrypted, k)
	if err != nil {
		t.Fatal(err)
	}

	if len(decrypted.Secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(decrypted.Secrets))
	}
	if decrypted.Secrets["API_KEY"].Value != "sk-test-123" {
		t.Fatalf("API_KEY mismatch: %s", decrypted.Secrets["API_KEY"].Value)
	}
	if decrypted.Secrets["DB_URL"].Value != "postgres://localhost/db" {
		t.Fatalf("DB_URL mismatch: %s", decrypted.Secrets["DB_URL"].Value)
	}
}

func TestEncryptVaultNoRecipients(t *testing.T) {
	content := &domain.VaultContent{Secrets: map[string]domain.VaultSecret{}}
	_, err := EncryptVault(content, []string{})
	if err == nil {
		t.Fatal("expected error with no recipients")
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	k1 := generateTestKeypair(t)
	k2 := generateTestKeypair(t)

	content := &domain.VaultContent{
		Secrets: map[string]domain.VaultSecret{
			"SECRET": {Value: "hidden"},
		},
	}

	encrypted, err := EncryptVault(content, []string{k1.Recipient().String()})
	if err != nil {
		t.Fatal(err)
	}

	_, err = DecryptVault(encrypted, k2)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestEncryptVaultEmptySecrets(t *testing.T) {
	k := generateTestKeypair(t)
	content := &domain.VaultContent{Secrets: map[string]domain.VaultSecret{}}

	encrypted, err := EncryptVault(content, []string{k.Recipient().String()})
	if err != nil {
		t.Fatal(err)
	}

	decrypted, err := DecryptVault(encrypted, k)
	if err != nil {
		t.Fatal(err)
	}
	if len(decrypted.Secrets) != 0 {
		t.Fatalf("expected 0 secrets, got %d", len(decrypted.Secrets))
	}
}

func TestMultipleRecipients(t *testing.T) {
	k1 := generateTestKeypair(t)
	k2 := generateTestKeypair(t)

	content := &domain.VaultContent{
		Secrets: map[string]domain.VaultSecret{
			"SHARED": {Value: "shared-secret"},
		},
	}

	encrypted, err := EncryptVault(content, []string{
		k1.Recipient().String(),
		k2.Recipient().String(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Both keys should be able to decrypt
	d1, err := DecryptVault(encrypted, k1)
	if err != nil {
		t.Fatal("k1 should decrypt:", err)
	}
	if d1.Secrets["SHARED"].Value != "shared-secret" {
		t.Fatal("k1 decrypted wrong value")
	}

	d2, err := DecryptVault(encrypted, k2)
	if err != nil {
		t.Fatal("k2 should decrypt:", err)
	}
	if d2.Secrets["SHARED"].Value != "shared-secret" {
		t.Fatal("k2 decrypted wrong value")
	}
}

func TestReencryptVault(t *testing.T) {
	k1 := generateTestKeypair(t)
	k2 := generateTestKeypair(t)

	content := &domain.VaultContent{
		Secrets: map[string]domain.VaultSecret{
			"SECRET": {Value: "my-secret"},
		},
	}

	// Encrypt to k1 only
	encrypted, err := EncryptVault(content, []string{k1.Recipient().String()})
	if err != nil {
		t.Fatal(err)
	}

	// k2 cannot decrypt
	_, err = DecryptVault(encrypted, k2)
	if err == nil {
		t.Fatal("k2 should not decrypt before re-encryption")
	}

	// Re-encrypt to both k1 and k2
	reencrypted, err := ReencryptVault(encrypted, k1, []string{
		k1.Recipient().String(),
		k2.Recipient().String(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Now k2 can decrypt
	d, err := DecryptVault(reencrypted, k2)
	if err != nil {
		t.Fatal("k2 should decrypt after re-encryption:", err)
	}
	if d.Secrets["SECRET"].Value != "my-secret" {
		t.Fatal("wrong value after re-encryption")
	}
}

func TestEncryptSpecialCharacters(t *testing.T) {
	k := generateTestKeypair(t)

	content := &domain.VaultContent{
		Secrets: map[string]domain.VaultSecret{
			"UNICODE":  {Value: "日本語テスト 🔑"},
			"NEWLINES": {Value: "line1\nline2\nline3"},
			"QUOTES":   {Value: `she said "hello" and it's fine`},
			"EMPTY":    {Value: ""},
			"URL":      {Value: "postgres://user:p@ss!w0rd@host:5432/db?sslmode=require"},
		},
	}

	encrypted, err := EncryptVault(content, []string{k.Recipient().String()})
	if err != nil {
		t.Fatal(err)
	}

	decrypted, err := DecryptVault(encrypted, k)
	if err != nil {
		t.Fatal(err)
	}

	for key, original := range content.Secrets {
		got := decrypted.Secrets[key]
		if got.Value != original.Value {
			t.Fatalf("%s: expected %q, got %q", key, original.Value, got.Value)
		}
	}
}
