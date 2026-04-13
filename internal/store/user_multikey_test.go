package store

import (
	"testing"

	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/identity"
)

func setupMultikeyTestStore(t *testing.T) (*Store, *identity.Identity) {
	t.Helper()
	dir := t.TempDir()
	id, err := identity.NewForTesting()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Create(dir, "test", domain.StoreTypeLocal, id)
	if err != nil {
		t.Fatal(err)
	}
	return s, id
}

func TestAddUserWithKeys_Multiple(t *testing.T) {
	s, _ := setupMultikeyTestStore(t)

	keys := []domain.UserKey{
		{Key: "ssh-ed25519 AAAA1", Label: "laptop", Source: "github"},
		{Key: "ssh-ed25519 AAAA2", Label: "desktop", Source: "github"},
	}
	u, err := s.AddUserWithKeys("alice", "alice-gh", keys)
	if err != nil {
		t.Fatal(err)
	}

	if len(u.AllKeys()) != 2 {
		t.Errorf("expected 2 keys, got %d", len(u.AllKeys()))
	}
	if !u.HasKey("ssh-ed25519 AAAA1") || !u.HasKey("ssh-ed25519 AAAA2") {
		t.Error("expected both keys to be present")
	}
	if u.PrimaryKey() != "ssh-ed25519 AAAA1" {
		t.Errorf("expected first key as primary, got %q", u.PrimaryKey())
	}
}

func TestAddUserKey(t *testing.T) {
	s, _ := setupMultikeyTestStore(t)

	s.AddUser("bob", "", "age1abc")

	err := s.AddUserKey("bob", "ssh-ed25519 BBBB", "work laptop", "manual")
	if err != nil {
		t.Fatal(err)
	}

	u, _ := s.GetUser("bob")
	if len(u.AllKeys()) != 2 {
		t.Errorf("expected 2 keys, got %d", len(u.AllKeys()))
	}
	if !u.HasKey("age1abc") || !u.HasKey("ssh-ed25519 BBBB") {
		t.Error("expected both keys")
	}
}

func TestAddUserKey_Duplicate(t *testing.T) {
	s, _ := setupMultikeyTestStore(t)

	s.AddUser("bob", "", "age1abc")
	err := s.AddUserKey("bob", "age1abc", "", "manual")
	if err != nil {
		t.Fatal("duplicate key should be a no-op, not error")
	}

	u, _ := s.GetUser("bob")
	if len(u.AllKeys()) != 1 {
		t.Errorf("expected 1 key (no duplicate), got %d", len(u.AllKeys()))
	}
}

func TestRemoveUserKey(t *testing.T) {
	s, _ := setupMultikeyTestStore(t)

	keys := []domain.UserKey{
		{Key: "ssh-ed25519 AAAA1"},
		{Key: "ssh-ed25519 AAAA2"},
	}
	s.AddUserWithKeys("alice", "", keys)

	// Just test that the user file is updated correctly.
	// Re-encryption requires valid age keys, which is tested in store_test.go.
	u, _ := s.GetUser("alice")
	if len(u.AllKeys()) != 2 {
		t.Fatalf("setup: expected 2 keys, got %d", len(u.AllKeys()))
	}

	_, err := s.RemoveUserKey("alice", "ssh-ed25519 AAAA1")
	if err != nil {
		t.Fatal(err)
	}

	u, _ = s.GetUser("alice")
	if len(u.AllKeys()) != 1 {
		t.Errorf("expected 1 key after removal, got %d", len(u.AllKeys()))
	}
	if u.HasKey("ssh-ed25519 AAAA1") {
		t.Error("removed key should not be present")
	}
}

func TestRemoveUserKey_LastKey(t *testing.T) {
	s, _ := setupMultikeyTestStore(t)

	s.AddUser("bob", "", "age1abc")

	_, err := s.RemoveUserKey("bob", "age1abc")
	if err == nil {
		t.Error("expected error when removing last key")
	}
}

func TestSyncUserKeys(t *testing.T) {
	s, _ := setupMultikeyTestStore(t)

	initial := []domain.UserKey{
		{Key: "ssh-ed25519 OLD1", Label: "old laptop", Source: "github"},
		{Key: "ssh-ed25519 KEEP1", Source: "age-identity"},
	}
	s.AddUserWithKeys("alice", "alice-gh", initial)

	// Sync: remove OLD1, add NEW1, keep KEEP1.
	newKeys := []domain.UserKey{
		{Key: "ssh-ed25519 KEEP1", Source: "age-identity"},
		{Key: "ssh-ed25519 NEW1", Label: "new laptop", Source: "github"},
	}

	added, removed, _, err := s.SyncUserKeys("alice", newKeys)
	if err != nil {
		t.Fatal(err)
	}

	if added != 1 {
		t.Errorf("expected 1 added, got %d", added)
	}
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}

	u, _ := s.GetUser("alice")
	if u.HasKey("ssh-ed25519 OLD1") {
		t.Error("OLD1 should be gone")
	}
	if !u.HasKey("ssh-ed25519 NEW1") {
		t.Error("NEW1 should be present")
	}
	if !u.HasKey("ssh-ed25519 KEEP1") {
		t.Error("KEEP1 should still be present")
	}
}

func TestSyncUserKeys_NoChange(t *testing.T) {
	s, _ := setupMultikeyTestStore(t)

	keys := []domain.UserKey{{Key: "age1abc"}}
	s.AddUserWithKeys("bob", "", keys)

	added, removed, _, err := s.SyncUserKeys("bob", keys)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || removed != 0 {
		t.Errorf("expected no changes, got added=%d removed=%d", added, removed)
	}
}

func TestUpdateUser(t *testing.T) {
	s, _ := setupMultikeyTestStore(t)

	s.AddUser("alice", "", "age1abc")

	err := s.UpdateUser("alice", map[string]string{"github": "alice-gh"})
	if err != nil {
		t.Fatal(err)
	}

	u, _ := s.GetUser("alice")
	if u.GitHub != "alice-gh" {
		t.Errorf("expected github 'alice-gh', got %q", u.GitHub)
	}
}
