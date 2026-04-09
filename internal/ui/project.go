package ui

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/store"
)

// requirementRow is a single requirement in the project requirements view.
type requirementRow struct {
	Key         string
	Provider    string
	Description string
	Optional    bool
	Status      string // "ok", "missing", "optional"
	Source      string // e.g. "my-keys/dev (linked)" or "MISSING"
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	if s.valetCfg == nil {
		// Check for .env.example in the current directory or s.projectDir.
		checkDir := s.projectDir
		if checkDir == "" {
			checkDir, _ = os.Getwd()
		}
		envExamplePath := ""
		if checkDir != "" {
			envExamplePath = store.FindEnvExample(checkDir)
		}
		s.render(w, "project.html", map[string]any{
			"Page":           "project",
			"HasProject":     false,
			"ProjectDir":     s.projectDir,
			"EnvExamplePath": envExamplePath,
			"AdoptDir":       checkDir,
		})
		return
	}

	env := r.URL.Query().Get("env")
	if env == "" {
		env = s.valetCfg.DefaultEnv
	}

	// Get environments for the selector.
	var envs []string
	if embedded, err := s.openEmbeddedStore(); err == nil {
		if project, err := embedded.ResolveDefaultProject(); err == nil {
			envs, _ = embedded.ListEnvironments(project)
		}
	}

	// Build requirements list.
	linkedStores := s.openAllStores()
	resolved, err := store.ResolveAllSecrets(linkedStores, env)
	if err != nil {
		resolved = make(map[string]store.ResolvedSecret)
	}

	var rows []requirementRow
	for key, req := range s.valetCfg.Requires {
		row := requirementRow{
			Key:         key,
			Provider:    req.Provider,
			Description: req.Description,
			Optional:    req.Optional,
		}

		if rs, ok := resolved[key]; ok {
			row.Status = "ok"
			row.Source = rs.StoreName + "/" + envFromScope(rs.ScopePath)
			if rs.Wildcard {
				row.Source += " (wildcard)"
			}
		} else if req.Optional {
			row.Status = "optional"
			row.Source = "not set"
		} else {
			row.Status = "missing"
			row.Source = "MISSING"
		}

		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })

	s.render(w, "project.html", map[string]any{
		"Page":         "project",
		"Tab":          "requirements",
		"HasProject":   true,
		"ProjectName":  s.valetCfg.Project,
		"ProjectDir":   s.projectDir,
		"StoreName":    s.valetCfg.Store,
		"ActiveEnv":    env,
		"Environments": envs,
		"Requirements": rows,
	})
}

func (s *Server) handleProjectAdoptPreview(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = s.projectDir
	}
	if dir == "" {
		http.Error(w, "no project directory", http.StatusBadRequest)
		return
	}

	result, err := store.AnalyzeForAdopt(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.render(w, "project_adopt.html", map[string]any{
		"Page":       "project",
		"ProjectDir": dir,
		"Result":     result,
	})
}

func (s *Server) handleProjectAdoptApply(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	dir := r.FormValue("path")
	if dir == "" {
		dir = s.projectDir
	}
	importEnv := r.FormValue("import_env") == "on"

	result, err := store.AnalyzeForAdopt(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Apply user's selection: only track keys they checked.
	tracked := make(map[string]bool)
	for _, k := range r.Form["track"] {
		tracked[k] = true
	}

	// Filter requirements + promote any non-secrets the user opted to track.
	var newReqs []store.DetectedRequirement
	for _, req := range result.Requirements {
		if tracked[req.Key] {
			newReqs = append(newReqs, req)
		}
	}
	for _, c := range result.NonSecrets {
		if tracked[c.Key] {
			newReqs = append(newReqs, store.DetectedRequirement{
				Key:              c.Key,
				PlaceholderValue: c.Value,
			})
		}
	}
	result.Requirements = newReqs

	if err := result.Apply(dir, s.id, importEnv); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Refresh server's project context.
	s.projectDir = dir
	s.loadProjectConfig()

	http.Redirect(w, r, "/project/setup", http.StatusFound)
}

func (s *Server) handleProjectPickFolder(w http.ResponseWriter, r *http.Request) {
	var path string
	var err error

	switch runtime.GOOS {
	case "darwin":
		// Use osascript to open a native folder picker.
		out, e := exec.Command("osascript", "-e",
			`set theFolder to choose folder with prompt "Select a valet project folder"`,
			"-e", `POSIX path of theFolder`).Output()
		if e != nil {
			http.Error(w, "folder picker cancelled", http.StatusNoContent)
			return
		}
		path = strings.TrimSpace(strings.TrimRight(string(out), "/\n"))
		err = nil
	default:
		http.Error(w, "folder picker not supported on "+runtime.GOOS, http.StatusNotImplemented)
		return
	}

	if err != nil || path == "" {
		http.Error(w, "no folder selected", http.StatusNoContent)
		return
	}

	// Return the path as plain text for JS to pick up.
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(path))
}

func (s *Server) handleProjectSelect(w http.ResponseWriter, r *http.Request) {
	dir := r.FormValue("path")
	if dir == "" {
		http.Redirect(w, r, "/project", http.StatusFound)
		return
	}

	// Validate the directory has a .valet.toml.
	tomlPath := filepath.Join(dir, config.ValetToml)
	vc, err := config.LoadValetToml(tomlPath)
	if err != nil {
		if isHTMX(r) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<span class="text-rose-400">No .valet.toml found in %s</span>`, dir)
			return
		}
		http.Redirect(w, r, "/project", http.StatusFound)
		return
	}

	s.projectDir = dir
	s.valetCfg = vc
	lc, _ := config.LoadLocalConfig(dir)
	s.localCfg = lc

	http.Redirect(w, r, "/project", http.StatusFound)
}

// resolutionRow shows where a key comes from in the resolution chain.
type resolutionRow struct {
	Key     string
	Sources []resolutionSource
}

type resolutionSource struct {
	StoreName string
	ScopePath string
	Masked    string
	IsWinner  bool
	HasValue  bool
}

func (s *Server) handleProjectResolution(w http.ResponseWriter, r *http.Request) {
	if s.valetCfg == nil {
		http.Redirect(w, r, "/project", http.StatusFound)
		return
	}

	env := r.URL.Query().Get("env")
	if env == "" {
		env = s.valetCfg.DefaultEnv
	}

	linkedStores := s.openAllStores()
	resolved, provenance, err := store.ResolveAllSecretsWithProvenance(linkedStores, env, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var rows []resolutionRow
	for key, sources := range provenance {
		row := resolutionRow{Key: key}
		winner := resolved[key]

		for _, src := range sources {
			rs := resolutionSource{
				StoreName: src.StoreName,
				ScopePath: src.ScopePath,
				HasValue:  true,
				Masked:    maskSecret(src.Value),
				IsWinner:  src.StoreName == winner.StoreName && src.ScopePath == winner.ScopePath,
			}
			row.Sources = append(row.Sources, rs)
		}

		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })

	// Get environments for selector.
	var envs []string
	if embedded, err := s.openEmbeddedStore(); err == nil {
		if project, err := embedded.ResolveDefaultProject(); err == nil {
			envs, _ = embedded.ListEnvironments(project)
		}
	}

	s.render(w, "project_resolution.html", map[string]any{
		"Page":         "project",
		"Tab":          "resolution",
		"ProjectName":  s.valetCfg.Project,
		"ActiveEnv":    env,
		"Environments": envs,
		"Rows":         rows,
	})
}

// setupCard is a missing requirement to show in the setup form.
type setupCard struct {
	Key         string
	Provider    *providerInfo
	Description string
	Optional    bool
	Format      string // expected prefix hint
}

type providerInfo struct {
	Name        string
	DisplayName string
	Description string
	SetupURL    string
	FreeTier    string
	Category    string
}

func (s *Server) handleProjectSetup(w http.ResponseWriter, r *http.Request) {
	if s.valetCfg == nil {
		http.Redirect(w, r, "/project", http.StatusFound)
		return
	}

	env := s.valetCfg.DefaultEnv
	keysFilter := r.URL.Query().Get("keys")

	linkedStores := s.openAllStores()
	resolved, _ := store.ResolveAllSecrets(linkedStores, env)

	var cards []setupCard
	for key, req := range s.valetCfg.Requires {
		// Skip already-resolved keys.
		if _, ok := resolved[key]; ok {
			continue
		}

		// If filtering by specific keys, skip non-matching.
		if keysFilter != "" {
			allowed := strings.Split(keysFilter, ",")
			found := false
			for _, a := range allowed {
				if strings.TrimSpace(a) == key {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		card := setupCard{
			Key:         key,
			Description: req.Description,
			Optional:    req.Optional,
		}

		// Look up provider info.
		if req.Provider != "" {
			prov := s.registry.Get(req.Provider)
			if prov != nil {
				card.Provider = &providerInfo{
					Name:        prov.Name,
					DisplayName: prov.DisplayName,
					Description: prov.Description,
					SetupURL:    prov.SetupURL,
					FreeTier:    prov.FreeTier,
					Category:    prov.Category,
				}
				// Find the prefix for this key.
				for _, ev := range prov.EnvVars {
					if ev.Name == key {
						prefixes := ev.AllPrefixes()
						if len(prefixes) > 0 {
							card.Format = prefixes[0] + "..."
						}
						break
					}
				}
			}
		}

		cards = append(cards, card)
	}

	sort.Slice(cards, func(i, j int) bool { return cards[i].Key < cards[j].Key })

	s.render(w, "setup.html", map[string]any{
		"Page":        "project",
		"Tab":         "setup",
		"ProjectName": s.valetCfg.Project,
		"Cards":       cards,
		"Count":       len(cards),
	})
}

func (s *Server) handleProjectSetupSave(w http.ResponseWriter, r *http.Request) {
	if s.valetCfg == nil {
		http.Error(w, "no project", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	embedded, err := s.openEmbeddedStore()
	if err != nil {
		http.Error(w, "cannot open store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	project, _ := embedded.ResolveDefaultProject()
	env := s.valetCfg.DefaultEnv
	scopePath := env + "/default"

	var savedKeys []string
	for key := range s.valetCfg.Requires {
		value := r.FormValue("secret_" + key)
		if value == "" {
			continue
		}

		// Look up provider for metadata.
		req := s.valetCfg.Requires[key]
		providerName := req.Provider

		if providerName != "" {
			err = embedded.SetSecretWithProvider(project, scopePath, key, value, providerName)
		} else {
			err = embedded.SetSecret(project, scopePath, key, value)
		}
		if err != nil {
			continue
		}
		savedKeys = append(savedKeys, key)
	}

	// Notify MCP channel if waiting.
	if s.setupCh != nil && len(savedKeys) > 0 {
		select {
		case s.setupCh <- SetupResult{Keys: savedKeys}:
		default:
		}
	}

	if isHTMX(r) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<div class="bg-emerald-500/10 border border-emerald-500/30 rounded-lg p-4 text-emerald-400">
			Configured %d secret(s): %s
		</div>`, len(savedKeys), strings.Join(savedKeys, ", "))
		return
	}

	http.Redirect(w, r, "/project/setup", http.StatusFound)
}

func (s *Server) handleProjectLinks(w http.ResponseWriter, r *http.Request) {
	if s.valetCfg == nil {
		http.Redirect(w, r, "/project", http.StatusFound)
		return
	}

	type linkRow struct {
		Name       string
		Type       string
		KeyFilter  string
		EnvMapping string
		Source     string // ".valet.toml" or ".valet.local.toml"
		Provides   []string
	}

	var rows []linkRow

	// Shared links from .valet.toml.
	for _, link := range s.valetCfg.Stores {
		row := linkRow{
			Name:   link.Name,
			Type:   "shared",
			Source: ".valet.toml",
		}

		keys := link.ParsedKeys()
		if keys == nil {
			row.KeyFilter = "all keys"
		} else {
			row.KeyFilter = fmt.Sprintf("%d keys", len(keys))
		}

		if len(link.Environments) > 0 {
			var maps []string
			for _, em := range link.Environments {
				maps = append(maps, em.Local+"→"+em.Remote)
			}
			row.EnvMapping = strings.Join(maps, ", ")
		}

		// Try to list what this store provides.
		if st, err := s.resolveStoreByName(link.Name); err == nil {
			if proj, err := st.ResolveDefaultProject(); err == nil {
				if secs, err := st.ListSecretsInEnv(proj, s.valetCfg.DefaultEnv); err == nil {
					for k := range secs {
						row.Provides = append(row.Provides, k)
					}
					sort.Strings(row.Provides)
				}
			}
		}

		rows = append(rows, row)
	}

	// Personal links from .valet.local.toml.
	if s.localCfg != nil {
		for _, link := range s.localCfg.Stores {
			row := linkRow{
				Name:   link.Name,
				Type:   "personal",
				Source: ".valet.local.toml",
			}

			keys := link.ParsedKeys()
			if keys == nil {
				row.KeyFilter = "all keys"
			} else {
				row.KeyFilter = fmt.Sprintf("%d keys", len(keys))
			}

			if len(link.Environments) > 0 {
				var maps []string
				for _, em := range link.Environments {
					maps = append(maps, em.Local+"→"+em.Remote)
				}
				row.EnvMapping = strings.Join(maps, ", ")
			}

			if st, err := s.resolveStoreByName(link.Name); err == nil {
				if proj, err := st.ResolveDefaultProject(); err == nil {
					if secs, err := st.ListSecretsInEnv(proj, s.valetCfg.DefaultEnv); err == nil {
						for k := range secs {
							row.Provides = append(row.Provides, k)
						}
						sort.Strings(row.Provides)
					}
				}
			}

			rows = append(rows, row)
		}
	}

	s.render(w, "project_links.html", map[string]any{
		"Page":        "project",
		"Tab":         "links",
		"ProjectName": s.valetCfg.Project,
		"Links":       rows,
	})
}
