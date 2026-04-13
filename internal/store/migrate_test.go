package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/peterday/valet/internal/domain"
)

func TestPlanMigration_NoEnvExample(t *testing.T) {
	dir := t.TempDir()
	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"KEY": {Provider: "test"},
		},
	}

	plan := PlanMigration(dir, vc)

	if plan.HasEnvExample {
		t.Error("expected no env example")
	}
	if len(plan.Redundant) != 0 {
		t.Error("expected no redundant entries")
	}
}

func TestPlanMigration_RedundantEntries(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
DATABASE_URL=postgres://...
AUTH_TOKEN=abc...
`), 0644)

	// DATABASE_URL with no provider override should be redundant
	// (auto-detection also finds it with no provider).
	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"DATABASE_URL": {},
			"AUTH_TOKEN":   {},
		},
	}

	plan := PlanMigration(dir, vc)

	if !plan.HasEnvExample {
		t.Fatal("expected HasEnvExample")
	}

	// Both should be redundant (match auto-detection with no provider).
	if len(plan.Redundant) != 2 {
		t.Errorf("expected 2 redundant, got %d (redundant=%v, overrides=%v)",
			len(plan.Redundant), plan.Redundant, plan.Overrides)
	}
}

func TestPlanMigration_OverrideKept(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
STRIPE_KEY=sk_test_...
`), 0644)

	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"STRIPE_KEY": {Optional: true},
		},
	}

	plan := PlanMigration(dir, vc)

	foundOverride := false
	for _, e := range plan.Overrides {
		if e.Key == "STRIPE_KEY" {
			foundOverride = true
			if e.Reason != "marked optional" {
				t.Errorf("expected reason 'marked optional', got %q", e.Reason)
			}
		}
	}
	if !foundOverride {
		t.Error("expected STRIPE_KEY as override")
	}
}

func TestPlanMigration_MissingFromExample(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
OPENAI_API_KEY=sk-...
`), 0644)

	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"OPENAI_API_KEY": {Provider: "openai"},
			"LEGACY_KEY":     {Provider: "custom"},
		},
	}

	plan := PlanMigration(dir, vc)

	foundMissing := false
	for _, e := range plan.MissingFromExample {
		if e.Key == "LEGACY_KEY" {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Error("expected LEGACY_KEY as missing from example")
	}
}

func TestPlanMigration_NewlyDetected(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
OPENAI_API_KEY=sk-...
NEW_SECRET_KEY=abc-...
`), 0644)

	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"OPENAI_API_KEY": {Provider: "openai"},
		},
	}

	plan := PlanMigration(dir, vc)

	foundNew := false
	for _, key := range plan.NewlyDetected {
		if key == "NEW_SECRET_KEY" {
			foundNew = true
		}
	}
	if !foundNew {
		t.Error("expected NEW_SECRET_KEY as newly detected")
	}
}

func TestPlanMigration_Apply(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
DATABASE_URL=postgres://...
AUTH_TOKEN=abc...
STRIPE_KEY=sk_test_...
`), 0644)

	vc := &domain.ValetConfig{
		Requires: map[string]domain.Requirement{
			"DATABASE_URL": {},                             // redundant
			"AUTH_TOKEN":   {},                             // redundant
			"STRIPE_KEY":   {Optional: true},               // override: optional
			"LEGACY_KEY":   {Provider: "custom"},           // not in .env.example
		},
	}

	plan := PlanMigration(dir, vc)
	updated := plan.Apply(vc)

	// DATABASE_URL + AUTH_TOKEN should be removed (redundant).
	if _, ok := updated.Requires["DATABASE_URL"]; ok {
		t.Error("DATABASE_URL should have been removed")
	}
	if _, ok := updated.Requires["AUTH_TOKEN"]; ok {
		t.Error("AUTH_TOKEN should have been removed")
	}

	// STRIPE_KEY should remain (override: optional).
	if _, ok := updated.Requires["STRIPE_KEY"]; !ok {
		t.Error("STRIPE_KEY should remain (override)")
	}

	// LEGACY_KEY should remain (not in .env.example).
	if _, ok := updated.Requires["LEGACY_KEY"]; !ok {
		t.Error("LEGACY_KEY should remain (not in example)")
	}
}
