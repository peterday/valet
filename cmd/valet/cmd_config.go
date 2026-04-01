package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// valetGlobalConfig is stored at ~/.valet/config.json.
type valetGlobalConfig struct {
	AutoUpdate bool `json:"auto_update"`
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View or update valet configuration",
	Long: `View or update global valet settings.

  valet config                           # show current config
  valet config autoupdate on             # enable automatic updates
  valet config autoupdate off            # disable (show notice instead)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadGlobalConfig()
		autoUpdate := "off"
		if cfg.AutoUpdate {
			autoUpdate = "on"
		}
		fmt.Printf("auto_update: %s\n", autoUpdate)
		fmt.Printf("\nConfig file: %s\n", globalConfigPath())
		return nil
	},
}

var configAutoUpdateCmd = &cobra.Command{
	Use:   "autoupdate <on|off>",
	Short: "Enable or disable automatic updates",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "on", "true", "yes":
			return setAutoUpdate(true)
		case "off", "false", "no":
			return setAutoUpdate(false)
		default:
			return fmt.Errorf("use 'on' or 'off'")
		}
	},
}

func setAutoUpdate(enabled bool) error {
	cfg := loadGlobalConfig()
	cfg.AutoUpdate = enabled

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	path := globalConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}

	if enabled {
		fmt.Println("Auto-update enabled. Valet will update automatically when a new version is available.")
	} else {
		fmt.Println("Auto-update disabled. You'll see a notice when updates are available.")
	}
	return nil
}

func loadGlobalConfig() valetGlobalConfig {
	var cfg valetGlobalConfig
	data, err := os.ReadFile(globalConfigPath())
	if err != nil {
		return cfg
	}
	json.Unmarshal(data, &cfg)
	return cfg
}

func globalConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".valet", "config.json")
}

func init() {
	configCmd.AddCommand(configAutoUpdateCmd)
	rootCmd.AddCommand(configCmd)
}
