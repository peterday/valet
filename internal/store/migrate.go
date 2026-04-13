package store

import (
	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/provider"
)

// MigratePlan describes what `valet migrate` would do to a project's
// .valet.toml after .env.example has been adopted as the source of truth.
type MigratePlan struct {
	HasEnvExample bool
	ExamplePath   string

	// Redundant: in .valet.toml but exactly matches what auto-detection would
	// produce from .env.example. Safe to remove.
	Redundant []MigrateEntry

	// Overrides: in .valet.toml but differs from auto-detection. Should stay
	// in .valet.toml as an override.
	Overrides []MigrateEntry

	// MissingFromExample: in .valet.toml but not in .env.example at all.
	// Stays in .valet.toml (acts as the only source).
	MissingFromExample []MigrateEntry

	// NewlyDetected: in .env.example, will become available after migration
	// without needing an entry in .valet.toml.
	NewlyDetected []string
}

// MigrateEntry is a single entry in a migration plan.
type MigrateEntry struct {
	Key      string
	Existing domain.Requirement
	Reason   string // for Overrides: why it differs
}

// PlanMigration analyzes a project and returns the migration plan.
func PlanMigration(projectDir string, vc *domain.ValetConfig) *MigratePlan {
	plan := &MigratePlan{}

	examplePath := FindEnvExample(projectDir)
	if examplePath == "" {
		return plan // no .env.example → nothing to migrate
	}
	plan.HasEnvExample = true
	plan.ExamplePath = examplePath

	// Auto-detected from .env.example.
	parsed, err := ParseEnvExample(examplePath)
	if err != nil {
		return plan
	}

	autoDetected := make(map[string]autoEntry)
	for _, entry := range parsed.Entries {
		if !isSecret(entry.Key, entry.Value) {
			continue
		}
		ae := autoEntry{Description: entry.Description}
		if p := provider.FindByEnvVar(entry.Key); p != nil {
			ae.Provider = p.Name
		}
		autoDetected[entry.Key] = ae
	}

	// Compare each .valet.toml entry against auto-detection.
	if vc != nil {
		for key, req := range vc.Requires {
			auto, isAuto := autoDetected[key]
			entry := MigrateEntry{Key: key, Existing: req}

			if !isAuto {
				plan.MissingFromExample = append(plan.MissingFromExample, entry)
				continue
			}

			// Check if this entry is redundant (matches auto-detection).
			diff := requirementDiff(req, auto)
			if diff == "" {
				plan.Redundant = append(plan.Redundant, entry)
			} else {
				entry.Reason = diff
				plan.Overrides = append(plan.Overrides, entry)
			}
		}
	}

	// Find keys that auto-detection picks up but aren't yet in .valet.toml.
	for key := range autoDetected {
		if vc == nil || vc.Requires == nil {
			plan.NewlyDetected = append(plan.NewlyDetected, key)
			continue
		}
		if _, exists := vc.Requires[key]; !exists {
			plan.NewlyDetected = append(plan.NewlyDetected, key)
		}
	}

	return plan
}

// Apply executes the migration: removes redundant entries from vc.Requires.
// Returns the updated config (caller should write it).
func (p *MigratePlan) Apply(vc *domain.ValetConfig) *domain.ValetConfig {
	if vc == nil {
		return vc
	}
	for _, entry := range p.Redundant {
		delete(vc.Requires, entry.Key)
	}
	if len(vc.Requires) == 0 {
		vc.Requires = nil
	}
	return vc
}

type autoEntry struct {
	Provider    string
	Description string
}

// requirementDiff returns "" if the existing matches auto-detection,
// or a short reason describing the difference.
func requirementDiff(req domain.Requirement, auto autoEntry) string {
	if req.Provider != "" && req.Provider != auto.Provider {
		return "different provider"
	}
	if req.Optional {
		return "marked optional"
	}
	if req.Track != nil {
		return "explicit track override"
	}
	if req.Scope != "" {
		return "scope override"
	}
	// Description differences are not enough to count as override —
	// the user might have a richer description than the comments.
	if req.Description != "" && req.Description != auto.Description {
		// Keep it as override since the user-provided description is more authoritative.
		return "custom description"
	}
	return ""
}
