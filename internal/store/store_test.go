package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/superset-studio/valet/internal/domain"
	"github.com/superset-studio/valet/internal/identity"
)

func testIdentity(t *testing.T) *identity.Identity {
	t.Helper()
	id, err := identity.NewForTesting()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testIdentityFromKey(t *testing.T, k *age.X25519Identity) *identity.Identity {
	t.Helper()
	return identity.NewForTestingFromKey(k)
}

// setupTestStore creates a temp store for testing.
func setupTestStore(t *testing.T) (*Store, *identity.Identity) {
	t.Helper()
	dir := t.TempDir()
	id := testIdentity(t)

	s, err := Create(filepath.Join(dir, ".valet"), "test-store", domain.StoreTypeEmbedded, id)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.AddUser("me", "", id.PublicKey); err != nil {
		t.Fatal(err)
	}

	return s, id
}

// === Store lifecycle ===

func TestCreateAndOpenStore(t *testing.T) {
	dir := t.TempDir()
	id := testIdentity(t)

	s, err := Create(filepath.Join(dir, ".valet"), "my-store", domain.StoreTypeEmbedded, id)
	if err != nil {
		t.Fatal(err)
	}

	if s.Meta.Name != "my-store" {
		t.Fatalf("expected name 'my-store', got %q", s.Meta.Name)
	}
	if s.Meta.Type != domain.StoreTypeEmbedded {
		t.Fatalf("expected type in-repo, got %q", s.Meta.Type)
	}
	if s.Meta.Version != 1 {
		t.Fatalf("expected version 1, got %d", s.Meta.Version)
	}

	// Re-open
	s2, err := Open(filepath.Join(dir, ".valet"), id)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Meta.Name != "my-store" {
		t.Fatal("metadata not persisted")
	}
}

func TestOpenNonexistentStore(t *testing.T) {
	id := testIdentity(t)
	_, err := Open("/nonexistent/path", id)
	if err == nil {
		t.Fatal("expected error opening nonexistent store")
	}
}

// === User management ===

func TestUserCRUD(t *testing.T) {
	s, id := setupTestStore(t)

	// User "me" was added in setup
	users, err := s.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].Name != "me" {
		t.Fatalf("expected user 'me', got %q", users[0].Name)
	}

	// Add another user
	k2, _ := age.GenerateX25519Identity()
	_, err = s.AddUser("alice", "alice-gh", k2.Recipient().String())
	if err != nil {
		t.Fatal(err)
	}

	users, _ = s.ListUsers()
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}

	// Get user
	alice, err := s.GetUser("alice")
	if err != nil {
		t.Fatal(err)
	}
	if alice.GitHub != "alice-gh" {
		t.Fatalf("expected github 'alice-gh', got %q", alice.GitHub)
	}

	// Duplicate
	_, err = s.AddUser("alice", "", id.PublicKey)
	if err == nil {
		t.Fatal("expected error adding duplicate user")
	}

	// Remove
	if err := s.RemoveUser("alice"); err != nil {
		t.Fatal(err)
	}
	users, _ = s.ListUsers()
	if len(users) != 1 {
		t.Fatalf("expected 1 user after remove, got %d", len(users))
	}

	// Remove nonexistent
	if err := s.RemoveUser("bob"); err == nil {
		t.Fatal("expected error removing nonexistent user")
	}
}

// === Project management ===

func TestProjectCRUD(t *testing.T) {
	s, _ := setupTestStore(t)

	p, err := s.CreateProject("my-app")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "my-app" {
		t.Fatalf("expected name 'my-app', got %q", p.Name)
	}

	// Get
	got, err := s.GetProject("my-app")
	if err != nil {
		t.Fatal(err)
	}
	if got.Slug != "my-app" {
		t.Fatal("slug mismatch")
	}

	// List
	projects, err := s.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}

	// Duplicate
	_, err = s.CreateProject("my-app")
	if err == nil {
		t.Fatal("expected error creating duplicate project")
	}

	// Nonexistent
	_, err = s.GetProject("nope")
	if err == nil {
		t.Fatal("expected error getting nonexistent project")
	}
}

// === Environment management ===

func TestEnvironmentCRUD(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")

	if err := s.CreateEnvironment("app", "dev"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEnvironment("app", "prod"); err != nil {
		t.Fatal(err)
	}

	envs, err := s.ListEnvironments("app")
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 2 {
		t.Fatalf("expected 2 envs, got %d", len(envs))
	}

	// Duplicate
	if err := s.CreateEnvironment("app", "dev"); err == nil {
		t.Fatal("expected error creating duplicate env")
	}
}

// === Scope management ===

func TestScopeCreateAndList(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")

	if err := s.CreateScope("app", "dev/runtime"); err != nil {
		t.Fatal(err)
	}

	scopes, err := s.ListScopes("app", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 1 {
		t.Fatalf("expected 1 scope, got %d", len(scopes))
	}
	if scopes[0].Path != "dev/runtime" {
		t.Fatalf("expected path 'dev/runtime', got %q", scopes[0].Path)
	}

	// Scope should have the creator as recipient
	if len(scopes[0].Recipients) != 1 {
		t.Fatalf("expected 1 recipient, got %d", len(scopes[0].Recipients))
	}

	// Duplicate
	if err := s.CreateScope("app", "dev/runtime"); err == nil {
		t.Fatal("expected error creating duplicate scope")
	}

	// Multiple scopes in same env
	if err := s.CreateScope("app", "dev/db"); err != nil {
		t.Fatal(err)
	}
	scopes, _ = s.ListScopes("app", "dev")
	if len(scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d", len(scopes))
	}
}

func TestListAllScopes(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateEnvironment("app", "prod")
	s.CreateScope("app", "dev/runtime")
	s.CreateScope("app", "prod/runtime")

	all, err := s.ListAllScopes("app")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 total scopes, got %d", len(all))
	}
}

// === Secret operations ===

func TestSecretSetAndGet(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")

	if err := s.SetSecret("app", "dev/runtime", "API_KEY", "sk-test-123"); err != nil {
		t.Fatal(err)
	}

	secret, err := s.GetSecret("app", "dev/runtime", "API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "sk-test-123" {
		t.Fatalf("expected 'sk-test-123', got %q", secret.Value)
	}
	if secret.Version != 1 {
		t.Fatalf("expected version 1, got %d", secret.Version)
	}
}

func TestSecretUpdate(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")

	s.SetSecret("app", "dev/runtime", "KEY", "v1")
	s.SetSecret("app", "dev/runtime", "KEY", "v2")

	secret, _ := s.GetSecret("app", "dev/runtime", "KEY")
	if secret.Value != "v2" {
		t.Fatalf("expected 'v2', got %q", secret.Value)
	}
	if secret.Version != 2 {
		t.Fatalf("expected version 2, got %d", secret.Version)
	}
}

func TestSecretGetNonexistent(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")

	_, err := s.GetSecret("app", "dev/runtime", "NOPE")
	if err == nil {
		t.Fatal("expected error for nonexistent secret")
	}
}

func TestSecretRemove(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")

	s.SetSecret("app", "dev/runtime", "KEY1", "val1")
	s.SetSecret("app", "dev/runtime", "KEY2", "val2")

	if err := s.RemoveSecret("app", "dev/runtime", "KEY1"); err != nil {
		t.Fatal(err)
	}

	_, err := s.GetSecret("app", "dev/runtime", "KEY1")
	if err == nil {
		t.Fatal("expected error for removed secret")
	}

	// KEY2 should still exist
	secret, err := s.GetSecret("app", "dev/runtime", "KEY2")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "val2" {
		t.Fatal("KEY2 should be unaffected")
	}

	// Remove nonexistent
	if err := s.RemoveSecret("app", "dev/runtime", "NOPE"); err == nil {
		t.Fatal("expected error removing nonexistent secret")
	}
}

func TestMultipleSecretsInScope(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")

	secrets := map[string]string{
		"OPENAI_API_KEY":    "sk-abc123",
		"DATABASE_URL":      "postgres://localhost/db",
		"REDIS_URL":         "redis://localhost:6379",
		"SECRET_KEY":        "super-secret-key",
		"STRIPE_SECRET_KEY": "sk_test_xxx",
	}

	for k, v := range secrets {
		if err := s.SetSecret("app", "dev/runtime", k, v); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
	}

	for k, expected := range secrets {
		got, err := s.GetSecret("app", "dev/runtime", k)
		if err != nil {
			t.Fatalf("get %s: %v", k, err)
		}
		if got.Value != expected {
			t.Fatalf("%s: expected %q, got %q", k, expected, got.Value)
		}
	}
}

func TestGetSecretFromEnv(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")
	s.CreateScope("app", "dev/db")

	s.SetSecret("app", "dev/runtime", "API_KEY", "sk-123")
	s.SetSecret("app", "dev/db", "DATABASE_URL", "postgres://localhost")

	// Find API_KEY across scopes
	secret, scopePath, err := s.GetSecretFromEnv("app", "dev", "API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "sk-123" {
		t.Fatal("wrong value")
	}
	if scopePath != "dev/runtime" {
		t.Fatalf("expected scope 'dev/runtime', got %q", scopePath)
	}

	// Find DATABASE_URL
	secret, scopePath, err = s.GetSecretFromEnv("app", "dev", "DATABASE_URL")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "postgres://localhost" {
		t.Fatal("wrong value")
	}
	if scopePath != "dev/db" {
		t.Fatalf("expected scope 'dev/db', got %q", scopePath)
	}

	// Nonexistent
	_, _, err = s.GetSecretFromEnv("app", "dev", "NOPE")
	if err == nil {
		t.Fatal("expected error for nonexistent secret")
	}
}

func TestGetAllSecretsInEnv(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")
	s.CreateScope("app", "dev/db")

	s.SetSecret("app", "dev/runtime", "API_KEY", "sk-123")
	s.SetSecret("app", "dev/runtime", "SECRET", "shhh")
	s.SetSecret("app", "dev/db", "DATABASE_URL", "postgres://localhost")

	all, err := s.GetAllSecretsInEnv("app", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 secrets, got %d", len(all))
	}
	if all["API_KEY"] != "sk-123" {
		t.Fatal("API_KEY mismatch")
	}
	if all["DATABASE_URL"] != "postgres://localhost" {
		t.Fatal("DATABASE_URL mismatch")
	}
}

func TestListSecretsInEnv(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")
	s.CreateScope("app", "dev/db")

	s.SetSecret("app", "dev/runtime", "API_KEY", "val")
	s.SetSecret("app", "dev/db", "DB_URL", "val")

	secretMap, err := s.ListSecretsInEnv("app", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(secretMap) != 2 {
		t.Fatalf("expected 2, got %d", len(secretMap))
	}
	if secretMap["API_KEY"] != "dev/runtime" {
		t.Fatalf("API_KEY scope: expected 'dev/runtime', got %q", secretMap["API_KEY"])
	}
	if secretMap["DB_URL"] != "dev/db" {
		t.Fatalf("DB_URL scope: expected 'dev/db', got %q", secretMap["DB_URL"])
	}
}

// === Recipient management ===

func TestAddAndRemoveRecipient(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")

	// Set a secret before adding recipient
	s.SetSecret("app", "dev/runtime", "KEY", "secret-value")

	// Add a second user
	k2, _ := age.GenerateX25519Identity()
	s.AddUser("alice", "", k2.Recipient().String())

	// Add alice as recipient
	if err := s.AddRecipient("app", "dev/runtime", "alice"); err != nil {
		t.Fatal(err)
	}

	// Verify alice is listed
	scopes, _ := s.ListScopes("app", "dev")
	if len(scopes[0].Recipients) != 2 {
		t.Fatalf("expected 2 recipients, got %d", len(scopes[0].Recipients))
	}

	// Duplicate add
	if err := s.AddRecipient("app", "dev/runtime", "alice"); err == nil {
		t.Fatal("expected error adding duplicate recipient")
	}

	// Alice should be able to decrypt (create a store from alice's perspective)
	id2 := testIdentityFromKey(t, k2)
	s2, err := Open(s.Root, id2)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := s2.GetSecret("app", "dev/runtime", "KEY")
	if err != nil {
		t.Fatal("alice should be able to decrypt:", err)
	}
	if secret.Value != "secret-value" {
		t.Fatal("alice got wrong value")
	}

	// Remove alice
	if err := s.RemoveRecipient("app", "dev/runtime", "alice"); err != nil {
		t.Fatal(err)
	}
	scopes, _ = s.ListScopes("app", "dev")
	if len(scopes[0].Recipients) != 1 {
		t.Fatalf("expected 1 recipient after remove, got %d", len(scopes[0].Recipients))
	}

	// Cannot remove last recipient
	if err := s.RemoveRecipient("app", "dev/runtime", "me"); err == nil {
		t.Fatal("expected error removing last recipient")
	}
}

// === Environment grant/revoke ===

func TestGrantAndRevokeEnvironment(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")
	s.CreateScope("app", "dev/db")

	s.SetSecret("app", "dev/runtime", "API_KEY", "val")
	s.SetSecret("app", "dev/db", "DB_URL", "val")

	k2, _ := age.GenerateX25519Identity()
	s.AddUser("alice", "", k2.Recipient().String())

	// Grant
	count, err := s.GrantEnvironment("app", "dev", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 scopes granted, got %d", count)
	}

	// Grant again — should be 0 (already granted)
	count, err = s.GrantEnvironment("app", "dev", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 scopes granted (already done), got %d", count)
	}

	// Revoke
	count, err = s.RevokeEnvironment("app", "dev", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 scopes revoked, got %d", count)
	}
}

// === Sync (.env generation) ===

func TestSyncDotenv(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")

	s.SetSecret("app", "dev/runtime", "API_KEY", "sk-123")
	s.SetSecret("app", "dev/runtime", "DB_URL", "postgres://localhost")

	envFile := filepath.Join(t.TempDir(), ".env")
	if err := s.SyncDotenv("app", "dev", envFile); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !contains(content, "API_KEY=sk-123") {
		t.Fatal("missing API_KEY")
	}
	if !contains(content, "DB_URL=postgres://localhost") {
		t.Fatal("missing DB_URL")
	}

	// File permissions should be 0600
	info, _ := os.Stat(envFile)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
}

func TestSyncDotenvEmptyEnv(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")

	envFile := filepath.Join(t.TempDir(), ".env")
	err := s.SyncDotenv("app", "dev", envFile)
	if err == nil {
		t.Fatal("expected error syncing empty env")
	}
}

// === DefaultProject resolution ===

func TestDefaultProjectResolution(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("my-app")
	s.DefaultProject = "my-app"

	// Empty string should resolve to default
	slug, err := s.resolveProject("")
	if err != nil {
		t.Fatal(err)
	}
	if slug != "my-app" {
		t.Fatalf("expected 'my-app', got %q", slug)
	}

	// Explicit slug should override
	slug, err = s.resolveProject("other")
	if err != nil {
		t.Fatal(err)
	}
	if slug != "other" {
		t.Fatalf("expected 'other', got %q", slug)
	}

	// No default, no explicit
	s.DefaultProject = ""
	_, err = s.resolveProject("")
	if err == nil {
		t.Fatal("expected error with no project")
	}
}

// === Cross-environment isolation ===

func TestEnvironmentIsolation(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateEnvironment("app", "prod")
	s.CreateScope("app", "dev/runtime")
	s.CreateScope("app", "prod/runtime")

	s.SetSecret("app", "dev/runtime", "API_KEY", "dev-key")
	s.SetSecret("app", "prod/runtime", "API_KEY", "prod-key")

	devSecrets, _ := s.GetAllSecretsInEnv("app", "dev")
	prodSecrets, _ := s.GetAllSecretsInEnv("app", "prod")

	if devSecrets["API_KEY"] != "dev-key" {
		t.Fatal("dev API_KEY wrong")
	}
	if prodSecrets["API_KEY"] != "prod-key" {
		t.Fatal("prod API_KEY wrong")
	}
}

// === Special values ===

func TestSpecialCharacterSecrets(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")

	testCases := map[string]string{
		"EMPTY":      "",
		"SPACES":     "value with spaces",
		"NEWLINES":   "line1\nline2",
		"UNICODE":    "日本語 🔑",
		"QUOTES":     `she said "hello"`,
		"URL":        "postgres://user:p@ss@host:5432/db?ssl=true&timeout=30",
		"JSON":       `{"key": "value", "nested": {"a": 1}}`,
		"LONG_VALUE": string(make([]byte, 10000)),
	}

	for k, v := range testCases {
		if err := s.SetSecret("app", "dev/runtime", k, v); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
	}

	for k, expected := range testCases {
		got, err := s.GetSecret("app", "dev/runtime", k)
		if err != nil {
			t.Fatalf("get %s: %v", k, err)
		}
		if got.Value != expected {
			t.Fatalf("%s: value mismatch (len expected=%d, got=%d)", k, len(expected), len(got.Value))
		}
	}
}

// === Secret history ===

func TestSecretHistory(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")

	s.SetSecret("app", "dev/runtime", "KEY", "v1")
	s.SetSecret("app", "dev/runtime", "KEY", "v2")
	s.SetSecret("app", "dev/runtime", "KEY", "v3")

	current, history, err := s.GetSecretHistory("app", "dev/runtime", "KEY")
	if err != nil {
		t.Fatal(err)
	}

	if current.Value != "v3" {
		t.Fatalf("expected current 'v3', got %q", current.Value)
	}
	if current.Version != 3 {
		t.Fatalf("expected version 3, got %d", current.Version)
	}

	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
	if history[0].Value != "v2" {
		t.Fatalf("expected first history entry 'v2', got %q", history[0].Value)
	}
	if history[1].Value != "v1" {
		t.Fatalf("expected second history entry 'v1', got %q", history[1].Value)
	}
}

func TestSecretHistoryLimit(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")

	// Set 15 versions — history should cap at 10.
	for i := 1; i <= 15; i++ {
		s.SetSecret("app", "dev/runtime", "KEY", fmt.Sprintf("v%d", i))
	}

	_, history, err := s.GetSecretHistory("app", "dev/runtime", "KEY")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) > 10 {
		t.Fatalf("history should be capped at 10, got %d", len(history))
	}
}

func TestSecretHistoryEmpty(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")

	s.SetSecret("app", "dev/runtime", "KEY", "only-version")

	current, history, err := s.GetSecretHistory("app", "dev/runtime", "KEY")
	if err != nil {
		t.Fatal(err)
	}
	if current.Value != "only-version" {
		t.Fatal("wrong current value")
	}
	if len(history) != 0 {
		t.Fatalf("expected no history, got %d", len(history))
	}
}

// === Provider metadata ===

func TestProviderMetadata(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")

	if err := s.SetSecretWithProvider("app", "dev/runtime", "OPENAI_API_KEY", "sk-123", "openai"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSecretWithProvider("app", "dev/runtime", "STRIPE_KEY", "sk_test", "stripe"); err != nil {
		t.Fatal(err)
	}
	// Regular secret without provider
	if err := s.SetSecret("app", "dev/runtime", "DATABASE_URL", "postgres://localhost"); err != nil {
		t.Fatal(err)
	}

	// Verify providers are in manifest
	scopes, err := s.ListScopes("app", "dev")
	if err != nil {
		t.Fatal(err)
	}
	// We can't directly check providers via ListScopes since Scope doesn't expose them,
	// but we can verify the secrets are correctly stored.
	if len(scopes[0].Secrets) != 3 {
		t.Fatalf("expected 3 secrets, got %d", len(scopes[0].Secrets))
	}

	// Verify the actual secret values are correct
	secret, err := s.GetSecret("app", "dev/runtime", "OPENAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "sk-123" {
		t.Fatal("wrong value for OPENAI_API_KEY")
	}
}

// === Rotation flags ===

func TestFlagSecretsForRotation(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")

	s.SetSecret("app", "dev/runtime", "KEY1", "v1")
	s.SetSecret("app", "dev/runtime", "KEY2", "v2")

	count, err := s.FlagSecretsForRotation("app", "dev/runtime", "user bob revoked")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 flagged, got %d", count)
	}

	// Flagging again should be 0 (already flagged).
	count, err = s.FlagSecretsForRotation("app", "dev/runtime", "again")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 (already flagged), got %d", count)
	}
}

func TestRevokeWithRotation(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/runtime")
	s.CreateScope("app", "dev/db")

	s.SetSecret("app", "dev/runtime", "API_KEY", "val")
	s.SetSecret("app", "dev/db", "DB_URL", "val")
	s.SetSecret("app", "dev/db", "DB_PASS", "val")

	k2, _ := age.GenerateX25519Identity()
	s.AddUser("alice", "", k2.Recipient().String())
	s.GrantEnvironment("app", "dev", "alice")

	scopeCount, secretCount, err := s.RevokeEnvironmentWithRotation("app", "dev", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if scopeCount != 2 {
		t.Fatalf("expected 2 scopes revoked, got %d", scopeCount)
	}
	if secretCount != 3 {
		t.Fatalf("expected 3 secrets flagged, got %d", secretCount)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
