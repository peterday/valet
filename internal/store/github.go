package store

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/peterday/valet/internal/domain"
)

// githubKey is the JSON response from GitHub's /users/:user/keys API.
type githubKey struct {
	ID        int    `json:"id"`
	Key       string `json:"key"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	LastUsed  string `json:"last_used"`
}

// FetchGitHubKeys fetches all SSH public keys for a GitHub user via the API,
// returning labeled UserKey structs. Falls back to the .keys endpoint if the
// API fails (e.g. rate limited).
func FetchGitHubKeys(username string) ([]domain.UserKey, error) {
	keys, err := fetchGitHubKeysAPI(username)
	if err == nil && len(keys) > 0 {
		return keys, nil
	}

	// Fallback to .keys endpoint (no labels).
	return fetchGitHubKeysPlain(username)
}

func fetchGitHubKeysAPI(username string) ([]domain.UserKey, error) {
	url := fmt.Sprintf("https://api.github.com/users/%s/keys", username)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "valet")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("GitHub user %q not found", username)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var ghKeys []githubKey
	if err := json.Unmarshal(body, &ghKeys); err != nil {
		return nil, fmt.Errorf("parsing GitHub response: %w", err)
	}

	var keys []domain.UserKey
	for _, gk := range ghKeys {
		k := strings.TrimSpace(gk.Key)
		if k == "" {
			continue
		}
		label := gk.Title
		if label == "" && gk.CreatedAt != "" {
			// No title from public API — use creation date as label.
			label = "added " + gk.CreatedAt[:10]
			if gk.LastUsed != "" {
				label += ", last used " + gk.LastUsed[:10]
			}
		}
		keys = append(keys, domain.UserKey{
			Key:    k,
			Label:  label,
			Source: "github",
		})
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("no SSH keys found for @%s — they need to add an SSH key to GitHub", username)
	}

	return keys, nil
}

func fetchGitHubKeysPlain(username string) ([]domain.UserKey, error) {
	url := fmt.Sprintf("https://github.com/%s.keys", username)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("GitHub user %q not found", username)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var keys []domain.UserKey
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && (strings.HasPrefix(line, "ssh-") || strings.HasPrefix(line, "ecdsa-")) {
			keys = append(keys, domain.UserKey{Key: line, Source: "github"})
		}
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("no SSH keys found for @%s", username)
	}

	return keys, nil
}
