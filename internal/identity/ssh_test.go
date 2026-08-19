package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// writeSSHKey drops an unencrypted ed25519 private key at ~/.ssh/<name>.
func writeSSHKey(t *testing.T, home, name string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSSHIdentitiesFindsUnencryptedKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeSSHKey(t, home, "id_ed25519")
	writeSSHKey(t, home, "id_work")

	if got := len(sshIdentities()); got != 2 {
		t.Fatalf("expected 2 SSH identities, got %d", got)
	}
}

func TestSSHIdentitiesIgnoresPublicKeysAndNonMatchingNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeSSHKey(t, home, "id_ed25519")
	dir := filepath.Join(home, ".ssh")
	for _, name := range []string{"id_ed25519.pub", "config", "known_hosts", "authorized_keys"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not a private key\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	if got := len(sshIdentities()); got != 1 {
		t.Fatalf("expected only the private key to be picked up, got %d", got)
	}
}

// An unparseable or passphrase-protected key must be skipped, not fatal —
// otherwise one bad key in ~/.ssh would break valet entirely.
func TestSSHIdentitiesSkipsUnparseableKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "id_broken"), []byte("garbage"), 0600); err != nil {
		t.Fatal(err)
	}
	writeSSHKey(t, home, "id_ed25519")

	if got := len(sshIdentities()); got != 1 {
		t.Fatalf("expected the broken key to be skipped, got %d identities", got)
	}
}

func TestSSHIdentitiesWithNoSSHDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := len(sshIdentities()); got != 0 {
		t.Fatalf("expected no identities, got %d", got)
	}
}

func TestLoadAttachesSSHKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VALET_KEY", "")

	if _, err := Init(); err != nil {
		t.Fatal(err)
	}
	writeSSHKey(t, home, "id_ed25519")

	id, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	ids := id.AgeIdentities()
	if len(ids) != 2 {
		t.Fatalf("expected age key + 1 SSH key, got %d", len(ids))
	}
	if ids[0] != id.AgeIdentity() {
		t.Fatal("the age identity should be tried first")
	}
}

// VALET_KEY is the bot path; it should stay a single key with no ~/.ssh scan.
func TestLoadFromKeyHasNoSSHKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSSHKey(t, home, "id_ed25519")

	keypair, err := NewForTesting()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("VALET_KEY", keypair.PrivateKey)

	id, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(id.AgeIdentities()); got != 1 {
		t.Fatalf("VALET_KEY should yield exactly one identity, got %d", got)
	}
}
