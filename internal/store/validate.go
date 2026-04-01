package store

import (
	"fmt"
	"regexp"
	"strings"
)

// Name validation patterns.
var (
	// validName matches alphanumeric, dash, underscore. Used for project names,
	// environment names, user names, store names.
	validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

	// validEnvVarName matches POSIX environment variable names.
	validEnvVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// MaxSecretValueSize is the maximum size of a secret value (1 MB).
const MaxSecretValueSize = 1 * 1024 * 1024

// ValidateName checks that a name is safe for use as a directory name.
// Used for project names, environment names, and user names.
func ValidateName(name, kind string) error {
	if name == "" {
		return fmt.Errorf("%s name cannot be empty", kind)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("%s name cannot contain '..'", kind)
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("%s name cannot contain path separators", kind)
	}
	if !validName.MatchString(name) {
		return fmt.Errorf("%s name %q contains invalid characters (use alphanumeric, dash, underscore, dot)", kind, name)
	}
	return nil
}

// ValidateScopePath checks that a scope path is safe (e.g. "dev/runtime").
// The first segment (environment) may be "*" for wildcard environments.
func ValidateScopePath(scopePath string) error {
	if scopePath == "" {
		return fmt.Errorf("scope path cannot be empty")
	}
	if strings.Contains(scopePath, "..") {
		return fmt.Errorf("scope path cannot contain '..'")
	}
	parts := strings.Split(scopePath, "/")
	if len(parts) < 2 {
		return fmt.Errorf("scope path must contain at least env/name (e.g. dev/runtime)")
	}
	for i, part := range parts {
		// Allow "*" as the environment segment (first part).
		if i == 0 && part == "*" {
			continue
		}
		if err := ValidateName(part, "scope segment"); err != nil {
			return err
		}
	}
	return nil
}

// ValidateEnvVarName checks that a string is a valid environment variable name.
func ValidateEnvVarName(name string) error {
	if !validEnvVarName.MatchString(name) {
		return fmt.Errorf("invalid environment variable name %q", name)
	}
	return nil
}

// ValidateSecretValue checks that a secret value is within size limits.
func ValidateSecretValue(value string) error {
	if len(value) > MaxSecretValueSize {
		return fmt.Errorf("secret value too large (%d bytes, max %d)", len(value), MaxSecretValueSize)
	}
	return nil
}
