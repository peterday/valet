package store

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// EnvExampleEntry is one variable parsed from a .env.example file.
type EnvExampleEntry struct {
	Key             string
	Value           string   // raw value as written
	Description     string   // joined leading + inline comments
	Section         string   // most recent section header
	IsPlaceholder   bool     // value looks like a placeholder, not real
	LineNumber      int
}

// EnvExample is a parsed .env.example (or similar) file.
type EnvExample struct {
	Path    string
	Entries []EnvExampleEntry
}

// CandidateExampleNames is the list of filenames we'll look for, in priority order.
var CandidateExampleNames = []string{
	".env.example",
	".env.sample",
	".env.template",
	".env.dist",
	"env.example",
	"env.sample",
}

// FindEnvExample looks for a .env.example-style file in the given directory.
// Returns the path of the first match, or "" if none found.
func FindEnvExample(dir string) string {
	for _, name := range CandidateExampleNames {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

// ParseEnvExample reads and parses a .env.example file.
func ParseEnvExample(path string) (*EnvExample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := &EnvExample{Path: path}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var pendingComments []string
	var currentSection string
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimRight(scanner.Text(), "\r")
		trimmed := strings.TrimSpace(line)

		// Empty line resets pending comments.
		if trimmed == "" {
			pendingComments = nil
			continue
		}

		// Comment line.
		if strings.HasPrefix(trimmed, "#") {
			content := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
			// Section header detection: === Foo ===, ### Foo ###, --- Foo ---
			if section := extractSection(content); section != "" {
				currentSection = section
				pendingComments = nil
				continue
			}
			pendingComments = append(pendingComments, content)
			continue
		}

		// Try to parse KEY=VALUE.
		key, value, inlineComment, ok := parseEnvLine(trimmed)
		if !ok {
			pendingComments = nil
			continue
		}

		entry := EnvExampleEntry{
			Key:        key,
			Value:      value,
			Section:    currentSection,
			LineNumber: lineNum,
		}

		// Build description from leading comments + inline comment.
		var descParts []string
		for _, c := range pendingComments {
			if c != "" {
				descParts = append(descParts, c)
			}
		}
		if inlineComment != "" {
			descParts = append(descParts, inlineComment)
		}
		entry.Description = strings.Join(descParts, " — ")
		entry.IsPlaceholder = looksLikePlaceholder(value)

		result.Entries = append(result.Entries, entry)
		pendingComments = nil
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// parseEnvLine parses "KEY=VALUE # comment" with optional quoting.
// Returns (key, value, inlineComment, ok).
func parseEnvLine(line string) (string, string, string, bool) {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return "", "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	if key == "" || !validEnvName(key) {
		return "", "", "", false
	}
	// Strip optional `export ` prefix.
	key = strings.TrimPrefix(key, "export ")
	key = strings.TrimSpace(key)

	rest := strings.TrimSpace(line[idx+1:])

	// Handle quoted values.
	if len(rest) >= 2 && (rest[0] == '"' || rest[0] == '\'') {
		quote := rest[0]
		end := strings.IndexByte(rest[1:], quote)
		if end >= 0 {
			value := rest[1 : 1+end]
			after := strings.TrimSpace(rest[2+end:])
			inlineComment := ""
			if strings.HasPrefix(after, "#") {
				inlineComment = strings.TrimSpace(strings.TrimPrefix(after, "#"))
			}
			return key, value, inlineComment, true
		}
	}

	// Unquoted: split on first unescaped #
	value := rest
	inlineComment := ""
	if hashIdx := findInlineHash(rest); hashIdx >= 0 {
		value = strings.TrimSpace(rest[:hashIdx])
		inlineComment = strings.TrimSpace(rest[hashIdx+1:])
	}
	return key, value, inlineComment, true
}

// findInlineHash returns the index of an inline comment hash (with whitespace before),
// or -1. We require whitespace before # to avoid matching things like sk-#abc.
func findInlineHash(s string) int {
	for i := 1; i < len(s); i++ {
		if s[i] == '#' && (s[i-1] == ' ' || s[i-1] == '\t') {
			return i
		}
	}
	return -1
}

var envNameRegex = regexp.MustCompile(`^(export\s+)?[A-Za-z_][A-Za-z0-9_]*$`)

func validEnvName(name string) bool {
	return envNameRegex.MatchString(name)
}

// extractSection returns the section name from a comment like "=== Database ==="
// or "### API Keys ###". Requires explicit markers to count as a section.
func extractSection(comment string) string {
	// Must start AND end with a marker character to be a section.
	hasMarkers := false
	for _, marker := range "=-*" {
		if strings.HasPrefix(comment, string(marker)) && strings.HasSuffix(comment, string(marker)) {
			hasMarkers = true
			break
		}
	}
	if !hasMarkers {
		return ""
	}
	// Strip the markers.
	for _, marker := range []string{"=", "-", "*"} {
		comment = strings.Trim(comment, marker)
	}
	comment = strings.TrimSpace(comment)
	if comment == "" || len(comment) > 40 {
		return ""
	}
	return comment
}

// placeholderPrefixes are values that, when the value STARTS with them
// (case-insensitive), indicate a placeholder.
var placeholderPrefixes = []string{
	"your_", "your-", "your ",
	"replace_", "replace-", "replace ", "replaceme",
	"changeme", "change_me", "change-me", "change me",
	"xxx", "yyy", "zzz",
	"<", "[", "{",
	"placeholder",
	"todo",
	"example_", "example-",
	"my_", "my-",
	"insert_", "insert-",
}

// looksLikePlaceholder returns true if the value appears to be a placeholder
// rather than a real value.
func looksLikePlaceholder(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return true
	}
	// Pure punctuation.
	if strings.Trim(v, ".-*_") == "" {
		return true
	}
	// Trailing ellipsis without anything substantial before it (e.g. "sk-...").
	if strings.HasSuffix(v, "...") && len(strings.TrimRight(v, ".")) < 10 {
		return true
	}
	// Starts-with check for known placeholder words.
	for _, p := range placeholderPrefixes {
		if strings.HasPrefix(v, p) {
			return true
		}
	}
	return false
}
