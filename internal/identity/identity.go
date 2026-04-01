package identity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

// Identity holds a local user's age keypair.
type Identity struct {
	Name       string
	PublicKey  string
	PrivateKey string
	Recipient  age.Recipient
	identity   age.Identity
}

func dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".valet", "identity"), nil
}

// Init generates a new age X25519 keypair and saves it to ~/.valet/identity/.
func Init() (*Identity, error) {
	d, err := dir()
	if err != nil {
		return nil, err
	}

	keyPath := filepath.Join(d, "key.txt")
	if _, err := os.Stat(keyPath); err == nil {
		return nil, fmt.Errorf("identity already exists at %s", keyPath)
	}

	k, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("generating keypair: %w", err)
	}

	if err := os.MkdirAll(d, 0700); err != nil {
		return nil, err
	}

	content := fmt.Sprintf("# created by valet\n# public key: %s\n%s\n", k.Recipient().String(), k.String())
	if err := os.WriteFile(keyPath, []byte(content), 0600); err != nil {
		return nil, err
	}

	pubPath := filepath.Join(d, "key.pub")
	if err := os.WriteFile(pubPath, []byte(k.Recipient().String()+"\n"), 0644); err != nil {
		return nil, err
	}

	return &Identity{
		PublicKey:  k.Recipient().String(),
		PrivateKey: k.String(),
		Recipient:  k.Recipient(),
		identity:   k,
	}, nil
}

// LoadFromKey parses an age private key string into an Identity.
// Used for VALET_KEY env var and bot key generation.
func LoadFromKey(privKey string) (*Identity, error) {
	privKey = strings.TrimSpace(privKey)
	k, err := age.ParseX25519Identity(privKey)
	if err != nil {
		return nil, fmt.Errorf("parsing key: %w", err)
	}
	return &Identity{
		Name:       "env",
		PublicKey:  k.Recipient().String(),
		PrivateKey: privKey,
		Recipient:  k.Recipient(),
		identity:   k,
	}, nil
}

// GenerateKeypair creates a new age keypair and returns the Identity
// without writing anything to disk. Used for bot creation.
func GenerateKeypair() (*Identity, error) {
	k, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("generating keypair: %w", err)
	}
	return &Identity{
		PublicKey:  k.Recipient().String(),
		PrivateKey: k.String(),
		Recipient:  k.Recipient(),
		identity:   k,
	}, nil
}

// Load reads an existing identity. Checks VALET_KEY env var first,
// then falls back to ~/.valet/identity/.
func Load() (*Identity, error) {
	// Check env var first.
	if envKey := os.Getenv("VALET_KEY"); envKey != "" {
		return LoadFromKey(envKey)
	}

	d, err := dir()
	if err != nil {
		return nil, err
	}

	keyPath := filepath.Join(d, "key.txt")
	data, err := os.ReadFile(keyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no identity found — run 'valet identity init' first")
		}
		return nil, err
	}

	var privKey string
	var pubKey string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# public key: ") {
			pubKey = strings.TrimPrefix(line, "# public key: ")
		}
		if strings.HasPrefix(line, "AGE-SECRET-KEY-") {
			privKey = line
		}
	}

	if privKey == "" {
		return nil, fmt.Errorf("no private key found in %s", keyPath)
	}

	k, err := age.ParseX25519Identity(privKey)
	if err != nil {
		return nil, fmt.Errorf("parsing identity: %w", err)
	}

	if pubKey == "" {
		pubKey = k.Recipient().String()
	}

	return &Identity{
		PublicKey:  pubKey,
		PrivateKey: privKey,
		Recipient:  k.Recipient(),
		identity:   k,
	}, nil
}

// LoadOrInit loads an existing identity or creates a new one.
func LoadOrInit() (*Identity, error) {
	id, err := Load()
	if err == nil {
		return id, nil
	}
	return Init()
}

// Export returns the public key as a string for sharing.
func (id *Identity) Export() string {
	return id.PublicKey
}

// AgeIdentity returns the underlying age.Identity for decryption.
func (id *Identity) AgeIdentity() age.Identity {
	return id.identity
}
