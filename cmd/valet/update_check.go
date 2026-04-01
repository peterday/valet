package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var execCommand = osexec.Command

// updateCheckResult is cached to disk.
type updateCheckResult struct {
	LatestVersion string    `json:"latest_version"`
	CheckedAt     time.Time `json:"checked_at"`
}

const updateCheckInterval = 24 * time.Hour

var (
	updateNotice     string
	updateNoticeOnce sync.Once
)

// startUpdateCheck kicks off a background update check.
// Call this early in command execution; call printUpdateNotice at the end.
func startUpdateCheck() {
	// Skip in non-interactive contexts.
	if os.Getenv("VALET_KEY") != "" {
		return // bot/CI mode
	}
	if version == "dev" {
		return // local build
	}

	go func() {
		updateNoticeOnce.Do(func() {
			latest := checkForUpdate()
			if latest != "" && latest != version {
				if isAutoUpdateEnabled() {
					// Auto-update: run valet update in background.
					fmt.Fprintf(os.Stderr, "Auto-updating valet v%s → v%s...\n", version, latest)
					valetPath, err := os.Executable()
					if err != nil {
						return
					}
					cmd := execCommand(valetPath, "update")
					cmd.Stderr = os.Stderr
					if err := cmd.Run(); err != nil {
						fmt.Fprintf(os.Stderr, "Auto-update failed: %v. Run manually: valet update\n", err)
					}
				} else {
					updateNotice = fmt.Sprintf("\nUpdate available: v%s → v%s. Run: valet update\n", version, latest)
				}
			}
		})
	}()
}

func isAutoUpdateEnabled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(home, ".valet", "config.json"))
	if err != nil {
		return false
	}
	var cfg struct {
		AutoUpdate bool `json:"auto_update"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return false
	}
	return cfg.AutoUpdate
}

// printUpdateNotice prints the update notice if one is available.
// Safe to call even if startUpdateCheck was never called.
func printUpdateNotice() {
	// Give the background check a moment to finish, but don't block.
	time.Sleep(50 * time.Millisecond)
	if updateNotice != "" {
		fmt.Fprint(os.Stderr, updateNotice)
	}
}

func checkForUpdate() string {
	cacheFile := updateCheckCachePath()

	// Check cache first.
	if data, err := os.ReadFile(cacheFile); err == nil {
		var cached updateCheckResult
		if json.Unmarshal(data, &cached) == nil {
			if time.Since(cached.CheckedAt) < updateCheckInterval {
				return cached.LatestVersion
			}
		}
	}

	// Fetch latest version from GitHub.
	latest := fetchLatestVersion()
	if latest == "" {
		return ""
	}

	// Cache the result.
	result := updateCheckResult{
		LatestVersion: latest,
		CheckedAt:     time.Now(),
	}
	if data, err := json.Marshal(result); err == nil {
		os.MkdirAll(filepath.Dir(cacheFile), 0755)
		os.WriteFile(cacheFile, data, 0644)
	}

	return latest
}

func fetchLatestVersion() string {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/peterday/valet/releases/latest")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return ""
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return ""
	}

	return strings.TrimPrefix(release.TagName, "v")
}

func updateCheckCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".valet", "update-check.json")
}
