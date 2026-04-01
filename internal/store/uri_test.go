package store

import "testing"

func TestParseStoreURI(t *testing.T) {
	tests := []struct {
		input     string
		storeName string
		project   string
		remote    string
		isRemote  bool
	}{
		// Embedded
		{".", ".", "", "", false},
		{"", ".", "", "", false},

		// Local name
		{"my-keys", "my-keys", "", "", false},

		// Local name with project
		{"my-store/api", "my-store", "api", "", false},

		// GitHub: org/repo (no project)
		{"github:acme/api-secrets", "api-secrets", "", "git@github.com:acme/api-secrets.git", true},

		// GitHub: org/repo/project
		{"github:acme/secrets/api", "secrets", "api", "git@github.com:acme/secrets.git", true},

		// GitHub: personal
		{"github:pday/my-keys", "my-keys", "", "git@github.com:pday/my-keys.git", true},

		// Full git URL
		{"git@github.com:acme/secrets.git", "secrets", "", "git@github.com:acme/secrets.git", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			u := ParseStoreURI(tt.input)
			if u.StoreName != tt.storeName {
				t.Errorf("StoreName: got %q, want %q", u.StoreName, tt.storeName)
			}
			if u.Project != tt.project {
				t.Errorf("Project: got %q, want %q", u.Project, tt.project)
			}
			if u.Remote != tt.remote {
				t.Errorf("Remote: got %q, want %q", u.Remote, tt.remote)
			}
			if u.IsRemote != tt.isRemote {
				t.Errorf("IsRemote: got %v, want %v", u.IsRemote, tt.isRemote)
			}
		})
	}
}

func TestEffectiveProject(t *testing.T) {
	// Explicit project
	u := ParseStoreURI("github:acme/secrets/api")
	if u.EffectiveProject() != "api" {
		t.Errorf("got %q, want %q", u.EffectiveProject(), "api")
	}

	// No project — defaults to store name
	u = ParseStoreURI("github:acme/api-secrets")
	if u.EffectiveProject() != "api-secrets" {
		t.Errorf("got %q, want %q", u.EffectiveProject(), "api-secrets")
	}

	// Local name
	u = ParseStoreURI("my-keys")
	if u.EffectiveProject() != "my-keys" {
		t.Errorf("got %q, want %q", u.EffectiveProject(), "my-keys")
	}
}
