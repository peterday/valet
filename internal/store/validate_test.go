package store

import (
	"strings"
	"testing"
)

func TestValidateName_Valid(t *testing.T) {
	valid := []string{"my-app", "prod", "dev", "user_1", "A", "test.v2", "123"}
	for _, name := range valid {
		if err := ValidateName(name, "test"); err != nil {
			t.Errorf("expected %q to be valid, got: %v", name, err)
		}
	}
}

func TestValidateName_Invalid(t *testing.T) {
	cases := []struct {
		name   string
		reason string
	}{
		{"", "empty"},
		{"../escape", "path traversal"},
		{"foo/bar", "path separator"},
		{"foo\\bar", "backslash"},
		{".hidden", "starts with dot"},
		{"-leading", "starts with dash"},
		{"has spaces", "spaces"},
		{"has@symbol", "special chars"},
	}
	for _, tc := range cases {
		if err := ValidateName(tc.name, "test"); err == nil {
			t.Errorf("expected %q (%s) to be invalid", tc.name, tc.reason)
		}
	}
}

func TestValidateScopePath_Valid(t *testing.T) {
	valid := []string{"dev/runtime", "prod/db", "dev/default", "*/default", "staging/integrations"}
	for _, path := range valid {
		if err := ValidateScopePath(path); err != nil {
			t.Errorf("expected %q to be valid, got: %v", path, err)
		}
	}
}

func TestValidateScopePath_Invalid(t *testing.T) {
	cases := []struct {
		path   string
		reason string
	}{
		{"", "empty"},
		{"dev", "single segment"},
		{"../escape/secret", "path traversal"},
		{"dev/../../etc", "embedded traversal"},
		{"dev/ spaces", "spaces in segment"},
	}
	for _, tc := range cases {
		if err := ValidateScopePath(tc.path); err == nil {
			t.Errorf("expected %q (%s) to be invalid", tc.path, tc.reason)
		}
	}
}

func TestValidateEnvVarName_Valid(t *testing.T) {
	valid := []string{"API_KEY", "DATABASE_URL", "_PRIVATE", "a", "MY_VAR_123"}
	for _, name := range valid {
		if err := ValidateEnvVarName(name); err != nil {
			t.Errorf("expected %q to be valid, got: %v", name, err)
		}
	}
}

func TestValidateEnvVarName_Invalid(t *testing.T) {
	invalid := []string{"", "123START", "HAS SPACE", "has-dash", "has.dot", "a=b"}
	for _, name := range invalid {
		if err := ValidateEnvVarName(name); err == nil {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}

func TestValidateSecretValue_WithinLimit(t *testing.T) {
	if err := ValidateSecretValue("short value"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSecretValue(""); err != nil {
		t.Fatal("empty value should be valid")
	}
}

func TestValidateSecretValue_ExceedsLimit(t *testing.T) {
	huge := strings.Repeat("x", MaxSecretValueSize+1)
	if err := ValidateSecretValue(huge); err == nil {
		t.Fatal("expected error for oversized value")
	}
}

func TestCreateProject_PathTraversal(t *testing.T) {
	s, _ := setupTestStore(t)
	_, err := s.CreateProject("../../escape")
	if err == nil {
		t.Fatal("expected error for path traversal in project name")
	}
}

func TestCreateEnvironment_PathTraversal(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	err := s.CreateEnvironment("app", "../../escape")
	if err == nil {
		t.Fatal("expected error for path traversal in env name")
	}
}

func TestCreateEnvironment_WildcardAllowed(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	err := s.CreateEnvironment("app", "*")
	if err != nil {
		t.Fatalf("wildcard env should be allowed: %v", err)
	}
}

func TestCreateScope_PathTraversal(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	err := s.CreateScope("app", "../../escape/secret")
	if err == nil {
		t.Fatal("expected error for path traversal in scope path")
	}
}

func TestSetSecret_InvalidKeyName(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/default")
	err := s.SetSecret("app", "dev/default", "INVALID KEY", "value")
	if err == nil {
		t.Fatal("expected error for invalid env var name")
	}
}

func TestSetSecret_OversizedValue(t *testing.T) {
	s, _ := setupTestStore(t)
	s.CreateProject("app")
	s.CreateEnvironment("app", "dev")
	s.CreateScope("app", "dev/default")
	huge := strings.Repeat("x", MaxSecretValueSize+1)
	err := s.SetSecret("app", "dev/default", "KEY", huge)
	if err == nil {
		t.Fatal("expected error for oversized secret value")
	}
}

func TestAddUser_PathTraversal(t *testing.T) {
	s, _ := setupTestStore(t)
	_, err := s.AddUser("../../escape", "", "age1fake")
	if err == nil {
		t.Fatal("expected error for path traversal in user name")
	}
}

func TestAddUser_EmptyPublicKey(t *testing.T) {
	s, _ := setupTestStore(t)
	_, err := s.AddUser("alice", "", "")
	if err == nil {
		t.Fatal("expected error for empty public key")
	}
}
