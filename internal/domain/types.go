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

// ValetConfig is the project-level .valet.toml configuration (committed to git).
type ValetConfig struct {
	Store      string                 `toml:"store"`
	Project    string                 `toml:"project"`
	DefaultEnv string                 `toml:"default_env"`
	Stores     []string               `toml:"stores,omitempty"` // shared store links (names or remote URLs)
	Requires   map[string]Requirement `toml:"requires,omitempty"`
}

// LocalConfig is the per-developer .valet.local.toml (gitignored).
type LocalConfig struct {
	Stores []string `toml:"stores,omitempty"` // personal store links (names or remote URLs)
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
