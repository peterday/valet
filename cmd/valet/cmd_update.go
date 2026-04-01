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
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/provider"
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

		// Determine install location.
		// Prefer updating in place if the current binary is user-writable.
		// Otherwise install to ~/.valet/bin/.
		binaryPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("finding current binary: %w", err)
		}
		binaryPath, err = resolveSymlink(binaryPath)
		if err != nil {
			return err
		}

		// If current binary isn't writable, use ~/.valet/bin/ instead.
		if checkWritable(binaryPath) != nil {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			binaryPath = filepath.Join(home, ".valet", "bin", "valet")
			if err := os.MkdirAll(filepath.Dir(binaryPath), 0755); err != nil {
				return err
			}
		}

		fmt.Printf("Downloading v%s...\n", latest)
		newBinary, err := downloadAndExtract(downloadURL)
		if err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
		defer os.Remove(newBinary)

		// Copy instead of rename to handle cross-filesystem.
		if err := copyFile(newBinary, binaryPath); err != nil {
			return fmt.Errorf("install failed: %w", err)
		}
		os.Chmod(binaryPath, 0755)

		// On macOS, re-sign the binary to prevent Gatekeeper issues.
		if runtime.GOOS == "darwin" {
			exec.Command("codesign", "-s", "-", "-f", binaryPath).Run()
		}

		fmt.Printf("Updated to v%s (%s)\n", latest, binaryPath)

		// Update provider registry (best-effort).
		updateProviders()

		// Check if the install location is in PATH.
		installDir := filepath.Dir(binaryPath)
		if !strings.Contains(os.Getenv("PATH"), installDir) {
			fmt.Printf("\nNote: %s is not in your PATH.\n", installDir)
			fmt.Printf("Add it: export PATH=\"%s:$PATH\"\n", installDir)
		}

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
		return path, nil
	}
	if !strings.HasPrefix(resolved, "/") {
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

// copyFile copies src to dst, handling cross-filesystem moves.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// updateProviders pulls the latest provider registry (best-effort).
func updateProviders() {
	defaultDir := provider.DefaultRegistryDir()
	if _, err := os.Stat(filepath.Join(defaultDir, ".git")); os.IsNotExist(err) {
		// Not cloned yet — clone it.
		fmt.Println("Fetching provider registry...")
		baseDir := provider.ProvidersBaseDir()
		os.MkdirAll(baseDir, 0755)
		cmd := exec.Command("git", "clone", "--depth", "1", provider.DefaultRegistry, defaultDir)
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not fetch providers: %v\n", err)
			return
		}
		fmt.Println("Provider registry installed.")
		return
	}

	// Already cloned — pull.
	fmt.Println("Updating provider registry...")
	cmd := exec.Command("git", "-C", defaultDir, "pull", "--ff-only")
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not update providers: %v\n", err)
		return
	}
	fmt.Println("Provider registry updated.")
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheckFlag, "check", false, "just check for updates, don't install")
	rootCmd.AddCommand(updateCmd)
}
