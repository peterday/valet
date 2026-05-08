package ui

import (
	"fmt"
	"net/http"
)

// envAccessRow is one user's access across all environments.
type envAccessRow struct {
	UserName string
	Access   map[string]bool // env name → has access
}

// envScopeDetail is per-scope info within an env card.
type envScopeDetail struct {
	Name        string   // display name (e.g. "default", "payments")
	Path        string   // full scope path (e.g. "prod/payments")
	SecretCount int
	Recipients  []string // user names with access
}

// envCard is summary info for one environment.
type envCard struct {
	Name        string
	SecretCount int
	Scopes      []envScopeDetail
}

func (s *Server) handleStoreEnvironments(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, err := st.ResolveDefaultProject()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	envs, _ := st.ListEnvironments(project)
	users, _ := st.ListUsers()

	// Filter out * from display envs.
	var displayEnvs []string
	for _, e := range envs {
		if e != "*" {
			displayEnvs = append(displayEnvs, e)
		}
	}

	// Build access matrix: for each user, check access per env.
	var matrix []envAccessRow
	for _, u := range users {
		row := envAccessRow{
			UserName: u.Name,
			Access:   make(map[string]bool),
		}
		for _, env := range displayEnvs {
			scopes, _ := st.ListScopes(project, env)
			for _, scope := range scopes {
				for _, r := range scope.Recipients {
					if r.Name == u.Name {
						row.Access[env] = true
					}
				}
			}
		}
		matrix = append(matrix, row)
	}

	// Build env cards with scope details.
	var cards []envCard
	for _, env := range displayEnvs {
		scopes, _ := st.ListScopes(project, env)
		totalSecrets := 0
		var scopeDetails []envScopeDetail
		for _, sc := range scopes {
			totalSecrets += len(sc.Secrets)
			seen := make(map[string]bool)
			var recipients []string
			for _, r := range sc.Recipients {
				if !seen[r.Name] {
					seen[r.Name] = true
					recipients = append(recipients, r.Name)
				}
			}
			scopeDetails = append(scopeDetails, envScopeDetail{
				Name:        scopeDisplayName(sc.Path),
				Path:        sc.Path,
				SecretCount: len(sc.Secrets),
				Recipients:  recipients,
			})
		}
		cards = append(cards, envCard{
			Name:        env,
			SecretCount: totalSecrets,
			Scopes:      scopeDetails,
		})
	}

	// Collect all user names for the scope recipient management.
	var userNames []string
	for _, u := range users {
		userNames = append(userNames, u.Name)
	}

	s.render(w, "store_environments.html", map[string]any{
		"Page":         "stores",
		"Tab":          "environments",
		"StoreName":    name,
		"StoreType":    st.Meta.Type,
		"StoreRemote":  storeRemote(st),
		"Environments": displayEnvs,
		"Matrix":       matrix,
		"EnvCards":     cards,
		"UserNames":   userNames,
	})
}

func (s *Server) handleEnvCreate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	envName := r.FormValue("env_name")

	if envName == "" {
		http.Error(w, "environment name required", http.StatusBadRequest)
		return
	}

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, _ := st.ResolveDefaultProject()

	if err := st.CreateEnvironment(project, envName); err != nil {
		if isHTMX(r) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<span class="text-rose-400 text-sm">%s</span>`, err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Create a default scope in the new environment.
	if err := st.CreateScope(project, envName+"/default"); err != nil {
		// Env created but scope failed — not fatal.
	}

	redirect := fmt.Sprintf("/stores/%s/environments", name)
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", redirect)
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (s *Server) handleEnvClone(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sourceEnv := r.FormValue("source_env")
	targetEnv := r.FormValue("target_env")

	if sourceEnv == "" || targetEnv == "" {
		http.Error(w, "source and target environment required", http.StatusBadRequest)
		return
	}

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	copyUsers := r.FormValue("copy_users") == "on"

	project, _ := st.ResolveDefaultProject()

	// Create the target environment.
	st.CreateEnvironment(project, targetEnv) // ignore "already exists" errors

	// Copy all scopes from source to target.
	sourceScopes, _ := st.ListScopes(project, sourceEnv)
	for _, scope := range sourceScopes {
		// Derive target scope path: replace env prefix.
		targetScope := targetEnv + scope.Path[len(sourceEnv):]

		// Create scope (sets up manifest + empty vault with current user as recipient).
		if err := st.CreateScope(project, targetScope); err != nil {
			continue
		}

		// Grant source recipients on the new scope.
		if copyUsers {
			for _, r := range scope.Recipients {
				// AddRecipient skips if already a recipient (the creator).
				st.AddRecipient(project, targetScope, r.Name)
			}
		}

		// Copy each secret from source to target.
		for _, key := range scope.Secrets {
			secret, err := st.GetSecret(project, scope.Path, key)
			if err != nil {
				continue
			}

			manifest := readManifestFile(st.Root, project, scope.Path)
			provider := ""
			if manifest != nil {
				if p, ok := manifest.Providers[key]; ok {
					provider = p
				}
			}

			if provider != "" {
				st.SetSecretWithProvider(project, targetScope, key, secret.Value, provider)
			} else {
				st.SetSecret(project, targetScope, key, secret.Value)
			}
		}
	}

	redirect := fmt.Sprintf("/stores/%s/environments", name)
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", redirect)
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (s *Server) handleScopeCreate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	env := r.FormValue("env")
	scopeName := r.FormValue("scope_name")

	if env == "" || scopeName == "" {
		http.Error(w, "environment and scope name required", http.StatusBadRequest)
		return
	}

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, _ := st.ResolveDefaultProject()
	scopePath := env + "/" + scopeName

	if err := st.CreateScope(project, scopePath); err != nil {
		if isHTMX(r) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<span class="text-rose-400 text-sm">%s</span>`, err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	redirect := fmt.Sprintf("/stores/%s/environments", name)
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", redirect)
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (s *Server) handleScopeGrant(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	userName := r.FormValue("user")
	scopePath := r.FormValue("scope")

	if userName == "" || scopePath == "" {
		http.Error(w, "user and scope required", http.StatusBadRequest)
		return
	}

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, _ := st.ResolveDefaultProject()
	if err := st.AddRecipient(project, scopePath, userName); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	redirect := fmt.Sprintf("/stores/%s/environments", name)
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", redirect)
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (s *Server) handleScopeRevoke(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	userName := r.FormValue("user")
	scopePath := r.FormValue("scope")

	if userName == "" || scopePath == "" {
		http.Error(w, "user and scope required", http.StatusBadRequest)
		return
	}

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, _ := st.ResolveDefaultProject()
	if err := st.RemoveRecipient(project, scopePath, userName); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	redirect := fmt.Sprintf("/stores/%s/environments", name)
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", redirect)
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (s *Server) handleEnvDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	envName := r.FormValue("env_name")

	if envName == "" {
		http.Error(w, "environment name required", http.StatusBadRequest)
		return
	}

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, _ := st.ResolveDefaultProject()

	// Delete the environment directory on disk.
	envDir := st.Root + "/projects/" + project + "/" + envName
	if err := removeAll(envDir); err != nil {
		if isHTMX(r) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<span class="text-rose-400 text-sm">%s</span>`, err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	redirect := fmt.Sprintf("/stores/%s/environments", name)
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", redirect)
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}
