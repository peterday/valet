package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindValetConfig_ValetToml(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ValetToml), []byte("store = '.'"), 0644)

	path, isLocal, err := FindValetConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if isLocal {
		t.Error("expected isLocal=false")
	}
	if filepath.Base(path) != ValetToml {
		t.Errorf("expected %s, got %s", ValetToml, filepath.Base(path))
	}
}

func TestFindValetConfig_LocalOnly(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ValetLocalToml), []byte("[[stores]]\nname = 'my-keys'"), 0644)

	path, isLocal, err := FindValetConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !isLocal {
		t.Error("expected isLocal=true")
	}
	if filepath.Base(path) != ValetLocalToml {
		t.Errorf("expected %s, got %s", ValetLocalToml, filepath.Base(path))
	}
}

func TestFindValetConfig_PreferValetToml(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ValetToml), []byte("store = '.'"), 0644)
	os.WriteFile(filepath.Join(dir, ValetLocalToml), []byte("[[stores]]\nname = 'x'"), 0644)

	path, isLocal, err := FindValetConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if isLocal {
		t.Error("should prefer .valet.toml over .valet.local.toml")
	}
	if filepath.Base(path) != ValetToml {
		t.Errorf("expected %s", ValetToml)
	}
}

func TestFindValetConfig_NotFound(t *testing.T) {
	dir := t.TempDir()

	_, _, err := FindValetConfig(dir)
	if err == nil {
		t.Error("expected error when no config file exists")
	}
}

func TestFindValetConfig_WalksUp(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ValetToml), []byte("store = '.'"), 0644)

	sub := filepath.Join(root, "src", "app")
	os.MkdirAll(sub, 0755)

	path, _, err := FindValetConfig(sub)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != root {
		t.Errorf("expected to find config in %s, got %s", root, filepath.Dir(path))
	}
}

func TestLoadValetToml_DefaultEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ValetToml)
	os.WriteFile(path, []byte("store = '.'\nproject = 'test'"), 0644)

	vc, err := LoadValetToml(path)
	if err != nil {
		t.Fatal(err)
	}

	if vc.DefaultEnv != "dev" {
		t.Errorf("expected default env 'dev', got %q", vc.DefaultEnv)
	}
}

func TestLoadLocalConfig_Empty(t *testing.T) {
	dir := t.TempDir()
	// No .valet.local.toml → returns empty config.
	lc, err := LoadLocalConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lc.Stores) != 0 {
		t.Error("expected empty stores")
	}
}

func TestLoadLocalConfig_WithRequires(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ValetLocalToml)
	os.WriteFile(path, []byte(`
[[stores]]
name = "my-keys"

[requires]
[requires.PORT]
description = "app port"
`), 0644)

	lc, err := LoadLocalConfig(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(lc.Stores) != 1 {
		t.Errorf("expected 1 store, got %d", len(lc.Stores))
	}
	if _, ok := lc.Requires["PORT"]; !ok {
		t.Error("expected PORT in requires")
	}
}
