package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/peterday/valet/internal/domain"
)

// CreateProject creates a new project in the store.
func (s *Store) CreateProject(name string) (*domain.Project, error) {
	if err := ValidateName(name, "project"); err != nil {
		return nil, err
	}
	slug := name
	dir := s.projectRoot(slug)

	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("project %q already exists", slug)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	p := &domain.Project{
		Name:      name,
		Slug:      slug,
		CreatedAt: time.Now().UTC(),
	}

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(filepath.Join(dir, "project.json"), data, 0644); err != nil {
		return nil, err
	}

	return p, nil
}

// GetProject reads a project by slug.
func (s *Store) GetProject(slug string) (*domain.Project, error) {
	data, err := os.ReadFile(filepath.Join(s.projectRoot(slug), "project.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("project %q not found", slug)
		}
		return nil, err
	}

	var p domain.Project
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ListProjects lists all projects in the store.
func (s *Store) ListProjects() ([]domain.Project, error) {
	dir := filepath.Join(s.Root, "projects")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var projects []domain.Project
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p, err := s.GetProject(e.Name())
		if err != nil {
			continue
		}
		projects = append(projects, *p)
	}
	return projects, nil
}
