package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsSecret(t *testing.T) {
	secrets := []struct {
		key, value string
	}{
		{"OPENAI_API_KEY", "sk-..."},
		{"STRIPE_SECRET_KEY", "sk_test_..."},
		{"DATABASE_URL", "postgres://user:pass@host/db"},
		{"AUTH_TOKEN", "abc123"},
		{"MY_PASSWORD", "secret"},
		{"WEBHOOK_SECRET", "whsec_..."},
		{"AWS_ACCESS_KEY_ID", "AKIA..."},
		{"REDIS_URL", "redis://localhost:6379"},
	}
	for _, tt := range secrets {
		if !isSecret(tt.key, tt.value) {
			t.Errorf("expected isSecret(%q, %q) = true", tt.key, tt.value)
		}
	}

	configs := []struct {
		key, value string
	}{
		{"NODE_ENV", "development"},
		{"LOG_LEVEL", "info"},
		{"PORT", "3000"},
		{"DEBUG", "true"},
		{"APP_HOST", "localhost"},
		{"TIMEOUT", "30"},
	}
	for _, tt := range configs {
		if isSecret(tt.key, tt.value) {
			t.Errorf("expected isSecret(%q, %q) = false", tt.key, tt.value)
		}
	}
}

func TestAnalyzeForAdopt(t *testing.T) {
	dir := t.TempDir()

	// No .env.example → error.
	_, err := AnalyzeForAdopt(dir)
	if err == nil {
		t.Fatal("expected error when no .env.example")
	}

	// Write .env.example.
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
# === API Keys ===
OPENAI_API_KEY=sk-...
DATABASE_URL=postgres://...

# === Config ===
NODE_ENV=development
PORT=3000
`), 0644)

	result, err := AnalyzeForAdopt(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Requirements) != 2 {
		t.Fatalf("expected 2 requirements, got %d", len(result.Requirements))
	}
	if len(result.NonSecrets) != 2 {
		t.Fatalf("expected 2 non-secrets, got %d", len(result.NonSecrets))
	}

	// Check that OPENAI_API_KEY is detected as a secret.
	foundOpenAI := false
	for _, req := range result.Requirements {
		if req.Key == "OPENAI_API_KEY" {
			foundOpenAI = true
			// Provider match depends on registry being installed — skip check in CI.
		}
	}
	if !foundOpenAI {
		t.Error("expected OPENAI_API_KEY to be detected as a requirement")
	}
}

func TestAnalyzeForAdopt_WithExistingEnv(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
OPENAI_API_KEY=sk-...
DATABASE_URL=postgres://...
`), 0644)

	os.WriteFile(filepath.Join(dir, ".env"), []byte(`
OPENAI_API_KEY=sk-proj-realkey123
DATABASE_URL=postgres://real:pass@db.example.com/prod
`), 0644)

	result, err := AnalyzeForAdopt(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !result.HasExistingEnv {
		t.Error("expected HasExistingEnv to be true")
	}
	if len(result.ExistingValues) != 2 {
		t.Errorf("expected 2 existing values, got %d", len(result.ExistingValues))
	}
	if result.ExistingValues["OPENAI_API_KEY"] != "sk-proj-realkey123" {
		t.Errorf("unexpected value: %q", result.ExistingValues["OPENAI_API_KEY"])
	}
}

func TestAnalyzeForAdopt_PlaceholderFiltering(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
KEY=sk-...
`), 0644)

	// .env with placeholder value should NOT be imported.
	os.WriteFile(filepath.Join(dir, ".env"), []byte(`
KEY=your_api_key_here
`), 0644)

	result, err := AnalyzeForAdopt(dir)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := result.ExistingValues["KEY"]; ok {
		t.Error("placeholder value should not be in ExistingValues")
	}
}

func TestClassifyConfigReason(t *testing.T) {
	tests := []struct {
		key, value string
		wantNonEmpty bool
	}{
		{"NODE_ENV", "development", true},
		{"PORT", "3000", true},
		{"LOG_LEVEL", "info", true},
		{"MY_SECRET", "abc", false},
	}
	for _, tt := range tests {
		reason := classifyConfigReason(tt.key, tt.value)
		if tt.wantNonEmpty && reason == "" {
			t.Errorf("expected reason for %q=%q", tt.key, tt.value)
		}
	}
}
