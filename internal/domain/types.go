package domain

import "time"

// StoreType defines where secrets live.
type StoreType string

const (
	StoreTypeLocal  StoreType = "local"
	StoreTypeEmbedded StoreType = "embedded"
	StoreTypeGit    StoreType = "git"
)

// StoreMeta is the on-disk store.json.
type StoreMeta struct {
	Version   int       `json:"version"`
	Name      string    `json:"name"`
	Type      StoreType `json:"type"`
	Remote    string    `json:"remote,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// Project is a logical grouping of secrets for a system/app.
type Project struct {
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

// User is a member of a store with a public key for encryption.
type User struct {
	Name      string    `json:"name"`
	GitHub    string    `json:"github,omitempty"`
	PublicKey string    `json:"public_key"`
	CreatedAt time.Time `json:"created_at"`
}

// ManifestRecipient is a recipient entry in a scope manifest.
type ManifestRecipient struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
}

// Manifest is the plaintext metadata for a scope (manifest.json).
type Manifest struct {
	Secrets       []string                   `json:"secrets"`
	Providers     map[string]string          `json:"providers,omitempty"`        // secret name → provider
	RotationFlags map[string]RotationFlag    `json:"rotation_flags,omitempty"`   // secret name → rotation flag
	Recipients    []ManifestRecipient        `json:"recipients"`
	UpdatedAt     time.Time                  `json:"updated_at"`
}

// VaultSecret is a single secret entry inside an encrypted vault.
type VaultSecret struct {
	Value     string    `json:"value"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}

// VaultContent is the decrypted content of a vault.age file.
type VaultContent struct {
	Secrets map[string]VaultSecret   `json:"secrets"`
	History map[string][]VaultSecret `json:"history,omitempty"` // key → previous versions (newest first)
}

// RotationFlag marks a secret as needing rotation (e.g. after user revocation).
type RotationFlag struct {
	FlaggedAt time.Time `json:"flagged_at"`
	Reason    string    `json:"reason"`
}

// Scope represents a scope with its path and manifest data.
type Scope struct {
	Path       string              `json:"path"`
	Secrets    []string            `json:"secrets"`
	Recipients []ManifestRecipient `json:"recipients"`
}

// Requirement declares a secret that a project needs.
type Requirement struct {
	Provider    string `toml:"provider,omitempty"`
	Description string `toml:"description,omitempty"`
	Optional    bool   `toml:"optional,omitempty"`
	Scope       string `toml:"scope,omitempty"` // default scope for this secret (e.g. "runtime", "db")
}

// KeyMapping maps a local key name to a remote key name in a linked store.
// When unmarshalled from TOML, a plain string becomes Local=Remote=string.
type KeyMapping struct {
	Local  string `toml:"local"`
	Remote string `toml:"remote"`
}

// EnvMapping maps a local environment name to a remote environment name.
type EnvMapping struct {
	Local  string `toml:"local"`
	Remote string `toml:"remote"`
}

// StoreLink declares a linked store with optional key filtering, name mapping,
// and environment mapping. The simplest form is just name + url; everything
// else is additive complexity.
type StoreLink struct {
	Name         string       `toml:"name"`
	URL          string       `toml:"url,omitempty"`          // git URL for git-backed stores
	RawKeys      []any        `toml:"keys,omitempty"`         // raw TOML: strings or {local, remote} maps
	Environments []EnvMapping `toml:"environments,omitempty"` // only needed when env names differ
}

// ParsedKeys returns the key mappings for this link. Plain strings become
// identity mappings (local == remote). If no keys are specified, all keys
// from the store are available.
func (sl *StoreLink) ParsedKeys() []KeyMapping {
	if len(sl.RawKeys) == 0 {
		return nil // all keys
	}
	var result []KeyMapping
	for _, raw := range sl.RawKeys {
		switch v := raw.(type) {
		case string:
			result = append(result, KeyMapping{Local: v, Remote: v})
		case map[string]any:
			km := KeyMapping{}
			if l, ok := v["local"].(string); ok {
				km.Local = l
			}
			if r, ok := v["remote"].(string); ok {
				km.Remote = r
			}
			if km.Local == "" || km.Remote == "" {
				continue // skip malformed entries
			}
			result = append(result, km)
		}
	}
	return result
}

// ResolveEnv maps a local environment name to the remote environment name
// for this store link. Unmapped environments match by name.
func (sl *StoreLink) ResolveEnv(localEnv string) string {
	for _, em := range sl.Environments {
		if em.Local == localEnv {
			return em.Remote
		}
	}
	return localEnv // default: same name
}

// ValetConfig is the project-level .valet.toml configuration (committed to git).
type ValetConfig struct {
	Store      string                 `toml:"store"`
	Project    string                 `toml:"project"`
	DefaultEnv string                 `toml:"default_env"`
	Stores     []StoreLink            `toml:"stores,omitempty"`  // shared store links
	Requires   map[string]Requirement `toml:"requires,omitempty"`
}

// LocalConfig is the per-developer .valet.local.toml (gitignored).
type LocalConfig struct {
	Stores []StoreLink `toml:"stores,omitempty"` // personal store links
}

// Invite is a pending invitation stored in .valet/invites/.
// The temp public key is added as a recipient; the temp private key
// is shared with the invitee out-of-band.
type Invite struct {
	ID           string    `json:"id"`
	TempPubKey   string    `json:"temp_public_key"`
	Environments []string  `json:"environments"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	MaxUses      int       `json:"max_uses"`
	Uses         int       `json:"uses"`
}
