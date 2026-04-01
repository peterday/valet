package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var updateCheckFlag bool

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update valet to the latest version",
	Long: `Check for and install the latest version of valet.

  valet update           # download and install latest
  valet update --check   # just check, don't install`,
	RunE: func(cmd *cobra.Command, args []string) error {
		repo := "peterday/valet"

		latest, downloadURL, err := getLatestRelease(repo)
		if err != nil {
			return fmt.Errorf("checking for updates: %w", err)
		}

		current := version
		if current == "" {
			current = "dev"
		}

		if current == latest {
			fmt.Printf("Already up to date (v%s)\n", current)
			return nil
		}

		fmt.Printf("Current: v%s → Latest: v%s\n", current, latest)

		if updateCheckFlag {
			fmt.Println("Run `valet update` to install.")
			return nil
		}

		// Find where the current binary lives.
		binaryPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("finding current binary: %w", err)
		}
		binaryPath, err = resolveSymlink(binaryPath)
		if err != nil {
			return err
		}

		fmt.Printf("Downloading v%s...\n", latest)
		newBinary, err := downloadAndExtract(downloadURL)
		if err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
		defer os.Remove(newBinary)

		// Check if we can write to the binary location.
		if err := checkWritable(binaryPath); err != nil {
			// Need sudo.
			fmt.Printf("Installing to %s (requires sudo)...\n", binaryPath)
			if err := sudoMove(newBinary, binaryPath); err != nil {
				return fmt.Errorf("install failed: %w", err)
			}
		} else {
			if err := os.Rename(newBinary, binaryPath); err != nil {
				return fmt.Errorf("install failed: %w", err)
			}
		}

		if err := os.Chmod(binaryPath, 0755); err != nil {
			return fmt.Errorf("chmod: %w", err)
		}

		fmt.Printf("Updated to v%s\n", latest)
		return nil
	},
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func getLatestRelease(repo string) (version, downloadURL string, err error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	resp, err := http.Get(url)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", err
	}

	ver := strings.TrimPrefix(release.TagName, "v")

	// Find the right asset for this platform.
	wantName := fmt.Sprintf("valet_%s_%s_%s.tar.gz", ver, runtime.GOOS, runtime.GOARCH)
	for _, asset := range release.Assets {
		if asset.Name == wantName {
			return ver, asset.BrowserDownloadURL, nil
		}
	}

	return ver, "", fmt.Errorf("no binary found for %s/%s in release v%s", runtime.GOOS, runtime.GOARCH, ver)
}

func downloadAndExtract(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download returned %d", resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		if header.Name == "valet" && header.Typeflag == tar.TypeReg {
			tmpFile, err := os.CreateTemp("", "valet-update-*")
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(tmpFile, tr); err != nil {
				tmpFile.Close()
				return "", err
			}
			tmpFile.Close()
			os.Chmod(tmpFile.Name(), 0755)
			return tmpFile.Name(), nil
		}
	}

	return "", fmt.Errorf("valet binary not found in archive")
}

func resolveSymlink(path string) (string, error) {
	resolved, err := os.Readlink(path)
	if err != nil {
		return path, nil // not a symlink
	}
	if !strings.HasPrefix(resolved, "/") {
		// Relative symlink — resolve against the directory.
		dir := path[:strings.LastIndex(path, "/")]
		resolved = dir + "/" + resolved
	}
	return resolved, nil
}

func checkWritable(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	f.Close()
	return nil
}

func sudoMove(src, dst string) error {
	cmd := exec.Command("sudo", "mv", src, dst)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheckFlag, "check", false, "just check for updates, don't install")
	rootCmd.AddCommand(updateCmd)
}
