package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/peterday/valet/internal/domain"
)

func TestResolveRequirements_EmptyEnvExample(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
# Just comments, no actual variables
NODE_ENV=development
PORT=3000
`), 0644)

	reqs := ResolveRequirements(dir, nil, nil)
	// NODE_ENV and PORT are config — should be 0 requirements.
	if len(reqs) != 0 {
		t.Errorf("expected 0 requirements (only config vars), got %d: %v", len(reqs), reqs)
	}
}

func TestResolveRequirements_ConflictingOverrides(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
MY_SECRET_KEY=abc...
`), 0644)

	f := false
	tr := true

	// Shared says don't track, personal says track → personal wins.
	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"MY_SECRET_KEY": {Track: &f},
		},
	}
	lc := &domain.LocalConfig{
		Requires: map[string]domain.Requirement{
			"MY_SECRET_KEY": {Track: &tr},
		},
	}

	reqs := ResolveRequirements(dir, vc, lc)

	// Shared Track=false removes it, then personal Track=true adds it back.
	// Actually: applyOverride with Track=false deletes from merged,
	// then personal layer re-creates it. Let me verify...
	// The shared layer deletes it. Then the personal layer calls applyOverride
	// which creates a new entry since it was deleted.
	found := false
	for _, r := range reqs {
		if r.Key == "MY_SECRET_KEY" {
			found = true
		}
	}
	// Track=true in personal should re-add after shared removed it.
	if !found {
		t.Error("personal track=true should override shared track=false")
	}
}

func TestResolveRequirements_NoEnvExample_NoConfig(t *testing.T) {
	dir := t.TempDir()
	reqs := ResolveRequirements(dir, nil, nil)
	if len(reqs) != 0 {
		t.Errorf("expected 0 requirements, got %d", len(reqs))
	}
}

func TestResolveRequirements_SharedAddsNewKey(t *testing.T) {
	dir := t.TempDir()
	// No .env.example — only .valet.toml has the key.
	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"CUSTOM_KEY":  {Provider: "custom"},
			"ANOTHER_KEY": {Description: "something"},
		},
	}

	reqs := ResolveRequirements(dir, vc, nil)
	if len(reqs) != 2 {
		t.Errorf("expected 2 from .valet.toml, got %d", len(reqs))
	}

	for _, r := range reqs {
		if r.Source != "valet-toml" {
			t.Errorf("source should be 'valet-toml', got %q", r.Source)
		}
	}
}

func TestResolveRequirements_PersonalAddsNewKey(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte("AUTH_TOKEN=..."), 0644)

	lc := &domain.LocalConfig{
		Requires: map[string]domain.Requirement{
			"DEV_ONLY_KEY": {Description: "my dev thing"},
		},
	}

	reqs := ResolveRequirements(dir, nil, lc)

	keys := make(map[string]string) // key → source
	for _, r := range reqs {
		keys[r.Key] = r.Source
	}
	if keys["AUTH_TOKEN"] != "env-example" {
		t.Errorf("AUTH_TOKEN source = %q, want env-example", keys["AUTH_TOKEN"])
	}
	if keys["DEV_ONLY_KEY"] != "valet-local-toml" {
		t.Errorf("DEV_ONLY_KEY source = %q, want valet-local-toml", keys["DEV_ONLY_KEY"])
	}
}

func TestResolveRequirements_OverrideDescription(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
# Auto-detected description
AUTH_TOKEN=...
`), 0644)

	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"AUTH_TOKEN": {Description: "Better description from team"},
		},
	}

	reqs := ResolveRequirements(dir, vc, nil)

	for _, r := range reqs {
		if r.Key == "AUTH_TOKEN" {
			if r.Description != "Better description from team" {
				t.Errorf("description not overridden: %q", r.Description)
			}
		}
	}
}

func TestResolveRequirements_OptionalOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte("SENTRY_DSN=https://..."), 0644)

	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"SENTRY_DSN": {Optional: true},
		},
	}

	reqs := ResolveRequirements(dir, vc, nil)

	for _, r := range reqs {
		if r.Key == "SENTRY_DSN" && !r.Optional {
			t.Error("SENTRY_DSN should be optional")
		}
	}
}

func TestResolveRequirements_SortedOutput(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
ZEBRA_KEY=...
ALPHA_TOKEN=...
MIDDLE_SECRET=...
`), 0644)

	reqs := ResolveRequirements(dir, nil, nil)

	if len(reqs) < 2 {
		t.Skip("not enough secret-classified keys")
	}
	for i := 1; i < len(reqs); i++ {
		if reqs[i].Key < reqs[i-1].Key {
			t.Errorf("not sorted: %s comes after %s", reqs[i].Key, reqs[i-1].Key)
		}
	}
}
