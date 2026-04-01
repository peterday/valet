package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/peterday/valet/internal/domain"
)

// AddUser adds a user to the store.
func (s *Store) AddUser(name, github, publicKey string) (*domain.User, error) {
	if err := ValidateName(name, "user"); err != nil {
		return nil, err
	}

	// Validate public key format.
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return nil, fmt.Errorf("public key cannot be empty")
	}

	dir := filepath.Join(s.Root, "users")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	path := filepath.Join(dir, name+".json")
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("user %q already exists", name)
	}

	u := &domain.User{
		Name:      name,
		GitHub:    github,
		PublicKey: publicKey,
		CreatedAt: time.Now().UTC(),
	}

	data, err := json.MarshalIndent(u, "", "  ")
	if err != nil {
		return nil, err
	}

	return u, os.WriteFile(path, data, 0644)
}

// GetUser reads a user by name.
func (s *Store) GetUser(name string) (*domain.User, error) {
	data, err := os.ReadFile(filepath.Join(s.Root, "users", name+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("user %q not found", name)
		}
		return nil, err
	}

	var u domain.User
	if err := json.Unmarshal(data, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// ListUsers lists all users in the store.
func (s *Store) ListUsers() ([]domain.User, error) {
	dir := filepath.Join(s.Root, "users")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var users []domain.User
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		u, err := s.GetUser(name)
		if err != nil {
			continue
		}
		users = append(users, *u)
	}
	return users, nil
}

// RemoveUser removes a user from the store.
func (s *Store) RemoveUser(name string) error {
	path := filepath.Join(s.Root, "users", name+".json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("user %q not found", name)
	}
	return os.Remove(path)
}
