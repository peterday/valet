package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// CreateEnvironment creates an environment directory under a project.
func (s *Store) CreateEnvironment(projectSlug, envName string) error {
	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return err
	}

	dir := filepath.Join(s.projectRoot(slug), envName)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("environment %q already exists in project %q", envName, slug)
	}

	return os.MkdirAll(dir, 0755)
}

// ListEnvironments lists all environments for a project.
func (s *Store) ListEnvironments(projectSlug string) ([]string, error) {
	slug, err := s.resolveProject(projectSlug)
	if err != nil {
		return nil, err
	}

	dir := s.projectRoot(slug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var envs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Check if this directory contains scopes (has subdirs or is a leaf with manifest.json).
		// Environment directories sit directly under the project dir.
		// We skip "hidden" dirs.
		name := e.Name()
		if name[0] == '.' {
			continue
		}
		envs = append(envs, name)
	}
	return envs, nil
}
