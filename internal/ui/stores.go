package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/store"
)

// storeListItem is a row in the store list page.
type storeListItem struct {
	Name          string
	Type          domain.StoreType
	SecretCount   int
	Environments  []string
	IsProject     bool
	RotationCount int
	UserCount     int
}

func (s *Server) handleStores(w http.ResponseWriter, r *http.Request) {
	allStores := s.listAllStores()

	var items []storeListItem
	for _, st := range allStores {
		item := storeListItem{
			Name: st.Meta.Name,
			Type: st.Meta.Type,
		}

		project, err := st.ResolveDefaultProject()
		if err != nil {
			items = append(items, item)
			continue
		}

		envs, _ := st.ListEnvironments(project)
		item.Environments = envs

		// Count secrets across all environments.
		seen := make(map[string]bool)
		for _, env := range envs {
			secrets, _ := st.ListSecretsInEnv(project, env)
			for k := range secrets {
				seen[k] = true
			}
		}
		item.SecretCount = len(seen)

		// Count rotation flags.
		allScopes, _ := st.ListAllScopes(project)
		for _, scope := range allScopes {
			manifest := readManifestFile(st.Root, project, scope.Path)
			if manifest != nil {
				item.RotationCount += len(manifest.RotationFlags)
			}
		}

		// Count users.
		users, _ := st.ListUsers()
		item.UserCount = len(users)

		// Check if this is the project's embedded store.
		if s.valetCfg != nil && s.valetCfg.Store == "." && strings.HasSuffix(st.Root, "/.valet") {
			item.IsProject = true
		}

		items = append(items, item)
	}

	s.render(w, "stores.html", map[string]any{
		"Page":   "stores",
		"Stores": items,
	})
}

type secretRow struct {
	Key       string
	Provider  string
	Scope     string
	UpdatedAt string
	UpdatedBy string
	Masked    string
}

// secretGroup is a key shown across all environments (for the "all" view).
type secretGroup struct {
	Key           string
	Provider      string
	EnvRows       []secretEnvRow
	PresentIn     []string // env names where this key exists and user has access
	LockedIn      []string // env names where key exists but user can't decrypt
	MissingIn     []string // env names where this key is absent
	RotationIn    []string // env names where this key needs rotation
	LatestTime    string   // most recent update across envs
	EnvCount      int      // number of envs that have this key
	TotalEnvs     int      // total environments
	NeedsRotation bool     // true if any env needs rotation
}

type secretEnvRow struct {
	Env          string
	Scope        string
	Masked       string
	UpdatedAt    string
	UpdatedBy    string
	Missing      bool   // key doesn't exist in this env at all
	Locked       bool   // key exists but user can't decrypt (not a recipient)
	NeedsRotation bool  // flagged for rotation
	rawTime      time.Time
}

func (s *Server) handleStoreDetail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	env := r.URL.Query().Get("env")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found: "+name, http.StatusNotFound)
		return
	}

	project, err := st.ResolveDefaultProject()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	envs, _ := st.ListEnvironments(project)
	if env == "" {
		env = "all"
	}

	// "all" view: grouped by key, showing presence across envs.
	if env == "all" {
		groups := s.buildAllEnvView(st, project, envs)
		s.render(w, "store_detail_all.html", map[string]any{
			"Page":          "stores",
			"Tab":           "secrets",
			"StoreName":     name,
			"StoreType":     st.Meta.Type,
			"StoreRemote":   storeRemote(st),
			"Environments":  envs,
			"ActiveEnv":     "all",
			"Groups":        groups,
			"ProvidersJSON": s.buildProvidersJSON(),
		})
		return
	}

	// Single-env view.
	var rows []secretRow
	scopes, _ := st.ListScopes(project, env)
	for _, scope := range scopes {
		manifest := readManifestFile(st.Root, project, scope.Path)

		for _, key := range scope.Secrets {
			row := secretRow{
				Key:   key,
				Scope: scope.Path,
			}

			if manifest != nil {
				if p, ok := manifest.Providers[key]; ok {
					row.Provider = p
				}
			}

			if secret, err := st.GetSecret(project, scope.Path, key); err == nil {
				row.Masked = maskSecret(secret.Value)
				row.UpdatedAt = timeAgo(secret.UpdatedAt)
				row.UpdatedBy = secret.UpdatedBy
			}

			rows = append(rows, row)
		}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })

	s.render(w, "store_detail.html", map[string]any{
		"Page":         "stores",
		"Tab":          "secrets",
		"StoreName":    name,
		"StoreType":    st.Meta.Type,
		"StoreRemote":  storeRemote(st),
		"Environments": envs,
		"ActiveEnv":    env,
		"Secrets":      rows,
	})
}

// buildAllEnvView collects all secrets across all environments, grouped by key.
// Uses plaintext manifests to discover keys (works even without decrypt access),
// then attempts decryption for values/metadata.
func (s *Server) buildAllEnvView(st *store.Store, project string, envs []string) []secretGroup {
	type keyInfo struct {
		provider string
		envData  map[string]secretEnvRow
	}
	keyMap := make(map[string]*keyInfo)

	// Check if user is a recipient on each scope (from manifest recipients).
	userPubKey := s.id.PublicKey

	for _, env := range envs {
		scopes, _ := st.ListScopes(project, env)
		for _, scope := range scopes {
			manifest := readManifestFile(st.Root, project, scope.Path)
			if manifest == nil {
				continue
			}

			// Check if the user is a recipient on this scope.
			hasAccess := false
			for _, r := range manifest.Recipients {
				if r.PublicKey == userPubKey {
					hasAccess = true
					break
				}
			}

			for _, key := range manifest.Secrets {
				ki, ok := keyMap[key]
				if !ok {
					ki = &keyInfo{envData: make(map[string]secretEnvRow)}
					keyMap[key] = ki
				}

				if p, ok := manifest.Providers[key]; ok && ki.provider == "" {
					ki.provider = p
				}

				row := secretEnvRow{
					Env:   env,
					Scope: scope.Path,
				}

				// Check rotation flags.
				if manifest.RotationFlags != nil {
					if _, flagged := manifest.RotationFlags[key]; flagged {
						row.NeedsRotation = true
					}
				}

				if hasAccess {
					if secret, err := st.GetSecret(project, scope.Path, key); err == nil {
						row.Masked = maskSecret(secret.Value)
						row.UpdatedAt = timeAgo(secret.UpdatedAt)
						row.UpdatedBy = secret.UpdatedBy
						row.rawTime = secret.UpdatedAt
					}
				} else {
					row.Locked = true
				}

				ki.envData[env] = row
			}
		}
	}

	// Build sorted groups with summary info.
	var groups []secretGroup
	for key, ki := range keyMap {
		g := secretGroup{
			Key:       key,
			Provider:  ki.provider,
			TotalEnvs: len(envs),
		}
		var latestTime time.Time
		for _, env := range envs {
			if row, ok := ki.envData[env]; ok {
				g.EnvRows = append(g.EnvRows, row)
				if row.NeedsRotation {
					g.RotationIn = append(g.RotationIn, env)
					g.NeedsRotation = true
				} else if row.Locked {
					g.LockedIn = append(g.LockedIn, env)
				} else {
					g.PresentIn = append(g.PresentIn, env)
				}
				g.EnvCount++
				if row.rawTime.After(latestTime) {
					latestTime = row.rawTime
					g.LatestTime = row.UpdatedAt
				}
			} else {
				g.EnvRows = append(g.EnvRows, secretEnvRow{
					Env:     env,
					Missing: true,
				})
				g.MissingIn = append(g.MissingIn, env)
			}
		}
		groups = append(groups, g)
	}

	sort.Slice(groups, func(i, j int) bool { return groups[i].Key < groups[j].Key })
	return groups
}

// providerJSON is a provider for the add-secret form JS.
type providerJSON struct {
	Name        string           `json:"name"`
	DisplayName string           `json:"displayName"`
	Category    string           `json:"category"`
	SetupURL    string           `json:"setupUrl"`
	FreeTier    string           `json:"freeTier"`
	EnvVars     []providerEnvVar `json:"envVars"`
}

type providerEnvVar struct {
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
}

func (s *Server) buildProvidersJSON() string {
	all := s.registry.All()
	var providers []providerJSON
	for _, p := range all {
		pj := providerJSON{
			Name:        p.Name,
			DisplayName: p.DisplayName,
			Category:    p.Category,
			SetupURL:    p.SetupURL,
			FreeTier:    p.FreeTier,
		}
		for _, ev := range p.EnvVars {
			prefix := ""
			prefixes := ev.AllPrefixes()
			if len(prefixes) > 0 {
				prefix = prefixes[0]
			}
			pj.EnvVars = append(pj.EnvVars, providerEnvVar{
				Name:   ev.Name,
				Prefix: prefix,
			})
		}
		providers = append(providers, pj)
	}

	// Sort by display name.
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].DisplayName < providers[j].DisplayName
	})

	data, _ := json.Marshal(providers)
	return string(data)
}

func (s *Server) handleSecretReveal(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	key := r.PathValue("key")
	scope := r.FormValue("scope")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, _ := st.ResolveDefaultProject()
	secret, err := st.GetSecret(project, scope, key)
	if err != nil {
		http.Error(w, "secret not found", http.StatusNotFound)
		return
	}

	// Return just the value fragment for htmx swap.
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<code class="font-mono text-sm text-emerald-400 break-all">` + secret.Value + `</code>`))
}

func (s *Server) handleSecretDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	key := r.PathValue("key")
	scope := r.FormValue("scope")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, _ := st.ResolveDefaultProject()
	if err := st.RemoveSecret(project, scope, key); err != nil {
		if isHTMX(r) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<span class="text-rose-400 text-sm">%s</span>`, err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	redirect := fmt.Sprintf("/stores/%s", name)
	w.Header().Set("HX-Redirect", redirect)
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (s *Server) handleSecretSet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	key := r.PathValue("key")
	value := r.FormValue("value")
	env := r.FormValue("env")
	scope := r.FormValue("scope")
	providerName := r.FormValue("provider")

	if value == "" || env == "" {
		http.Error(w, "value and env required", http.StatusBadRequest)
		return
	}

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, _ := st.ResolveDefaultProject()
	scopePath := scope
	if scopePath == "" {
		scopePath = env + "/default"
	}

	if providerName != "" {
		err = st.SetSecretWithProvider(project, scopePath, key, value, providerName)
	} else {
		err = st.SetSecret(project, scopePath, key, value)
	}

	if err != nil {
		if isHTMX(r) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<span class="text-rose-400 text-sm">%s</span>`, err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	redirect := fmt.Sprintf("/stores/%s", name)
	w.Header().Set("HX-Redirect", redirect)
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (s *Server) handleSecretAdd(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	key := r.FormValue("key")
	value := r.FormValue("value")
	env := r.FormValue("env")
	scope := r.FormValue("scope")
	providerName := r.FormValue("provider")

	if key == "" || value == "" || env == "" {
		http.Error(w, "key, value, and env are required", http.StatusBadRequest)
		return
	}

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, _ := st.ResolveDefaultProject()
	scopePath := scope
	if scopePath == "" {
		scopePath = env + "/default"
	}

	if providerName != "" {
		err = st.SetSecretWithProvider(project, scopePath, key, value, providerName)
	} else {
		err = st.SetSecret(project, scopePath, key, value)
	}

	if err != nil {
		if isHTMX(r) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<span class="text-rose-400 text-sm">%s</span>`, err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	redirect := fmt.Sprintf("/stores/%s", name)
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", redirect)
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (s *Server) handleStorePush(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	if err := st.Push("valet: update from dashboard"); err != nil {
		if isHTMX(r) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<span class="text-rose-400 text-sm">%s</span>`, err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if isHTMX(r) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<span class="text-emerald-400 text-sm">Pushed</span>`))
		return
	}

	http.Redirect(w, r, "/stores/"+name, http.StatusFound)
}

func (s *Server) handleStoreActivity(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	// Get git log if available.
	var entries []activityEntry
	if st.IsGitRepo() {
		entries = getGitActivity(st)
	}

	s.render(w, "store_activity.html", map[string]any{
		"Page":      "stores",
		"Tab":       "activity",
		"StoreName": name,
		"StoreType":   st.Meta.Type,
		"StoreRemote": storeRemote(st),
		"Entries":   entries,
		"IsGitRepo": st.IsGitRepo(),
	})
}

type activityEntry struct {
	Time    string
	Author  string
	Message string
}

func getGitActivity(st *store.Store) []activityEntry {
	cmd := exec.Command("git", "log", "--pretty=format:%ar\t%an\t%s", "-20")
	cmd.Dir = st.Root
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var entries []activityEntry
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		entries = append(entries, activityEntry{
			Time:    parts[0],
			Author:  parts[1],
			Message: parts[2],
		})
	}
	return entries
}

// inventoryItem is a cross-store view of a single key.
type inventoryItem struct {
	Key     string
	Sources []inventorySource
}

type inventorySource struct {
	StoreName string
	ScopePath string
	Masked    string
	UpdatedAt string
}

func (s *Server) handleStoreInventory(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, _ := st.ResolveDefaultProject()
	envs, _ := st.ListEnvironments(project)

	// Collect all secrets across all environments.
	keyMap := make(map[string][]inventorySource)
	for _, env := range envs {
		scopes, _ := st.ListScopes(project, env)
		for _, scope := range scopes {
			for _, key := range scope.Secrets {
				if secret, err := st.GetSecret(project, scope.Path, key); err == nil {
					keyMap[key] = append(keyMap[key], inventorySource{
						StoreName: name,
						ScopePath: scope.Path,
						Masked:    maskSecret(secret.Value),
						UpdatedAt: timeAgo(secret.UpdatedAt),
					})
				}
			}
		}
	}

	// Sort into a list.
	var items []inventoryItem
	for k, sources := range keyMap {
		items = append(items, inventoryItem{Key: k, Sources: sources})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })

	s.render(w, "store_inventory.html", map[string]any{
		"Page":      "stores",
		"Tab":       "inventory",
		"StoreName": name,
		"StoreType":   st.Meta.Type,
		"StoreRemote": storeRemote(st),
		"Items":     items,
	})
}

// resolveStoreByName handles both named stores and project-embedded stores.
func (s *Server) resolveStoreByName(name string) (*store.Store, error) {
	// Check if this is the embedded store.
	if s.valetCfg != nil && strings.HasSuffix(name, "/.valet") {
		return store.Open(name, s.id)
	}
	return store.FindStoreByName(name, s.id)
}

