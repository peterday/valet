package store

import (
	"fmt"
	"strings"
)

// StoreURI holds a parsed store reference.
// Format: github:org/repo[/project] or a local name.
type StoreURI struct {
	Raw       string // original input
	Remote    string // git remote URL (empty for local)
	StoreName string // local directory name
	Project   string // project within the store (empty = default/single project)
	IsRemote  bool
}

// ParseStoreURI parses a store reference string.
//
//	"github:acme/api-secrets"       → remote store, project defaults to store name
//	"github:acme/secrets/api"       → remote store "secrets", project "api"
//	"github:pday/my-keys"           → remote store
//	"my-keys"                       → local store by name
//	"."                             → embedded store
func ParseStoreURI(ref string) StoreURI {
	ref = strings.TrimSpace(ref)

	if ref == "." || ref == "" {
		return StoreURI{Raw: ref, StoreName: "."}
	}

	if strings.HasPrefix(ref, "github:") {
		path := strings.TrimPrefix(ref, "github:")
		parts := strings.Split(path, "/")

		switch len(parts) {
		case 2:
			// github:org/repo — store name = repo, no explicit project
			storeName := parts[1]
			remote := fmt.Sprintf("git@github.com:%s/%s.git", parts[0], parts[1])
			return StoreURI{
				Raw:       ref,
				Remote:    remote,
				StoreName: storeName,
				IsRemote:  true,
			}
		case 3:
			// github:org/repo/project — store name = repo, project explicit
			storeName := parts[1]
			remote := fmt.Sprintf("git@github.com:%s/%s.git", parts[0], parts[1])
			return StoreURI{
				Raw:       ref,
				Remote:    remote,
				StoreName: storeName,
				Project:   parts[2],
				IsRemote:  true,
			}
		default:
			// Invalid, treat as-is
			return StoreURI{Raw: ref, StoreName: ref}
		}
	}

	// Git URL (git@... or https://...)
	if strings.HasPrefix(ref, "git@") || strings.HasPrefix(ref, "https://") {
		name := inferNameFromGitURL(ref)
		return StoreURI{
			Raw:       ref,
			Remote:    ref,
			StoreName: name,
			IsRemote:  true,
		}
	}

	// Local name, possibly with project: "my-store/my-project"
	if strings.Contains(ref, "/") {
		parts := strings.SplitN(ref, "/", 2)
		return StoreURI{
			Raw:       ref,
			StoreName: parts[0],
			Project:   parts[1],
		}
	}

	// Plain local name
	return StoreURI{Raw: ref, StoreName: ref}
}

// GitRemote returns the git remote URL for cloning.
func (u StoreURI) GitRemote() string {
	return u.Remote
}

// EffectiveProject returns the project name — explicit if set, otherwise the store name.
func (u StoreURI) EffectiveProject() string {
	if u.Project != "" {
		return u.Project
	}
	return u.StoreName
}

func inferNameFromGitURL(url string) string {
	// git@github.com:acme/api-secrets.git → api-secrets
	// https://github.com/acme/api-secrets.git → api-secrets
	parts := strings.Split(url, "/")
	name := parts[len(parts)-1]
	return strings.TrimSuffix(name, ".git")
}
