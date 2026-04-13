package domain

import "testing"

func TestUser_AllKeys_BackwardCompat(t *testing.T) {
	// Old format: single PublicKey.
	u := User{PublicKey: "age1abc"}
	keys := u.AllKeys()
	if len(keys) != 1 || keys[0] != "age1abc" {
		t.Errorf("expected [age1abc], got %v", keys)
	}
}

func TestUser_AllKeys_NewFormat(t *testing.T) {
	u := User{Keys: []UserKey{{Key: "ssh-ed25519 A"}, {Key: "ssh-ed25519 B"}}}
	keys := u.AllKeys()
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

func TestUser_AllKeys_MigratesPublicKeys(t *testing.T) {
	// Middle format: PublicKeys []string.
	u := User{PublicKeys: []string{"key1", "key2"}}
	keys := u.AllKeys()
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

func TestUser_HasKey(t *testing.T) {
	u := User{Keys: []UserKey{{Key: "age1abc"}, {Key: "ssh-ed25519 XYZ"}}}
	if !u.HasKey("age1abc") {
		t.Error("expected HasKey(age1abc) = true")
	}
	if !u.HasKey("ssh-ed25519 XYZ") {
		t.Error("expected HasKey(ssh-ed25519 XYZ) = true")
	}
	if u.HasKey("nonexistent") {
		t.Error("expected HasKey(nonexistent) = false")
	}
}

func TestUser_PrimaryKey(t *testing.T) {
	u := User{Keys: []UserKey{{Key: "first"}, {Key: "second"}}}
	if u.PrimaryKey() != "first" {
		t.Errorf("expected 'first', got %q", u.PrimaryKey())
	}

	empty := User{}
	if empty.PrimaryKey() != "" {
		t.Error("expected empty primary key")
	}
}

func TestUser_AllUserKeys_InfersSource(t *testing.T) {
	u := User{Keys: []UserKey{
		{Key: "age1abc"},
		{Key: "ssh-ed25519 XYZ"},
		{Key: "something-else"},
	}}
	uks := u.AllUserKeys()

	if uks[0].Source != "age-identity" {
		t.Errorf("expected age-identity, got %q", uks[0].Source)
	}
	if uks[1].Source != "ssh" {
		t.Errorf("expected ssh, got %q", uks[1].Source)
	}
	if uks[2].Source != "manual" {
		t.Errorf("expected manual, got %q", uks[2].Source)
	}
}

func TestStoreLink_ParsedKeys(t *testing.T) {
	sl := StoreLink{Name: "test"}
	if sl.ParsedKeys() != nil {
		t.Error("no keys should return nil")
	}

	sl.RawKeys = []any{"KEY1", "KEY2"}
	keys := sl.ParsedKeys()
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
	if keys[0].Local != "KEY1" || keys[0].Remote != "KEY1" {
		t.Error("plain string should be identity mapping")
	}
}

func TestStoreLink_ResolveEnv(t *testing.T) {
	sl := StoreLink{
		Name:         "test",
		Environments: []EnvMapping{{Local: "dev", Remote: "staging"}},
	}
	if sl.ResolveEnv("dev") != "staging" {
		t.Error("expected dev → staging")
	}
	if sl.ResolveEnv("prod") != "prod" {
		t.Error("unmapped env should pass through")
	}
}

func TestInferKeySource(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"age1abc", "age-identity"},
		{"ssh-ed25519 AAAA", "ssh"},
		{"ssh-rsa AAAA", "ssh"},
		{"ecdsa-sha2-nistp256 AAAA", "ssh"},
		{"something-else", "manual"},
		{"", "manual"},
	}
	for _, tt := range tests {
		got := inferKeySource(tt.key)
		if got != tt.want {
			t.Errorf("inferKeySource(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}
