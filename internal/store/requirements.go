package store

import (
	"sort"

	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/provider"
)

// ResolvedRequirement is a single requirement after merging all sources:
// .env.example heuristics, .valet.toml [requires] overrides, and
// .valet.local.toml [requires] personal overrides.
type ResolvedRequirement struct {
	Key         string
	Provider    string // canonical provider name, "" if none
	Description string
	Section     string // from .env.example section header, if any
	Optional    bool

	// Source describes where the strongest signal came from.
	// One of: "env-example", "valet-toml", "valet-local-toml", "valet-toml-only".
	Source string

	// LayerSources lists every source that contributed to this requirement,
	// for debugging/inspection.
	LayerSources []string
}

// ResolveRequirements merges requirements from .env.example, .valet.toml, and
// .valet.local.toml. Personal overrides take precedence, then shared, then auto.
func ResolveRequirements(projectDir string, valetCfg *domain.ValetConfig, localCfg *domain.LocalConfig) []ResolvedRequirement {
	merged := make(map[string]*ResolvedRequirement)

	// Layer 1: .env.example auto-detection (lowest priority).
	if examplePath := FindEnvExample(projectDir); examplePath != "" {
		if parsed, err := ParseEnvExample(examplePath); err == nil {
			for _, entry := range parsed.Entries {
				if !isSecret(entry.Key, entry.Value) {
					continue
				}
				rr := &ResolvedRequirement{
					Key:          entry.Key,
					Description:  entry.Description,
					Section:      entry.Section,
					Source:       "env-example",
					LayerSources: []string{"env-example"},
				}
				if p := provider.FindByEnvVar(entry.Key); p != nil {
					rr.Provider = p.Name
				}
				merged[entry.Key] = rr
			}
		}
	}

	// Layer 2: .valet.toml [requires] (shared overrides).
	if valetCfg != nil {
		for key, req := range valetCfg.Requires {
			applyOverride(merged, key, req, "valet-toml")
		}
	}

	// Layer 3: .valet.local.toml [requires] (personal overrides — highest priority).
	if localCfg != nil {
		for key, req := range localCfg.Requires {
			applyOverride(merged, key, req, "valet-local-toml")
		}
	}

	// Filter out anything explicitly opted out (Track == false).
	var result []ResolvedRequirement
	for _, rr := range merged {
		if rr == nil {
			continue
		}
		result = append(result, *rr)
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

// applyOverride layers a Requirement onto an existing ResolvedRequirement,
// or creates a new one if no auto-detected entry existed.
func applyOverride(merged map[string]*ResolvedRequirement, key string, req domain.Requirement, source string) {
	existing := merged[key]

	// Track == false → opt out, remove from merged.
	if req.Track != nil && !*req.Track {
		delete(merged, key)
		return
	}

	if existing == nil {
		// New entry from override.
		rr := &ResolvedRequirement{
			Key:          key,
			Provider:     req.Provider,
			Description:  req.Description,
			Optional:     req.Optional,
			Source:       source,
			LayerSources: []string{source},
		}
		merged[key] = rr
		return
	}

	// Layer override fields onto existing.
	existing.LayerSources = append(existing.LayerSources, source)
	existing.Source = source // most recent layer wins for display
	if req.Provider != "" {
		existing.Provider = req.Provider
	}
	if req.Description != "" {
		existing.Description = req.Description
	}
	if req.Optional {
		existing.Optional = req.Optional
	}
}

// HasEnvExampleRequirements returns true if the project has a .env.example file.
func HasEnvExampleRequirements(projectDir string) bool {
	return FindEnvExample(projectDir) != ""
}
