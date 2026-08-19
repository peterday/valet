package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"github.com/peterday/valet/internal/domain"
	"golang.org/x/crypto/ssh"
)

func vaultFixture() *domain.VaultContent {
	return &domain.VaultContent{
		Secrets: map[string]domain.VaultSecret{
			"API_KEY": {Value: "s3cret", Version: 1},
		},
	}
}

// newSSHKey returns an age identity for a fresh ed25519 SSH key, plus the
// authorized_keys-format public key valet would store as a recipient.
func newSSHKey(t *testing.T) (age.Identity, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	id, err := agessh.NewEd25519Identity(priv)
	if err != nil {
		t.Fatal(err)
	}
	return id, string(ssh.MarshalAuthorizedKey(sshPub))
}

// A vault encrypted to an SSH recipient must be decryptable by the matching
// SSH key. This is the case that previously failed: valet accepts SSH public
// keys as recipients, so it could write vaults it could not read back.
func TestDecryptVaultWithSSHIdentity(t *testing.T) {
	sshID, sshPub := newSSHKey(t)

	encrypted, err := EncryptVault(vaultFixture(), []string{sshPub})
	if err != nil {
		t.Fatal(err)
	}

	got, err := DecryptVault(encrypted, sshID)
	if err != nil {
		t.Fatalf("SSH identity should decrypt a vault encrypted to its public key: %v", err)
	}
	if got.Secrets["API_KEY"].Value != "s3cret" {
		t.Fatalf("round-trip mismatch: got %q", got.Secrets["API_KEY"].Value)
	}
}

// The age key and SSH keys are tried together, so a vault encrypted to either
// one opens with the same identity set.
func TestDecryptVaultTriesEveryIdentity(t *testing.T) {
	ageID, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	sshID, sshPub := newSSHKey(t)
	identities := []age.Identity{ageID, sshID}

	for _, tc := range []struct {
		name      string
		recipient string
	}{
		{"encrypted to the age key", ageID.Recipient().String()},
		{"encrypted to the ssh key", sshPub},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encrypted, err := EncryptVault(vaultFixture(), []string{tc.recipient})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecryptVault(encrypted, identities...); err != nil {
				t.Fatalf("expected decrypt to succeed: %v", err)
			}
		})
	}
}

// An unrelated SSH key must still be refused.
func TestDecryptVaultRejectsWrongSSHIdentity(t *testing.T) {
	_, sshPub := newSSHKey(t)
	otherID, _ := newSSHKey(t)

	encrypted, err := EncryptVault(vaultFixture(), []string{sshPub})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptVault(encrypted, otherID); err == nil {
		t.Fatal("a non-recipient SSH key must not decrypt the vault")
	}
}

func TestDecryptVaultWithoutIdentitiesIsAnError(t *testing.T) {
	_, sshPub := newSSHKey(t)
	encrypted, err := EncryptVault(vaultFixture(), []string{sshPub})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptVault(encrypted); err == nil {
		t.Fatal("expected an error when no identities are supplied")
	}
}

// Re-encrypting must work when the only usable identity is an SSH key.
func TestReencryptVaultWithSSHIdentity(t *testing.T) {
	sshID, sshPub := newSSHKey(t)
	ageID, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}

	encrypted, err := EncryptVault(vaultFixture(), []string{sshPub})
	if err != nil {
		t.Fatal(err)
	}

	reencrypted, err := ReencryptVault(encrypted, []string{sshPub, ageID.Recipient().String()}, sshID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptVault(reencrypted, ageID); err != nil {
		t.Fatalf("age key should decrypt after being added as a recipient: %v", err)
	}
}
