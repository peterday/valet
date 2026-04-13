package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseEnvExample_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(""), 0644)

	parsed, err := ParseEnvExample(filepath.Join(dir, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(parsed.Entries))
	}
}

func TestParseEnvExample_OnlyComments(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
# This is a header
# with multiple lines
# but no variables
`), 0644)

	parsed, err := ParseEnvExample(filepath.Join(dir, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(parsed.Entries))
	}
}

func TestParseEnvExample_MalformedLines(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
not-a-valid-line
=no_key
VALID_KEY=value
123BAD=nope
`), 0644)

	parsed, err := ParseEnvExample(filepath.Join(dir, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	// Only VALID_KEY should parse (123BAD fails envNameRegex).
	if len(parsed.Entries) != 1 || parsed.Entries[0].Key != "VALID_KEY" {
		t.Errorf("expected [VALID_KEY], got %v", parsed.Entries)
	}
}

func TestParseEnvExample_HashInValue(t *testing.T) {
	dir := t.TempDir()
	// A URL with a fragment — the # should NOT be treated as inline comment
	// unless preceded by whitespace.
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
CALLBACK_URL=https://example.com/callback#section
REDIS_URL=redis://localhost:6379/0#db
WITH_COMMENT=value # this is a comment
`), 0644)

	parsed, err := ParseEnvExample(filepath.Join(dir, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Entries[0].Value != "https://example.com/callback#section" {
		t.Errorf("hash in URL shouldn't split: got %q", parsed.Entries[0].Value)
	}
	if parsed.Entries[1].Value != "redis://localhost:6379/0#db" {
		t.Errorf("hash in URL shouldn't split: got %q", parsed.Entries[1].Value)
	}
	if parsed.Entries[2].Value != "value" {
		t.Errorf("expected 'value', got %q", parsed.Entries[2].Value)
	}
	if parsed.Entries[2].Description != "this is a comment" {
		t.Errorf("expected inline comment, got %q", parsed.Entries[2].Description)
	}
}

func TestParseEnvExample_SpacesAroundEquals(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
KEY1 = value1
KEY2=value2
KEY3 =value3
`), 0644)

	parsed, err := ParseEnvExample(filepath.Join(dir, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}

	if len(parsed.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(parsed.Entries))
	}
	if parsed.Entries[0].Key != "KEY1" || parsed.Entries[0].Value != "value1" {
		t.Errorf("entry 0: key=%q val=%q", parsed.Entries[0].Key, parsed.Entries[0].Value)
	}
}

func TestParseEnvExample_DuplicateKeys(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
KEY=first
KEY=second
`), 0644)

	parsed, err := ParseEnvExample(filepath.Join(dir, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	// Both entries should be returned — dedup is the caller's responsibility.
	if len(parsed.Entries) != 2 {
		t.Errorf("expected 2 entries (both), got %d", len(parsed.Entries))
	}
}

func TestParseEnvExample_EmptyValue(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
KEY=
ANOTHER_KEY=""
`), 0644)

	parsed, err := ParseEnvExample(filepath.Join(dir, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Entries[0].Value != "" {
		t.Errorf("expected empty value, got %q", parsed.Entries[0].Value)
	}
	if !parsed.Entries[0].IsPlaceholder {
		t.Error("empty value should be a placeholder")
	}
	if parsed.Entries[1].Value != "" {
		t.Errorf("expected empty quoted value, got %q", parsed.Entries[1].Value)
	}
}

func TestParseEnvExample_WindowsLineEndings(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte("KEY1=val1\r\nKEY2=val2\r\n"), 0644)

	parsed, err := ParseEnvExample(filepath.Join(dir, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(parsed.Entries))
	}
	if parsed.Entries[0].Value != "val1" {
		t.Errorf("expected 'val1', got %q", parsed.Entries[0].Value)
	}
}

func TestParseEnvExample_CommentsResetOnBlankLine(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte(`
# Comment for KEY1

# Comment for KEY2
KEY2=val
`), 0644)

	parsed, err := ParseEnvExample(filepath.Join(dir, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	// Blank line between comment and KEY2 means the first comment is forgotten.
	if parsed.Entries[0].Description != "Comment for KEY2" {
		t.Errorf("expected 'Comment for KEY2', got %q", parsed.Entries[0].Description)
	}
}

func TestIsSecret_AmbiguousNames(t *testing.T) {
	// SECRET_MODE is config-like (mode), but contains "SECRET".
	if !isSecret("SECRET_MODE", "true") {
		t.Error("SECRET_MODE should be detected as secret (contains SECRET)")
	}

	// GITHUB_TOKEN is a token.
	if !isSecret("GITHUB_TOKEN", "ghp_xxx") {
		t.Error("GITHUB_TOKEN should be a secret")
	}

	// APP_URL is a URL var — treated as secret (connection strings often are).
	if !isSecret("APP_URL", "http://localhost:3000") {
		t.Error("APP_URL should be treated as secret (name ends with _URL)")
	}

	// APP_HOST (not _URL) with no secret signals is config.
	if isSecret("APP_HOST", "localhost") {
		t.Error("APP_HOST should not be a secret")
	}

	// But a URL with @ (credentials) IS.
	if !isSecret("DB_URL", "postgres://user:pass@host/db") {
		t.Error("URL with credentials should be a secret")
	}
}

func TestLooksLikePlaceholder_EdgeCases(t *testing.T) {
	// Short prefix-like values that are actually placeholders.
	if !looksLikePlaceholder("sk-...") {
		t.Error("'sk-...' should be placeholder")
	}

	// But longer values with prefix are real.
	if looksLikePlaceholder("sk-proj-1234567890abcdef") {
		t.Error("real-looking key should not be placeholder")
	}

	// Pure dots.
	if !looksLikePlaceholder("...") {
		t.Error("'...' should be placeholder")
	}

	// Curly braces.
	if !looksLikePlaceholder("{your_key}") {
		t.Error("'{your_key}' should be placeholder")
	}

	// Number is not a placeholder.
	if looksLikePlaceholder("3000") {
		t.Error("'3000' should not be placeholder")
	}
}
