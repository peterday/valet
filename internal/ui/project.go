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
	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/identity"
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

	requirements := store.ResolveRequirements(s.projectDir, s.valetCfg, s.localCfg)

	var rows []requirementRow
	for _, req := range requirements {
		row := requirementRow{
			Key:         req.Key,
			Provider:    req.Provider,
			Description: req.Description,
			Optional:    req.Optional,
		}

		if rs, ok := resolved[req.Key]; ok {
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

	// Check if a migrate banner should show: project has .env.example AND
	// .valet.toml has redundant requirements that could be auto-detected.
	canMigrate := false
	migrateRedundantCount := 0
	if store.HasEnvExampleRequirements(s.projectDir) {
		plan := store.PlanMigration(s.projectDir, s.valetCfg)
		migrateRedundantCount = len(plan.Redundant)
		canMigrate = migrateRedundantCount > 0
	}

	s.render(w, "project.html", map[string]any{
		"Page":            "project",
		"Tab":             "requirements",
		"HasProject":      true,
		"ProjectName":     s.valetCfg.Project,
		"ProjectDir":      s.projectDir,
		"StoreName":       s.valetCfg.Store,
		"ActiveEnv":       env,
		"Environments":    envs,
		"Requirements":    rows,
		"CanMigrate":      canMigrate,
		"MigrateCount":    migrateRedundantCount,
	})
}

func (s *Server) handleProjectMigratePreview(w http.ResponseWriter, r *http.Request) {
	if s.valetCfg == nil {
		http.Error(w, "no project loaded", http.StatusBadRequest)
		return
	}

	plan := store.PlanMigration(s.projectDir, s.valetCfg)

	s.render(w, "project_migrate.html", map[string]any{
		"Page":       "project",
		"ProjectDir": s.projectDir,
		"Plan":       plan,
	})
}

func (s *Server) handleProjectMigrateApply(w http.ResponseWriter, r *http.Request) {
	if s.valetCfg == nil {
		http.Error(w, "no project loaded", http.StatusBadRequest)
		return
	}

	plan := store.PlanMigration(s.projectDir, s.valetCfg)
	if len(plan.Redundant) == 0 {
		http.Redirect(w, r, "/project", http.StatusFound)
		return
	}

	updated := plan.Apply(s.valetCfg)
	tomlPath := filepath.Join(s.projectDir, config.ValetToml)
	if err := config.WriteValetToml(tomlPath, updated); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Reload project context.
	s.loadProjectConfig()
	http.Redirect(w, r, "/project", http.StatusFound)
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
	personalScope := r.FormValue("override_scope") == "personal"

	result, err := store.AnalyzeForAdopt(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Determine user selections vs heuristic defaults — only deviations
	// become overrides.
	tracked := make(map[string]bool)
	for _, k := range r.Form["track"] {
		tracked[k] = true
	}

	// Heuristic defaults: requirements = checked, non-secrets = unchecked.
	heuristicTracked := make(map[string]bool)
	for _, req := range result.Requirements {
		heuristicTracked[req.Key] = true
	}

	// User opt-ins (heuristic said no, user said yes) → write override.
	// User opt-outs (heuristic said yes, user said no) → write Track:false override.
	overrides := make(map[string]domain.Requirement)
	for _, c := range result.NonSecrets {
		if tracked[c.Key] {
			t := true
			overrides[c.Key] = domain.Requirement{Track: &t}
		}
	}
	for _, req := range result.Requirements {
		if !tracked[req.Key] {
			f := false
			overrides[req.Key] = domain.Requirement{Track: &f}
		}
	}

	// Apply: create embedded store, write overrides (if any) to chosen scope,
	// optionally import existing .env values.
	if err := applyAdopt(dir, s.id, result, overrides, personalScope, importEnv); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Refresh server's project context.
	s.projectDir = dir
	s.loadProjectConfig()

	http.Redirect(w, r, "/project/setup", http.StatusFound)
}

// applyAdopt creates the embedded store, writes overrides to the chosen
// config file (.valet.toml or .valet.local.toml), and optionally imports
// values from .env.
func applyAdopt(dir string, id *identity.Identity, result *store.AdoptResult, overrides map[string]domain.Requirement, personalScope bool, importEnv bool) error {
	// 1. Create embedded store if missing.
	storeRoot := filepath.Join(dir, ".valet")
	if _, err := os.Stat(filepath.Join(storeRoot, "store.json")); os.IsNotExist(err) {
		s, err := store.Create(storeRoot, "default", domain.StoreTypeEmbedded, id)
		if err != nil {
			return fmt.Errorf("creating embedded store: %w", err)
		}
		s.AddUser("me", "", id.PublicKey)
		s.CreateProject("default")
		s.CreateEnvironment("default", "dev")
		s.CreateScope("default", "dev/default")
	}

	// 2. Ensure .valet.toml exists with basic project info.
	tomlPath := filepath.Join(dir, config.ValetToml)
	vc, err := config.LoadValetToml(tomlPath)
	if err != nil {
		vc = &domain.ValetConfig{
			Store:      ".",
			Project:    "default",
			DefaultEnv: "dev",
		}
	}

	// 3. Write overrides to chosen scope.
	if len(overrides) > 0 {
		if personalScope {
			lc, _ := config.LoadLocalConfig(dir)
			if lc == nil {
				lc = &domain.LocalConfig{}
			}
			if lc.Requires == nil {
				lc.Requires = make(map[string]domain.Requirement)
			}
			for k, v := range overrides {
				lc.Requires[k] = v
			}
			if err := config.WriteLocalConfig(dir, lc); err != nil {
				return err
			}
			ensureLineInFile(filepath.Join(dir, ".gitignore"), config.ValetLocalToml)
		} else {
			if vc.Requires == nil {
				vc.Requires = make(map[string]domain.Requirement)
			}
			for k, v := range overrides {
				vc.Requires[k] = v
			}
		}
	}

	if err := config.WriteValetToml(tomlPath, vc); err != nil {
		return err
	}

	// 4. Import existing .env values into the embedded store.
	if importEnv && result.HasExistingEnv {
		st, err := store.Open(storeRoot, id)
		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}
		// Build the merged requirements list to know what we should import.
		localCfg, _ := config.LoadLocalConfig(dir)
		merged := store.ResolveRequirements(dir, vc, localCfg)
		for _, req := range merged {
			val, ok := result.ExistingValues[req.Key]
			if !ok || val == "" {
				continue
			}
			scopePath := vc.DefaultEnv + "/default"
			if req.Provider != "" {
				_ = st.SetSecretWithProvider("default", scopePath, req.Key, val, req.Provider)
			} else {
				_ = st.SetSecret("default", scopePath, req.Key, val)
			}
		}
	}

	return nil
}

// ensureLineInFile appends a line to a file if it's not already present.
func ensureLineInFile(path, line string) {
	data, _ := os.ReadFile(path)
	for _, existing := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(existing) == line {
			return
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(data) > 0 && data[len(data)-1] != '\n' {
		f.WriteString("\n")
	}
	f.WriteString(line + "\n")
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

	requirements := store.ResolveRequirements(s.projectDir, s.valetCfg, s.localCfg)

	var cards []setupCard
	for _, req := range requirements {
		// Skip already-resolved keys.
		if _, ok := resolved[req.Key]; ok {
			continue
		}

		// If filtering by specific keys, skip non-matching.
		if keysFilter != "" {
			allowed := strings.Split(keysFilter, ",")
			found := false
			for _, a := range allowed {
				if strings.TrimSpace(a) == req.Key {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		card := setupCard{
			Key:         req.Key,
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
					if ev.Name == req.Key {
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

	requirements := store.ResolveRequirements(s.projectDir, s.valetCfg, s.localCfg)

	var savedKeys []string
	for _, req := range requirements {
		value := r.FormValue("secret_" + req.Key)
		if value == "" {
			continue
		}

		if req.Provider != "" {
			err = embedded.SetSecretWithProvider(project, scopePath, req.Key, value, req.Provider)
		} else {
			err = embedded.SetSecret(project, scopePath, req.Key, value)
		}
		if err != nil {
			continue
		}
		savedKeys = append(savedKeys, req.Key)
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
