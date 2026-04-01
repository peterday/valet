package config

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/peterday/valet/internal/domain"
)

const (
	DefaultPort    = 9876
	ValetDir       = ".valet"
	ValetToml      = ".valet.toml"
	ValetLocalToml = ".valet.local.toml"
)

type Config struct {
	DataDir     string // ~/.valet
	IdentityDir string // ~/.valet/identity
	StoresDir   string // ~/.valet/stores
	Port        int
}

// Load returns the valet config, creating ~/.valet/ if needed.
func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}

	dataDir := filepath.Join(home, ValetDir)
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	return &Config{
		DataDir:     dataDir,
		IdentityDir: filepath.Join(dataDir, "identity"),
		StoresDir:   filepath.Join(dataDir, "stores"),
		Port:        DefaultPort,
	}, nil
}

// LoadValetToml reads and parses a .valet.toml file.
func LoadValetToml(path string) (*domain.ValetConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var vc domain.ValetConfig
	if err := toml.Unmarshal(data, &vc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if vc.DefaultEnv == "" {
		vc.DefaultEnv = "dev"
	}

	return &vc, nil
}

// WriteValetToml writes a .valet.toml file.
func WriteValetToml(path string, vc *domain.ValetConfig) error {
	data, err := toml.Marshal(vc)
	if err != nil {
		return fmt.Errorf("marshal valet config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// FindValetToml walks up from dir looking for .valet.toml, returning its path.
func FindValetToml(dir string) (string, error) {
	for {
		path := filepath.Join(dir, ValetToml)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s found in %s or any parent directory", ValetToml, dir)
		}
		dir = parent
	}
}

// LoadLocalConfig reads .valet.local.toml from the same directory as .valet.toml.
func LoadLocalConfig(tomlDir string) (*domain.LocalConfig, error) {
	path := filepath.Join(tomlDir, ValetLocalToml)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &domain.LocalConfig{}, nil
		}
		return nil, err
	}

	var lc domain.LocalConfig
	if err := toml.Unmarshal(data, &lc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &lc, nil
}

// WriteLocalConfig writes .valet.local.toml.
func WriteLocalConfig(tomlDir string, lc *domain.LocalConfig) error {
	data, err := toml.Marshal(lc)
	if err != nil {
		return err
	}
	path := filepath.Join(tomlDir, ValetLocalToml)
	return os.WriteFile(path, data, 0644)
}
