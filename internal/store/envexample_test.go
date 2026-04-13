package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseEnvExample_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.example")
	os.WriteFile(path, []byte(`
# Database connection
DATABASE_URL=postgres://localhost/mydb

OPENAI_API_KEY=sk-...
PORT=3000
`), 0644)

	parsed, err := ParseEnvExample(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(parsed.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(parsed.Entries))
	}

	if parsed.Entries[0].Key != "DATABASE_URL" {
		t.Errorf("expected DATABASE_URL, got %s", parsed.Entries[0].Key)
	}
	if parsed.Entries[0].Description != "Database connection" {
		t.Errorf("expected 'Database connection', got %q", parsed.Entries[0].Description)
	}
}

func TestParseEnvExample_Sections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.example")
	os.WriteFile(path, []byte(`
# === Database ===
DATABASE_URL=postgres://...

# === API Keys ===
OPENAI_API_KEY=sk-...
STRIPE_KEY=sk_test_...
`), 0644)

	parsed, err := ParseEnvExample(path)
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Entries[0].Section != "Database" {
		t.Errorf("expected section 'Database', got %q", parsed.Entries[0].Section)
	}
	if parsed.Entries[1].Section != "API Keys" {
		t.Errorf("expected section 'API Keys', got %q", parsed.Entries[1].Section)
	}
	if parsed.Entries[2].Section != "API Keys" {
		t.Errorf("expected section 'API Keys', got %q", parsed.Entries[2].Section)
	}
}

func TestParseEnvExample_InlineComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.example")
	os.WriteFile(path, []byte(`
STRIPE_KEY=sk_test_... # from stripe dashboard
`), 0644)

	parsed, err := ParseEnvExample(path)
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Entries[0].Description != "from stripe dashboard" {
		t.Errorf("expected 'from stripe dashboard', got %q", parsed.Entries[0].Description)
	}
}

func TestParseEnvExample_QuotedValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.example")
	os.WriteFile(path, []byte(`
KEY1="hello world"
KEY2='single quoted'
KEY3=unquoted
`), 0644)

	parsed, err := ParseEnvExample(path)
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Entries[0].Value != "hello world" {
		t.Errorf("expected 'hello world', got %q", parsed.Entries[0].Value)
	}
	if parsed.Entries[1].Value != "single quoted" {
		t.Errorf("expected 'single quoted', got %q", parsed.Entries[1].Value)
	}
	if parsed.Entries[2].Value != "unquoted" {
		t.Errorf("expected 'unquoted', got %q", parsed.Entries[2].Value)
	}
}

func TestParseEnvExample_ExportPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.example")
	os.WriteFile(path, []byte(`export MY_KEY=value`), 0644)

	parsed, err := ParseEnvExample(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(parsed.Entries) != 1 || parsed.Entries[0].Key != "MY_KEY" {
		t.Errorf("expected MY_KEY, got %v", parsed.Entries)
	}
}

func TestParseEnvExample_MultilineComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.example")
	os.WriteFile(path, []byte(`
# OpenAI API key
# Get yours at platform.openai.com
OPENAI_API_KEY=sk-...
`), 0644)

	parsed, err := ParseEnvExample(path)
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Entries[0].Description != "OpenAI API key — Get yours at platform.openai.com" {
		t.Errorf("got %q", parsed.Entries[0].Description)
	}
}

func TestLooksLikePlaceholder(t *testing.T) {
	placeholders := []string{
		"",
		"sk-...",
		"your_api_key_here",
		"YOUR-KEY",
		"<insert key>",
		"[your key]",
		"changeme",
		"xxx",
		"...",
		"placeholder_value",
		"my_key",
	}
	for _, v := range placeholders {
		if !looksLikePlaceholder(v) {
			t.Errorf("expected %q to be a placeholder", v)
		}
	}

	realValues := []string{
		"sk-proj-abcd1234realkey5678",
		"postgres://user:pass@db.example.com:5432/mydb",
		"redis://localhost:6379",
		"true",
		"3000",
		"development",
		"whsec_abc123def456",
	}
	for _, v := range realValues {
		if looksLikePlaceholder(v) {
			t.Errorf("expected %q to NOT be a placeholder", v)
		}
	}
}

func TestFindEnvExample(t *testing.T) {
	dir := t.TempDir()

	// No file.
	if got := FindEnvExample(dir); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	// .env.example exists.
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte("KEY=val"), 0644)
	if got := FindEnvExample(dir); got == "" {
		t.Error("expected to find .env.example")
	}
}

func TestFindEnvExample_Priority(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.sample"), []byte("A=1"), 0644)
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte("B=2"), 0644)

	// .env.example should take priority.
	got := FindEnvExample(dir)
	if filepath.Base(got) != ".env.example" {
		t.Errorf("expected .env.example, got %q", filepath.Base(got))
	}
}

func TestExtractSection(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"=== Database ===", "Database"},
		{"--- API Keys ---", "API Keys"},
		{"*** Config ***", "Config"},
		{"OpenAI API key", ""},         // not a section
		{"This is a long sentence.", ""}, // too sentence-like
	}

	for _, tt := range tests {
		got := extractSection(tt.input)
		if got != tt.want {
			t.Errorf("extractSection(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
