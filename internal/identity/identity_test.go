package identity

import (
	"os"
	"testing"
)

func TestGenerateKeypair(t *testing.T) {
	id, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	if id.PublicKey == "" {
		t.Fatal("empty public key")
	}
	if id.PrivateKey == "" {
		t.Fatal("empty private key")
	}
	if id.Recipient == nil {
		t.Fatal("nil recipient")
	}
	if id.AgeIdentity() == nil {
		t.Fatal("nil age identity")
	}
}

func TestLoadFromKey(t *testing.T) {
	// Generate a keypair, then reload from the private key.
	original, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadFromKey(original.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.PublicKey != original.PublicKey {
		t.Fatalf("public key mismatch: %s != %s", loaded.PublicKey, original.PublicKey)
	}
}

func TestLoadFromKeyInvalid(t *testing.T) {
	_, err := LoadFromKey("not-a-valid-key")
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestLoadFromEnvVar(t *testing.T) {
	keypair, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}

	// Set VALET_KEY and verify Load() picks it up.
	os.Setenv("VALET_KEY", keypair.PrivateKey)
	defer os.Unsetenv("VALET_KEY")

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PublicKey != keypair.PublicKey {
		t.Fatal("Load() should use VALET_KEY when set")
	}
}

func TestNewForTesting(t *testing.T) {
	id, err := NewForTesting()
	if err != nil {
		t.Fatal(err)
	}
	if id.AgeIdentity() == nil {
		t.Fatal("test identity should have working age identity")
	}
}

func TestTwoKeypairsAreDifferent(t *testing.T) {
	id1, _ := GenerateKeypair()
	id2, _ := GenerateKeypair()

	if id1.PublicKey == id2.PublicKey {
		t.Fatal("two generated keypairs should be different")
	}
	if id1.PrivateKey == id2.PrivateKey {
		t.Fatal("two generated keypairs should be different")
	}
}
