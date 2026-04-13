package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/peterday/valet/internal/domain"
)

func TestResolveRequirements_EnvExampleOnly(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
OPENAI_API_KEY=sk-...
DATABASE_URL=postgres://...
NODE_ENV=development
`), 0644)

	reqs := ResolveRequirements(dir, nil, nil)

	// NODE_ENV is config, should not appear.
	keys := make(map[string]bool)
	for _, r := range reqs {
		keys[r.Key] = true
	}
	if !keys["OPENAI_API_KEY"] {
		t.Error("expected OPENAI_API_KEY")
	}
	if !keys["DATABASE_URL"] {
		t.Error("expected DATABASE_URL")
	}
	if keys["NODE_ENV"] {
		t.Error("NODE_ENV should not be tracked")
	}
}

func TestResolveRequirements_SharedOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
OPENAI_API_KEY=sk-...
SENTRY_DSN=https://...
`), 0644)

	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"SENTRY_DSN": {Optional: true},
		},
	}

	reqs := ResolveRequirements(dir, vc, nil)

	for _, r := range reqs {
		if r.Key == "SENTRY_DSN" && !r.Optional {
			t.Error("expected SENTRY_DSN to be optional (from shared override)")
		}
	}
}

func TestResolveRequirements_PersonalOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
OPENAI_API_KEY=sk-...
`), 0644)

	lc := &domain.LocalConfig{
		Requires: map[string]domain.Requirement{
			"OPENAI_API_KEY": {Description: "my personal description"},
		},
	}

	reqs := ResolveRequirements(dir, nil, lc)

	for _, r := range reqs {
		if r.Key == "OPENAI_API_KEY" && r.Description != "my personal description" {
			t.Errorf("expected personal description, got %q", r.Description)
		}
	}
}

func TestResolveRequirements_TrackFalseExcludes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
OPENAI_API_KEY=sk-...
STRIPE_KEY=sk_test_...
`), 0644)

	f := false
	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"STRIPE_KEY": {Track: &f},
		},
	}

	reqs := ResolveRequirements(dir, vc, nil)

	for _, r := range reqs {
		if r.Key == "STRIPE_KEY" {
			t.Error("STRIPE_KEY should be excluded (track=false)")
		}
	}
	if len(reqs) != 1 {
		t.Errorf("expected 1 requirement, got %d", len(reqs))
	}
}

func TestResolveRequirements_TrackTrueIncludes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
PORT=3000
`), 0644)

	tr := true
	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"PORT": {Track: &tr},
		},
	}

	reqs := ResolveRequirements(dir, vc, nil)

	found := false
	for _, r := range reqs {
		if r.Key == "PORT" {
			found = true
		}
	}
	if !found {
		t.Error("PORT should be tracked (track=true override)")
	}
}

func TestResolveRequirements_VaultTomlOnly(t *testing.T) {
	dir := t.TempDir()
	// No .env.example — requirements come from .valet.toml directly.

	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"CUSTOM_KEY": {Provider: "custom", Description: "my key"},
		},
	}

	reqs := ResolveRequirements(dir, vc, nil)

	if len(reqs) != 1 || reqs[0].Key != "CUSTOM_KEY" {
		t.Errorf("expected CUSTOM_KEY, got %v", reqs)
	}
}

func TestResolveRequirements_PersonalOnlyMode(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
OPENAI_API_KEY=sk-...
`), 0644)

	// No ValetConfig, just local.
	lc := &domain.LocalConfig{
		Requires: map[string]domain.Requirement{
			"MY_LOCAL_KEY": {Description: "local only"},
		},
	}

	reqs := ResolveRequirements(dir, nil, lc)

	keys := make(map[string]bool)
	for _, r := range reqs {
		keys[r.Key] = true
	}
	if !keys["OPENAI_API_KEY"] {
		t.Error("expected OPENAI_API_KEY from .env.example")
	}
	if !keys["MY_LOCAL_KEY"] {
		t.Error("expected MY_LOCAL_KEY from local config")
	}
}

func TestResolveRequirements_ProviderOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
MY_AI_KEY=sk-...
`), 0644)

	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"MY_AI_KEY": {Provider: "openai"},
		},
	}

	reqs := ResolveRequirements(dir, vc, nil)

	for _, r := range reqs {
		if r.Key == "MY_AI_KEY" && r.Provider != "openai" {
			t.Errorf("expected provider 'openai', got %q", r.Provider)
		}
	}
}

func TestResolveRequirements_LayerPriority(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
MY_KEY=...
`), 0644)

	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"MY_KEY": {Description: "shared desc"},
		},
	}
	lc := &domain.LocalConfig{
		Requires: map[string]domain.Requirement{
			"MY_KEY": {Description: "personal desc"},
		},
	}

	reqs := ResolveRequirements(dir, vc, lc)

	for _, r := range reqs {
		if r.Key == "MY_KEY" {
			if r.Description != "personal desc" {
				t.Errorf("personal should win, got %q", r.Description)
			}
			if r.Source != "valet-local-toml" {
				t.Errorf("source should be valet-local-toml, got %q", r.Source)
			}
		}
	}
}
