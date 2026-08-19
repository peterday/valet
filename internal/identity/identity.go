package identity

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"filippo.io/age"
	"filippo.io/age/agessh"
)

// Identity holds a local user's age keypair, plus any SSH keys found on
// this machine that can also decrypt.
//
// PublicKey and Recipient always refer to the age keypair — that is the
// identity valet publishes when adding you to a store. The SSH keys are
// decrypt-only: valet already accepts SSH public keys as recipients (see
// crypto.ParseRecipients), typically harvested from GitHub, so a vault is
// often encrypted to a key this machine holds even when the age key is not
// a recipient. Without these, that vault would be unopenable here.
type Identity struct {
	Name       string
	PublicKey  string
	PrivateKey string
	Recipient  age.Recipient
	identity   age.Identity
	sshKeys    []age.Identity
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

// sshIdentities returns age identities for the usable SSH private keys in
// ~/.ssh. Keys that cannot be parsed are skipped rather than failing the
// load: an unreadable or passphrase-protected key in ~/.ssh should never
// stop valet from using the age identity it already has.
func sshIdentities() []age.Identity {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	matches, err := filepath.Glob(filepath.Join(home, ".ssh", "id_*"))
	if err != nil {
		return nil
	}
	sort.Strings(matches)

	var ids []age.Identity
	for _, path := range matches {
		if strings.HasSuffix(path, ".pub") {
			continue
		}
		pem, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		// Encrypted keys return an error here. Handling them would need a
		// passphrase prompt, which would break non-interactive `valet drive`.
		id, err := agessh.ParseIdentity(pem)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids
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
		sshKeys:    sshIdentities(),
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

// AgeIdentity returns the primary age identity.
//
// Deprecated: use AgeIdentities, which also returns any SSH keys that can
// decrypt. Decrypting with only this key fails on vaults encrypted to an
// SSH recipient.
func (id *Identity) AgeIdentity() age.Identity {
	return id.identity
}

// AgeIdentities returns every identity that may decrypt a vault: the age
// keypair first, then any usable SSH keys from ~/.ssh. age tries each in
// turn, so ordering only affects which is attempted first.
func (id *Identity) AgeIdentities() []age.Identity {
	ids := make([]age.Identity, 0, 1+len(id.sshKeys))
	if id.identity != nil {
		ids = append(ids, id.identity)
	}
	return append(ids, id.sshKeys...)
}
