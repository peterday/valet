package store

import (
	"testing"

	"github.com/peterday/valet/internal/domain"
)

// setupLinkedStore creates a named store with secrets for testing linked store resolution.
func setupLinkedStore(t *testing.T, name string, secrets map[string]map[string]string) LinkedStore {
	t.Helper()
	s, _ := setupTestStore(t)
	s.Meta.Name = name
	s.CreateProject("app")
	s.DefaultProject = "app"

	for env, kvs := range secrets {
		s.CreateEnvironment("app", env)
		s.CreateScope("app", env+"/default")
		for k, v := range kvs {
			if err := s.SetSecret("app", env+"/default", k, v); err != nil {
				t.Fatalf("set %s/%s in %s: %v", env, k, name, err)
			}
		}
	}

	return LinkedStore{Store: s}
}

// setupLinkedStoreWithLink is like setupLinkedStore but attaches a StoreLink.
func setupLinkedStoreWithLink(t *testing.T, name string, secrets map[string]map[string]string, link *domain.StoreLink) LinkedStore {
	t.Helper()
	ls := setupLinkedStore(t, name, secrets)
	ls.Link = link
	return ls
}

// === Basic resolution (no link metadata) ===

func TestResolveAllSecrets_SingleStore(t *testing.T) {
	ls := setupLinkedStore(t, "embedded", map[string]map[string]string{
		"dev": {"API_KEY": "sk-123", "DB_URL": "postgres://dev"},
	})

	result, err := ResolveAllSecrets([]LinkedStore{ls}, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(result))
	}
	if result["API_KEY"].Value != "sk-123" {
		t.Fatalf("expected sk-123, got %q", result["API_KEY"].Value)
	}
	if result["DB_URL"].Value != "postgres://dev" {
		t.Fatalf("expected postgres://dev, got %q", result["DB_URL"].Value)
	}
}

func TestResolveAllSecrets_LaterStoreOverrides(t *testing.T) {
	personal := setupLinkedStore(t, "personal", map[string]map[string]string{
		"dev": {"API_KEY": "personal-key", "EXTRA": "from-personal"},
	})
	embedded := setupLinkedStore(t, "embedded", map[string]map[string]string{
		"dev": {"API_KEY": "project-key"},
	})

	result, err := ResolveAllSecrets([]LinkedStore{personal, embedded}, "dev")
	if err != nil {
		t.Fatal(err)
	}

	// Embedded (later) should override personal for API_KEY.
	if result["API_KEY"].Value != "project-key" {
		t.Fatalf("expected project-key, got %q", result["API_KEY"].Value)
	}
	if result["API_KEY"].StoreName != "embedded" {
		t.Fatalf("expected source embedded, got %q", result["API_KEY"].StoreName)
	}

	// EXTRA only exists in personal, should still be present.
	if result["EXTRA"].Value != "from-personal" {
		t.Fatalf("expected from-personal, got %q", result["EXTRA"].Value)
	}
}

func TestResolveAllSecrets_EmptyEnv(t *testing.T) {
	ls := setupLinkedStore(t, "store", map[string]map[string]string{
		"dev": {"KEY": "val"},
	})

	result, err := ResolveAllSecrets([]LinkedStore{ls}, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 secrets for nonexistent env, got %d", len(result))
	}
}

// === Wildcard environment ===

func TestResolveAllSecrets_WildcardEnv(t *testing.T) {
	ls := setupLinkedStore(t, "store", map[string]map[string]string{
		"*": {"GLOBAL_KEY": "global-value"},
	})

	result, err := ResolveAllSecrets([]LinkedStore{ls}, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if result["GLOBAL_KEY"].Value != "global-value" {
		t.Fatalf("expected global-value, got %q", result["GLOBAL_KEY"].Value)
	}
	if !result["GLOBAL_KEY"].Wildcard {
		t.Fatal("expected Wildcard=true for * env secret")
	}
}

func TestResolveAllSecrets_ExactEnvOverridesWildcard(t *testing.T) {
	ls := setupLinkedStore(t, "store", map[string]map[string]string{
		"*":   {"API_KEY": "wildcard-key", "SHARED": "shared-val"},
		"dev": {"API_KEY": "dev-key"},
	})

	result, err := ResolveAllSecrets([]LinkedStore{ls}, "dev")
	if err != nil {
		t.Fatal(err)
	}

	// Exact env should override wildcard.
	if result["API_KEY"].Value != "dev-key" {
		t.Fatalf("expected dev-key, got %q", result["API_KEY"].Value)
	}
	if result["API_KEY"].Wildcard {
		t.Fatal("exact env match should not be marked as Wildcard")
	}

	// Wildcard-only key should still resolve.
	if result["SHARED"].Value != "shared-val" {
		t.Fatalf("expected shared-val, got %q", result["SHARED"].Value)
	}
	if !result["SHARED"].Wildcard {
		t.Fatal("wildcard-only key should be marked as Wildcard")
	}
}

// === Key filtering ===

func TestResolveAllSecrets_KeyFiltering(t *testing.T) {
	link := &domain.StoreLink{
		Name: "team-infra",
		RawKeys: []any{
			"CACHE_URL",
			// DB_URL is NOT in the filter — should be excluded
		},
	}

	ls := setupLinkedStoreWithLink(t, "team-infra", map[string]map[string]string{
		"dev": {"CACHE_URL": "redis://cache", "DB_URL": "postgres://db", "OTHER": "val"},
	}, link)

	result, err := ResolveAllSecrets([]LinkedStore{ls}, "dev")
	if err != nil {
		t.Fatal(err)
	}

	if result["CACHE_URL"].Value != "redis://cache" {
		t.Fatalf("expected redis://cache, got %q", result["CACHE_URL"].Value)
	}
	if _, found := result["DB_URL"]; found {
		t.Fatal("DB_URL should be filtered out")
	}
	if _, found := result["OTHER"]; found {
		t.Fatal("OTHER should be filtered out")
	}
}

func TestResolveAllSecrets_KeyFilteringWithWildcard(t *testing.T) {
	link := &domain.StoreLink{
		Name:    "team",
		RawKeys: []any{"ALLOWED"},
	}

	ls := setupLinkedStoreWithLink(t, "team", map[string]map[string]string{
		"*":   {"ALLOWED": "wildcard-val", "BLOCKED": "nope"},
		"dev": {"ALLOWED": "dev-val", "BLOCKED": "nope-dev"},
	}, link)

	result, err := ResolveAllSecrets([]LinkedStore{ls}, "dev")
	if err != nil {
		t.Fatal(err)
	}

	if result["ALLOWED"].Value != "dev-val" {
		t.Fatalf("expected dev-val, got %q", result["ALLOWED"].Value)
	}
	if _, found := result["BLOCKED"]; found {
		t.Fatal("BLOCKED should be filtered out even from wildcard and exact env")
	}
}

// === Key name remapping ===

func TestResolveAllSecrets_KeyRemapping(t *testing.T) {
	link := &domain.StoreLink{
		Name: "team-infra",
		RawKeys: []any{
			map[string]any{"local": "DATABASE_URL", "remote": "POSTGRES_PRIMARY_URL"},
			map[string]any{"local": "DATABASE_URL_RO", "remote": "POSTGRES_REPLICA_URL"},
		},
	}

	ls := setupLinkedStoreWithLink(t, "team-infra", map[string]map[string]string{
		"dev": {
			"POSTGRES_PRIMARY_URL": "postgres://primary",
			"POSTGRES_REPLICA_URL": "postgres://replica",
			"OTHER_KEY":            "should-be-filtered",
		},
	}, link)

	result, err := ResolveAllSecrets([]LinkedStore{ls}, "dev")
	if err != nil {
		t.Fatal(err)
	}

	if result["DATABASE_URL"].Value != "postgres://primary" {
		t.Fatalf("expected postgres://primary, got %q", result["DATABASE_URL"].Value)
	}
	if result["DATABASE_URL_RO"].Value != "postgres://replica" {
		t.Fatalf("expected postgres://replica, got %q", result["DATABASE_URL_RO"].Value)
	}
	if _, found := result["POSTGRES_PRIMARY_URL"]; found {
		t.Fatal("remote key name should not appear in result")
	}
	if _, found := result["OTHER_KEY"]; found {
		t.Fatal("unmapped key should be filtered out")
	}
}

func TestResolveAllSecrets_MixedPlainAndMappedKeys(t *testing.T) {
	link := &domain.StoreLink{
		Name: "team",
		RawKeys: []any{
			"CACHE_URL", // plain string — local == remote
			map[string]any{"local": "DB_URL", "remote": "POSTGRES_URL"},
		},
	}

	ls := setupLinkedStoreWithLink(t, "team", map[string]map[string]string{
		"dev": {
			"CACHE_URL":    "redis://cache",
			"POSTGRES_URL": "postgres://db",
			"EXTRA":        "filtered",
		},
	}, link)

	result, err := ResolveAllSecrets([]LinkedStore{ls}, "dev")
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(result))
	}
	if result["CACHE_URL"].Value != "redis://cache" {
		t.Fatalf("expected redis://cache, got %q", result["CACHE_URL"].Value)
	}
	if result["DB_URL"].Value != "postgres://db" {
		t.Fatalf("expected postgres://db, got %q", result["DB_URL"].Value)
	}
}

// === Environment mapping ===

func TestResolveAllSecrets_EnvMapping(t *testing.T) {
	link := &domain.StoreLink{
		Name: "team",
		Environments: []domain.EnvMapping{
			{Local: "dev", Remote: "staging"},
		},
	}

	ls := setupLinkedStoreWithLink(t, "team", map[string]map[string]string{
		"staging": {"API_KEY": "staging-key"},
		"dev":     {"API_KEY": "dev-key-should-not-be-used"},
	}, link)

	result, err := ResolveAllSecrets([]LinkedStore{ls}, "dev")
	if err != nil {
		t.Fatal(err)
	}

	// Local "dev" maps to remote "staging".
	if result["API_KEY"].Value != "staging-key" {
		t.Fatalf("expected staging-key (mapped from staging), got %q", result["API_KEY"].Value)
	}
}

func TestResolveAllSecrets_UnmappedEnvPassesThrough(t *testing.T) {
	link := &domain.StoreLink{
		Name: "team",
		Environments: []domain.EnvMapping{
			{Local: "dev", Remote: "staging"}, // only dev is mapped
		},
	}

	ls := setupLinkedStoreWithLink(t, "team", map[string]map[string]string{
		"prod": {"API_KEY": "prod-key"},
	}, link)

	// "prod" is not in the mapping — should pass through as-is.
	result, err := ResolveAllSecrets([]LinkedStore{ls}, "prod")
	if err != nil {
		t.Fatal(err)
	}

	if result["API_KEY"].Value != "prod-key" {
		t.Fatalf("expected prod-key, got %q", result["API_KEY"].Value)
	}
}

// === Combined: key filtering + env mapping ===

func TestResolveAllSecrets_KeyFilteringAndEnvMapping(t *testing.T) {
	link := &domain.StoreLink{
		Name: "team-infra",
		RawKeys: []any{
			map[string]any{"local": "DB_URL", "remote": "PG_URL"},
		},
		Environments: []domain.EnvMapping{
			{Local: "dev", Remote: "development"},
		},
	}

	ls := setupLinkedStoreWithLink(t, "team-infra", map[string]map[string]string{
		"development": {"PG_URL": "postgres://dev-db", "EXTRA": "filtered"},
	}, link)

	result, err := ResolveAllSecrets([]LinkedStore{ls}, "dev")
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(result))
	}
	if result["DB_URL"].Value != "postgres://dev-db" {
		t.Fatalf("expected postgres://dev-db, got %q", result["DB_URL"].Value)
	}
}

// === No link metadata (embedded/local stores) ===

func TestResolveAllSecrets_NilLinkPassesAllKeys(t *testing.T) {
	ls := LinkedStore{
		Store: setupLinkedStore(t, "embedded", map[string]map[string]string{
			"dev": {"A": "1", "B": "2", "C": "3"},
		}).Store,
		Link: nil, // embedded store — no filtering
	}

	result, err := ResolveAllSecrets([]LinkedStore{ls}, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 secrets with nil link, got %d", len(result))
	}
}

// === Full resolution order ===

func TestResolveAllSecrets_FullResolutionOrder(t *testing.T) {
	// Simulates: personal → shared → embedded → local override
	personal := setupLinkedStore(t, "personal", map[string]map[string]string{
		"dev": {"KEY_A": "personal-a", "KEY_B": "personal-b", "KEY_C": "personal-c", "KEY_D": "personal-d"},
	})
	shared := setupLinkedStore(t, "shared", map[string]map[string]string{
		"dev": {"KEY_A": "shared-a", "KEY_B": "shared-b", "KEY_C": "shared-c"},
	})
	embedded := setupLinkedStore(t, "embedded", map[string]map[string]string{
		"dev": {"KEY_A": "embedded-a", "KEY_B": "embedded-b"},
	})
	local := setupLinkedStore(t, ".valet.local", map[string]map[string]string{
		"dev": {"KEY_A": "local-a"},
	})

	stores := []LinkedStore{personal, shared, embedded, local}
	result, err := ResolveAllSecrets(stores, "dev")
	if err != nil {
		t.Fatal(err)
	}

	// Local override wins for KEY_A.
	if result["KEY_A"].Value != "local-a" {
		t.Fatalf("KEY_A: expected local-a, got %q (from %s)", result["KEY_A"].Value, result["KEY_A"].StoreName)
	}
	// Embedded wins for KEY_B (later than shared/personal).
	if result["KEY_B"].Value != "embedded-b" {
		t.Fatalf("KEY_B: expected embedded-b, got %q (from %s)", result["KEY_B"].Value, result["KEY_B"].StoreName)
	}
	// Shared wins for KEY_C (later than personal).
	if result["KEY_C"].Value != "shared-c" {
		t.Fatalf("KEY_C: expected shared-c, got %q (from %s)", result["KEY_C"].Value, result["KEY_C"].StoreName)
	}
	// Personal is the only source for KEY_D.
	if result["KEY_D"].Value != "personal-d" {
		t.Fatalf("KEY_D: expected personal-d, got %q (from %s)", result["KEY_D"].Value, result["KEY_D"].StoreName)
	}
}

// === ResolveAllSecretsFlat ===

func TestResolveAllSecretsFlat(t *testing.T) {
	ls := setupLinkedStore(t, "store", map[string]map[string]string{
		"dev": {"A": "1", "B": "2"},
	})

	flat, err := ResolveAllSecretsFlat([]LinkedStore{ls}, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if flat["A"] != "1" || flat["B"] != "2" {
		t.Fatal("flat values mismatch")
	}
}

// === ResolveAllSecretsWithProvenance ===

func TestResolveAllSecretsWithProvenance_Basic(t *testing.T) {
	personal := setupLinkedStore(t, "personal", map[string]map[string]string{
		"dev": {"KEY": "personal-val"},
	})
	embedded := setupLinkedStore(t, "embedded", map[string]map[string]string{
		"dev": {"KEY": "embedded-val"},
	})

	result, provenance, err := ResolveAllSecretsWithProvenance([]LinkedStore{personal, embedded}, "dev", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Result should be from embedded (later store).
	if result["KEY"].Value != "embedded-val" {
		t.Fatalf("expected embedded-val, got %q", result["KEY"].Value)
	}

	// Provenance should show both sources.
	chain := provenance["KEY"]
	if len(chain) != 2 {
		t.Fatalf("expected 2 provenance entries, got %d", len(chain))
	}
}

func TestResolveAllSecretsWithProvenance_Overrides(t *testing.T) {
	ls := setupLinkedStore(t, "store", map[string]map[string]string{
		"dev": {"KEY": "store-val"},
	})

	overrides := map[string]string{"KEY": "override-val"}
	result, provenance, err := ResolveAllSecretsWithProvenance([]LinkedStore{ls}, "dev", overrides)
	if err != nil {
		t.Fatal(err)
	}

	// Override wins.
	if result["KEY"].Value != "override-val" {
		t.Fatalf("expected override-val, got %q", result["KEY"].Value)
	}
	if result["KEY"].StoreName != "--set" {
		t.Fatalf("expected source --set, got %q", result["KEY"].StoreName)
	}

	// Provenance should show both store and override.
	chain := provenance["KEY"]
	if len(chain) != 2 {
		t.Fatalf("expected 2 provenance entries, got %d", len(chain))
	}
}

func TestResolveAllSecretsWithProvenance_KeyRemapping(t *testing.T) {
	link := &domain.StoreLink{
		Name: "team",
		RawKeys: []any{
			map[string]any{"local": "MY_DB", "remote": "POSTGRES_URL"},
		},
	}

	ls := setupLinkedStoreWithLink(t, "team", map[string]map[string]string{
		"dev": {"POSTGRES_URL": "pg://db"},
	}, link)

	result, provenance, err := ResolveAllSecretsWithProvenance([]LinkedStore{ls}, "dev", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Result should use local key name.
	if result["MY_DB"].Value != "pg://db" {
		t.Fatalf("expected pg://db under MY_DB, got %q", result["MY_DB"].Value)
	}

	// Provenance should also use local key name.
	if _, found := provenance["MY_DB"]; !found {
		t.Fatal("provenance should have entry for MY_DB (local name)")
	}
	if _, found := provenance["POSTGRES_URL"]; found {
		t.Fatal("provenance should not have entry for POSTGRES_URL (remote name)")
	}
}

func TestResolveAllSecretsWithProvenance_EnvMapping(t *testing.T) {
	link := &domain.StoreLink{
		Name: "team",
		Environments: []domain.EnvMapping{
			{Local: "dev", Remote: "staging"},
		},
	}

	ls := setupLinkedStoreWithLink(t, "team", map[string]map[string]string{
		"staging": {"KEY": "staging-val"},
	}, link)

	result, _, err := ResolveAllSecretsWithProvenance([]LinkedStore{ls}, "dev", nil)
	if err != nil {
		t.Fatal(err)
	}

	if result["KEY"].Value != "staging-val" {
		t.Fatalf("expected staging-val, got %q", result["KEY"].Value)
	}
}

// === mapRemoteToLocal ===

func TestMapRemoteToLocal_NilMappings(t *testing.T) {
	// nil means all keys, no remapping.
	if got := mapRemoteToLocal("ANY_KEY", nil); got != "ANY_KEY" {
		t.Fatalf("expected ANY_KEY, got %q", got)
	}
}

func TestMapRemoteToLocal_IdentityMapping(t *testing.T) {
	mappings := []domain.KeyMapping{
		{Local: "KEY_A", Remote: "KEY_A"},
		{Local: "KEY_B", Remote: "KEY_B"},
	}
	if got := mapRemoteToLocal("KEY_A", mappings); got != "KEY_A" {
		t.Fatalf("expected KEY_A, got %q", got)
	}
}

func TestMapRemoteToLocal_Remapped(t *testing.T) {
	mappings := []domain.KeyMapping{
		{Local: "DB_URL", Remote: "POSTGRES_URL"},
	}
	if got := mapRemoteToLocal("POSTGRES_URL", mappings); got != "DB_URL" {
		t.Fatalf("expected DB_URL, got %q", got)
	}
}

func TestMapRemoteToLocal_FilteredOut(t *testing.T) {
	mappings := []domain.KeyMapping{
		{Local: "KEY_A", Remote: "KEY_A"},
	}
	if got := mapRemoteToLocal("UNLISTED_KEY", mappings); got != "" {
		t.Fatalf("expected empty (filtered), got %q", got)
	}
}

// === keyMappingsForLink ===

func TestKeyMappingsForLink_NilLink(t *testing.T) {
	if got := keyMappingsForLink(nil); got != nil {
		t.Fatal("expected nil for nil link")
	}
}

func TestKeyMappingsForLink_NoKeys(t *testing.T) {
	link := &domain.StoreLink{Name: "store"}
	if got := keyMappingsForLink(link); got != nil {
		t.Fatal("expected nil for link with no keys")
	}
}

func TestKeyMappingsForLink_WithKeys(t *testing.T) {
	link := &domain.StoreLink{
		Name: "store",
		RawKeys: []any{
			"PLAIN_KEY",
			map[string]any{"local": "LOCAL", "remote": "REMOTE"},
		},
	}
	got := keyMappingsForLink(link)
	if len(got) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(got))
	}
	if got[0].Local != "PLAIN_KEY" || got[0].Remote != "PLAIN_KEY" {
		t.Fatalf("plain key: expected PLAIN_KEY/PLAIN_KEY, got %s/%s", got[0].Local, got[0].Remote)
	}
	if got[1].Local != "LOCAL" || got[1].Remote != "REMOTE" {
		t.Fatalf("mapped key: expected LOCAL/REMOTE, got %s/%s", got[1].Local, got[1].Remote)
	}
}

// === StoreLinkNames / HasStoreLink ===

func TestStoreLinkNames(t *testing.T) {
	links := []domain.StoreLink{
		{Name: "alpha"},
		{Name: "beta"},
		{Name: "gamma"},
	}
	names := StoreLinkNames(links)
	if len(names) != 3 {
		t.Fatalf("expected 3, got %d", len(names))
	}
	if names[0] != "alpha" || names[1] != "beta" || names[2] != "gamma" {
		t.Fatalf("unexpected names: %v", names)
	}
}

func TestHasStoreLink(t *testing.T) {
	links := []domain.StoreLink{
		{Name: "alpha"},
		{Name: "beta"},
	}
	if !HasStoreLink(links, "alpha") {
		t.Fatal("expected to find alpha")
	}
	if !HasStoreLink(links, "beta") {
		t.Fatal("expected to find beta")
	}
	if HasStoreLink(links, "gamma") {
		t.Fatal("should not find gamma")
	}
	if HasStoreLink(nil, "alpha") {
		t.Fatal("should not find in nil slice")
	}
}

// === StoreLink.ParsedKeys ===

func TestStoreLink_ParsedKeys_Empty(t *testing.T) {
	link := domain.StoreLink{Name: "store"}
	if got := link.ParsedKeys(); got != nil {
		t.Fatal("expected nil for no keys")
	}
}

func TestStoreLink_ParsedKeys_PlainStrings(t *testing.T) {
	link := domain.StoreLink{
		Name:    "store",
		RawKeys: []any{"KEY_A", "KEY_B"},
	}
	got := link.ParsedKeys()
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	// Plain strings become identity mappings.
	if got[0].Local != "KEY_A" || got[0].Remote != "KEY_A" {
		t.Fatal("plain string should map to itself")
	}
}

func TestStoreLink_ParsedKeys_Maps(t *testing.T) {
	link := domain.StoreLink{
		Name: "store",
		RawKeys: []any{
			map[string]any{"local": "DB", "remote": "POSTGRES_URL"},
		},
	}
	got := link.ParsedKeys()
	if len(got) != 1 {
		t.Fatalf("expected 1, got %d", len(got))
	}
	if got[0].Local != "DB" || got[0].Remote != "POSTGRES_URL" {
		t.Fatalf("expected DB/POSTGRES_URL, got %s/%s", got[0].Local, got[0].Remote)
	}
}

// === StoreLink.ResolveEnv ===

func TestStoreLink_ResolveEnv_Mapped(t *testing.T) {
	link := domain.StoreLink{
		Name: "store",
		Environments: []domain.EnvMapping{
			{Local: "dev", Remote: "staging"},
			{Local: "prod", Remote: "production"},
		},
	}
	if got := link.ResolveEnv("dev"); got != "staging" {
		t.Fatalf("expected staging, got %q", got)
	}
	if got := link.ResolveEnv("prod"); got != "production" {
		t.Fatalf("expected production, got %q", got)
	}
}

func TestStoreLink_ResolveEnv_Unmapped(t *testing.T) {
	link := domain.StoreLink{
		Name: "store",
		Environments: []domain.EnvMapping{
			{Local: "dev", Remote: "staging"},
		},
	}
	// "test" is not mapped — should pass through.
	if got := link.ResolveEnv("test"); got != "test" {
		t.Fatalf("expected test (passthrough), got %q", got)
	}
}

func TestStoreLink_ResolveEnv_NoMappings(t *testing.T) {
	link := domain.StoreLink{Name: "store"}
	if got := link.ResolveEnv("dev"); got != "dev" {
		t.Fatalf("expected dev (passthrough), got %q", got)
	}
}

// === Edge cases ===

func TestResolveAllSecrets_EmptyStores(t *testing.T) {
	result, err := ResolveAllSecrets(nil, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 secrets from nil stores, got %d", len(result))
	}

	result, err = ResolveAllSecrets([]LinkedStore{}, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 secrets from empty stores, got %d", len(result))
	}
}

func TestResolveAllSecrets_KeyRemappingWithWildcard(t *testing.T) {
	link := &domain.StoreLink{
		Name: "team",
		RawKeys: []any{
			map[string]any{"local": "MY_KEY", "remote": "TEAM_KEY"},
		},
	}

	ls := setupLinkedStoreWithLink(t, "team", map[string]map[string]string{
		"*":   {"TEAM_KEY": "wildcard-val"},
		"dev": {"TEAM_KEY": "dev-val"},
	}, link)

	result, err := ResolveAllSecrets([]LinkedStore{ls}, "dev")
	if err != nil {
		t.Fatal(err)
	}

	// Exact env wins, remapped to local name.
	if result["MY_KEY"].Value != "dev-val" {
		t.Fatalf("expected dev-val, got %q", result["MY_KEY"].Value)
	}
	if _, found := result["TEAM_KEY"]; found {
		t.Fatal("remote key name should not appear")
	}
}

func TestResolveAllSecrets_LinkedStoreFilteredEmbeddedNot(t *testing.T) {
	// Linked store has filtering — only CACHE_URL passes through.
	// Embedded store has no link — all keys pass through.
	link := &domain.StoreLink{
		Name:    "team",
		RawKeys: []any{"CACHE_URL"},
	}
	team := setupLinkedStoreWithLink(t, "team", map[string]map[string]string{
		"dev": {"CACHE_URL": "redis://team", "SECRET_KEY": "team-secret"},
	}, link)
	embedded := LinkedStore{
		Store: setupLinkedStore(t, "embedded", map[string]map[string]string{
			"dev": {"API_KEY": "embedded-api"},
		}).Store,
		Link: nil,
	}

	result, err := ResolveAllSecrets([]LinkedStore{team, embedded}, "dev")
	if err != nil {
		t.Fatal(err)
	}

	if result["CACHE_URL"].Value != "redis://team" {
		t.Fatalf("expected redis://team, got %q", result["CACHE_URL"].Value)
	}
	if result["API_KEY"].Value != "embedded-api" {
		t.Fatalf("expected embedded-api, got %q", result["API_KEY"].Value)
	}
	if _, found := result["SECRET_KEY"]; found {
		t.Fatal("SECRET_KEY should be filtered from team store")
	}
}

// === OpenLinkedStores ===

func TestOpenLinkedStores_ResolutionOrder(t *testing.T) {
	s1, id := setupTestStore(t)
	s1.Meta.Name = "embedded"

	s2, _ := setupTestStore(t)
	s2.Meta.Name = "local-override"

	// No actual linked stores to open (they'd need to be on disk),
	// but we can verify embedded and local override positioning.
	stores := OpenLinkedStores(nil, nil, s1, s2, id)

	if len(stores) != 2 {
		t.Fatalf("expected 2 stores, got %d", len(stores))
	}
	if stores[0].Store.Meta.Name != "embedded" {
		t.Fatalf("expected embedded first, got %q", stores[0].Store.Meta.Name)
	}
	if stores[0].Link != nil {
		t.Fatal("embedded store should have nil link")
	}
	if stores[1].Store.Meta.Name != "local-override" {
		t.Fatalf("expected local-override second, got %q", stores[1].Store.Meta.Name)
	}
	if stores[1].Link != nil {
		t.Fatal("local override store should have nil link")
	}
}

func TestOpenLinkedStores_NilStores(t *testing.T) {
	id := testIdentity(t)
	stores := OpenLinkedStores(nil, nil, nil, nil, id)
	if len(stores) != 0 {
		t.Fatalf("expected 0 stores with all nil, got %d", len(stores))
	}
}

// === CreateLocalStore / OpenLocalStore ===

func TestCreateAndOpenLocalStore(t *testing.T) {
	dir := t.TempDir()
	id := testIdentity(t)

	// Create the local store.
	s, err := CreateLocalStore(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("expected non-nil store")
	}

	// OpenLocalStore should find it.
	s2 := OpenLocalStore(dir, id)
	if s2 == nil {
		t.Fatal("expected OpenLocalStore to find the store")
	}
	if s2.Meta.Name != ".valet.local" {
		t.Fatalf("expected name .valet.local, got %q", s2.Meta.Name)
	}

	// CreateLocalStore again should just open existing.
	s3, err := CreateLocalStore(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if s3 == nil {
		t.Fatal("expected non-nil store on second create")
	}
}

func TestStoreLink_ParsedKeys_SkipsMalformed(t *testing.T) {
	link := domain.StoreLink{
		Name: "store",
		RawKeys: []any{
			map[string]any{"local": "GOOD", "remote": "REMOTE_GOOD"},
			map[string]any{"local": "", "remote": "MISSING_LOCAL"},  // malformed
			map[string]any{"local": "MISSING_REMOTE"},               // malformed
			map[string]any{},                                        // empty
			"PLAIN",
		},
	}
	got := link.ParsedKeys()
	if len(got) != 2 {
		t.Fatalf("expected 2 valid mappings, got %d: %+v", len(got), got)
	}
	if got[0].Local != "GOOD" || got[0].Remote != "REMOTE_GOOD" {
		t.Fatalf("first: expected GOOD/REMOTE_GOOD, got %s/%s", got[0].Local, got[0].Remote)
	}
	if got[1].Local != "PLAIN" || got[1].Remote != "PLAIN" {
		t.Fatalf("second: expected PLAIN/PLAIN, got %s/%s", got[1].Local, got[1].Remote)
	}
}

func TestOpenLocalStore_NonExistent(t *testing.T) {
	dir := t.TempDir()
	id := testIdentity(t)

	s := OpenLocalStore(dir, id)
	if s != nil {
		t.Fatal("expected nil for nonexistent local store")
	}
}
