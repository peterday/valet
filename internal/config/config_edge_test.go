package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/peterday/valet/internal/domain"
)

func TestWriteAndReadValetToml_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ValetToml)

	vc := &domain.ValetConfig{
		Store:      ".",
		Project:    "myproject",
		DefaultEnv: "dev",
		Requires: map[string]domain.Requirement{
			"KEY1": {Provider: "openai", Description: "test"},
			"KEY2": {Optional: true},
		},
	}

	if err := WriteValetToml(path, vc); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadValetToml(path)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.Store != "." {
		t.Errorf("store=%q", loaded.Store)
	}
	if loaded.Project != "myproject" {
		t.Errorf("project=%q", loaded.Project)
	}
	if len(loaded.Requires) != 2 {
		t.Errorf("expected 2 requires, got %d", len(loaded.Requires))
	}
	if loaded.Requires["KEY1"].Provider != "openai" {
		t.Error("KEY1 provider lost")
	}
	if !loaded.Requires["KEY2"].Optional {
		t.Error("KEY2 optional lost")
	}
}

func TestWriteAndReadLocalConfig_Roundtrip(t *testing.T) {
	dir := t.TempDir()

	lc := &domain.LocalConfig{
		Stores: []domain.StoreLink{{Name: "my-keys"}},
		Requires: map[string]domain.Requirement{
			"PORT": {Description: "app port"},
		},
	}

	if err := WriteLocalConfig(dir, lc); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadLocalConfig(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded.Stores) != 1 || loaded.Stores[0].Name != "my-keys" {
		t.Errorf("stores: %v", loaded.Stores)
	}
	if _, ok := loaded.Requires["PORT"]; !ok {
		t.Error("PORT requires lost")
	}
}

func TestLoadValetToml_NonExistent(t *testing.T) {
	_, err := LoadValetToml("/nonexistent/path/.valet.toml")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestFindValetToml_WalksUp(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ValetToml), []byte("store = '.'"), 0644)

	sub := filepath.Join(root, "a", "b", "c")
	os.MkdirAll(sub, 0755)

	path, err := FindValetToml(sub)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != root {
		t.Errorf("expected root dir, got %s", filepath.Dir(path))
	}
}

func TestFindValetToml_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := FindValetToml(dir)
	if err == nil {
		t.Error("expected error")
	}
}

func TestWriteValetToml_HasHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ValetToml)

	vc := &domain.ValetConfig{Store: ".", Project: "test"}
	WriteValetToml(path, vc)

	data, _ := os.ReadFile(path)
	content := string(data)

	if len(content) < 10 {
		t.Fatal("file too short")
	}
	// Should start with a comment header.
	if content[0] != '#' {
		t.Error("expected file to start with # header")
	}
}
