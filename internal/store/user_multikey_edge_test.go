package store

import (
	"testing"

	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/identity"
)

func TestAddUserWithKeys_EmptyKeys(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.NewForTesting()
	s, _ := Create(dir, "test", domain.StoreTypeLocal, id)

	_, err := s.AddUserWithKeys("alice", "", []domain.UserKey{})
	if err == nil {
		t.Error("expected error for empty keys")
	}

	_, err = s.AddUserWithKeys("alice", "", []domain.UserKey{{Key: "  "}})
	if err == nil {
		t.Error("expected error for whitespace-only key")
	}
}

func TestAddUserWithKeys_Duplicate(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.NewForTesting()
	s, _ := Create(dir, "test", domain.StoreTypeLocal, id)

	s.AddUserWithKeys("alice", "", []domain.UserKey{{Key: "age1abc"}})
	_, err := s.AddUserWithKeys("alice", "", []domain.UserKey{{Key: "age1xyz"}})
	if err == nil {
		t.Error("expected error for duplicate user name")
	}
}

func TestRemoveUserKey_NotFound(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.NewForTesting()
	s, _ := Create(dir, "test", domain.StoreTypeLocal, id)

	s.AddUser("bob", "", "age1abc")
	_, err := s.RemoveUserKey("bob", "nonexistent-key")
	if err == nil {
		t.Error("expected error for non-existent key")
	}
}

func TestRemoveUserKey_UserNotFound(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.NewForTesting()
	s, _ := Create(dir, "test", domain.StoreTypeLocal, id)

	_, err := s.RemoveUserKey("nonexistent", "key")
	if err == nil {
		t.Error("expected error for non-existent user")
	}
}

func TestAddUserKey_UserNotFound(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.NewForTesting()
	s, _ := Create(dir, "test", domain.StoreTypeLocal, id)

	err := s.AddUserKey("nonexistent", "age1abc", "", "manual")
	if err == nil {
		t.Error("expected error for non-existent user")
	}
}

func TestSyncUserKeys_AllRemoved(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.NewForTesting()
	s, _ := Create(dir, "test", domain.StoreTypeLocal, id)

	s.AddUser("alice", "", "age1abc")

	// Sync to empty → user file still updated (but keys can be empty
	// since the user still exists, just no keys).
	_, _, _, err := s.SyncUserKeys("alice", nil)
	// This should work — writeUser is called even with no key diff.
	if err != nil {
		t.Fatal(err)
	}

	u, _ := s.GetUser("alice")
	if len(u.AllKeys()) != 0 {
		t.Errorf("expected 0 keys after sync to empty, got %d", len(u.AllKeys()))
	}
}

func TestSyncUserKeys_UserNotFound(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.NewForTesting()
	s, _ := Create(dir, "test", domain.StoreTypeLocal, id)

	_, _, _, err := s.SyncUserKeys("nonexistent", nil)
	if err == nil {
		t.Error("expected error for non-existent user")
	}
}

func TestUpdateUser_NotFound(t *testing.T) {
	dir := t.TempDir()
	id, _ := identity.NewForTesting()
	s, _ := Create(dir, "test", domain.StoreTypeLocal, id)

	err := s.UpdateUser("nonexistent", map[string]string{"github": "x"})
	if err == nil {
		t.Error("expected error for non-existent user")
	}
}

func TestUser_BackwardCompat_PublicKey(t *testing.T) {
	// Simulate reading an old user file with only PublicKey set.
	u := &domain.User{
		Name:      "legacy",
		PublicKey: "age1oldkey",
	}

	if len(u.AllKeys()) != 1 {
		t.Errorf("expected 1 key, got %d", len(u.AllKeys()))
	}
	if !u.HasKey("age1oldkey") {
		t.Error("expected to find legacy key")
	}
	if u.PrimaryKey() != "age1oldkey" {
		t.Errorf("primary key should be age1oldkey, got %q", u.PrimaryKey())
	}

	// AllUserKeys should migrate with inferred source.
	uks := u.AllUserKeys()
	if len(uks) != 1 || uks[0].Source != "age-identity" {
		t.Errorf("expected age-identity source, got %v", uks)
	}
}

func TestUser_BackwardCompat_PublicKeys(t *testing.T) {
	// Middle format: PublicKeys []string.
	u := &domain.User{
		Name:       "mid-format",
		PublicKeys: []string{"ssh-ed25519 AAA", "age1bbb"},
	}

	if len(u.AllKeys()) != 2 {
		t.Errorf("expected 2 keys, got %d", len(u.AllKeys()))
	}

	uks := u.AllUserKeys()
	if uks[0].Source != "ssh" {
		t.Errorf("expected ssh source, got %q", uks[0].Source)
	}
	if uks[1].Source != "age-identity" {
		t.Errorf("expected age-identity source, got %q", uks[1].Source)
	}
}
