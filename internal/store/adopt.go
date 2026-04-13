package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/identity"
	"github.com/peterday/valet/internal/provider"
)

// AdoptResult is the preview of what `valet adopt` would do.
type AdoptResult struct {
	SourceFile      string                 // .env.example path
	Requirements    []DetectedRequirement  // detected secrets to track
	NonSecrets      []DetectedConfig       // detected config to leave in .env
	HasExistingEnv  bool                   // is there a populated .env to import from?
	ExistingEnvPath string
	ExistingValues  map[string]string // values from existing .env if present
}

// DetectedRequirement is a secret detected from .env.example.
type DetectedRequirement struct {
	Key              string
	Description      string
	Section          string
	ProviderName     string // canonical provider name, "" if no match
	ProviderDisplay  string // display name, "" if no match
	MatchConfidence  string // "exact", "fuzzy", "none"
	PlaceholderValue string // value as found in .env.example (for hint)
}

// DetectedConfig is a non-secret config var (will not be tracked by valet).
type DetectedConfig struct {
	Key    string
	Value  string
	Reason string // why we think this is config, not secret
}

// secretNamePatterns are case-insensitive substring matches that suggest a secret.
var secretNamePatterns = []string{
	"KEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "PWD",
	"DSN", "CREDENTIAL", "AUTH", "CERT", "PRIVATE",
	"API_KEY", "ACCESS_KEY", "WEBHOOK",
}

// configNamePatterns are case-insensitive matches that suggest plain config.
var configNamePatterns = []string{
	"NODE_ENV", "ENV", "ENVIRONMENT",
	"LOG_LEVEL", "LOGLEVEL", "DEBUG",
	"PORT", "HOST", "BIND",
	"REGION",
	"VERSION",
	"TIMEOUT", "INTERVAL",
}

// configValueExacts are common config value strings.
var configValueExacts = map[string]bool{
	"true": true, "false": true,
	"development": true, "production": true, "staging": true, "test": true,
	"info": true, "debug": true, "warn": true, "error": true, "trace": true,
	"localhost": true,
}

// AnalyzeForAdopt scans a project directory and returns an AdoptResult preview.
// It does not modify any files.
func AnalyzeForAdopt(projectDir string) (*AdoptResult, error) {
	examplePath := FindEnvExample(projectDir)
	if examplePath == "" {
		return nil, fmt.Errorf("no .env.example file found in %s", projectDir)
	}

	parsed, err := ParseEnvExample(examplePath)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", examplePath, err)
	}

	result := &AdoptResult{SourceFile: examplePath}

	for _, entry := range parsed.Entries {
		if isSecret(entry.Key, entry.Value) {
			req := DetectedRequirement{
				Key:              entry.Key,
				Description:      entry.Description,
				Section:          entry.Section,
				PlaceholderValue: entry.Value,
				MatchConfidence:  "none",
			}
			// Look up provider by env var name (exact match).
			if p := provider.FindByEnvVar(entry.Key); p != nil {
				req.ProviderName = p.Name
				req.ProviderDisplay = p.DisplayName
				req.MatchConfidence = "exact"
			}
			result.Requirements = append(result.Requirements, req)
		} else {
			result.NonSecrets = append(result.NonSecrets, DetectedConfig{
				Key:    entry.Key,
				Value:  entry.Value,
				Reason: classifyConfigReason(entry.Key, entry.Value),
			})
		}
	}

	// Check for an existing populated .env alongside the example.
	envPath := filepath.Join(projectDir, ".env")
	if data, err := os.ReadFile(envPath); err == nil {
		result.ExistingEnvPath = envPath
		result.HasExistingEnv = true
		result.ExistingValues = parseDotenvValues(string(data))
	}

	return result, nil
}

// Apply executes the adoption: creates an embedded .valet/ store, writes
// .valet.toml with requirements, and optionally imports values from existing .env.
func (a *AdoptResult) Apply(projectDir string, id *identity.Identity, importExisting bool) error {
	// 1. Create embedded store if missing.
	storeRoot := filepath.Join(projectDir, ".valet")
	if _, err := os.Stat(filepath.Join(storeRoot, "store.json")); os.IsNotExist(err) {
		s, err := Create(storeRoot, "default", domain.StoreTypeEmbedded, id)
		if err != nil {
			return fmt.Errorf("creating embedded store: %w", err)
		}
		s.AddUser("me", "", id.PublicKey)
		s.CreateProject("default")
		s.CreateEnvironment("default", "dev")
		s.CreateScope("default", "dev/default")
	}

	// 2. Build .valet.toml with requirements.
	tomlPath := filepath.Join(projectDir, config.ValetToml)
	var vc *domain.ValetConfig
	if existing, err := config.LoadValetToml(tomlPath); err == nil {
		vc = existing
	} else {
		vc = &domain.ValetConfig{
			Store:      ".",
			Project:    "default",
			DefaultEnv: "dev",
		}
	}
	if vc.Requires == nil {
		vc.Requires = make(map[string]domain.Requirement)
	}
	for _, req := range a.Requirements {
		// Don't overwrite existing requirements.
		if _, exists := vc.Requires[req.Key]; exists {
			continue
		}
		vc.Requires[req.Key] = domain.Requirement{
			Provider:    req.ProviderName,
			Description: req.Description,
		}
	}
	if err := config.WriteValetToml(tomlPath, vc); err != nil {
		return fmt.Errorf("writing .valet.toml: %w", err)
	}

	// 3. Import existing .env values into the store.
	if importExisting && a.HasExistingEnv {
		s, err := Open(storeRoot, id)
		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}
		for _, req := range a.Requirements {
			val, ok := a.ExistingValues[req.Key]
			if !ok || val == "" {
				continue
			}
			scopePath := vc.DefaultEnv + "/default"
			if req.ProviderName != "" {
				_ = s.SetSecretWithProvider("default", scopePath, req.Key, val, req.ProviderName)
			} else {
				_ = s.SetSecret("default", scopePath, req.Key, val)
			}
		}
	}

	return nil
}

// isSecret returns true if a variable should be tracked as a secret.
func isSecret(key, value string) bool {
	upper := strings.ToUpper(key)

	// 1. Known provider env var → definitely a secret.
	if provider.FindByEnvVar(key) != nil {
		return true
	}

	// 2. Explicitly config-shaped name → not a secret.
	for _, p := range configNamePatterns {
		if upper == p || strings.HasSuffix(upper, "_"+p) || strings.HasPrefix(upper, p+"_") {
			return false
		}
	}

	// 3. Secret-shaped name → secret (checked before value heuristics
	// so SECRET_MODE=true is still a secret).
	for _, p := range secretNamePatterns {
		if strings.Contains(upper, p) {
			return true
		}
	}

	// 4. Common config values → not a secret.
	v := strings.ToLower(strings.TrimSpace(value))
	if configValueExacts[v] {
		return false
	}
	// Numeric values are usually config (ports, timeouts).
	if matchNumeric(v) {
		return false
	}

	// 5. URL secrets: credentials in URL or name ends with _URL/_URI.
	if strings.Contains(value, "://") {
		// URL with embedded credentials (@) is definitely a secret.
		if strings.Contains(value, "@") {
			return true
		}
		// Name ending with _URL or _URI + a non-localhost URL → likely a secret.
		if strings.HasSuffix(upper, "_URL") || strings.HasSuffix(upper, "_URI") {
			return true
		}
	}

	// Default: assume config (don't track unless we're confident it's a secret).
	return false
}

var numericRegex = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

func matchNumeric(s string) bool {
	return numericRegex.MatchString(s)
}

func classifyConfigReason(key, value string) string {
	upper := strings.ToUpper(key)
	for _, p := range configNamePatterns {
		if upper == p || strings.HasSuffix(upper, "_"+p) || strings.HasPrefix(upper, p+"_") {
			return "looks like config"
		}
	}
	v := strings.ToLower(strings.TrimSpace(value))
	if configValueExacts[v] {
		return "value looks like config"
	}
	if matchNumeric(v) {
		return "numeric value"
	}
	return "no secret signal"
}

// parseDotenvValues reads a .env file content and returns key→value.
// Strips quotes and handles inline comments.
func parseDotenvValues(content string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, _, ok := parseEnvLine(line)
		if !ok {
			continue
		}
		// Skip placeholder values.
		if !looksLikePlaceholder(value) {
			values[key] = value
		}
	}
	return values
}
