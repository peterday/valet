package ui

import (
	"fmt"
	"net/http"
	"sort"
)

type envUserRow struct {
	UserName  string
	IsOwner   bool
	HasAccess bool
}

type envSecretRow struct {
	Key       string
	Scope     string
	Provider  string
	UpdatedAt string
	UpdatedBy string
}

func (s *Server) handleStoreEnvironments(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	selectedEnv := r.URL.Query().Get("env")

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
	if selectedEnv == "" && len(envs) > 0 {
		selectedEnv = envs[0]
	}

	// Users and their access for this env.
	users, _ := st.ListUsers()
	scopes, _ := st.ListScopes(project, selectedEnv)

	hasAccess := make(map[string]bool)
	for _, scope := range scopes {
		for _, r := range scope.Recipients {
			hasAccess[r.Name] = true
		}
	}

	var userRows []envUserRow
	for _, u := range users {
		userRows = append(userRows, envUserRow{
			UserName:  u.Name,
			// IsOwner removed - no owner concept
			HasAccess: hasAccess[u.Name],
		})
	}

	// Secrets in this env.
	var secretRows []envSecretRow
	for _, scope := range scopes {
		manifest := readManifestFile(st.Root, project, scope.Path)

		for _, key := range scope.Secrets {
			row := envSecretRow{
				Key:   key,
				Scope: scope.Path,
			}
			if manifest != nil {
				if p, ok := manifest.Providers[key]; ok {
					row.Provider = p
				}
			}
			if secret, err := st.GetSecret(project, scope.Path, key); err == nil {
				row.UpdatedAt = timeAgo(secret.UpdatedAt)
				row.UpdatedBy = secret.UpdatedBy
			}
			secretRows = append(secretRows, row)
		}
	}

	sort.Slice(secretRows, func(i, j int) bool { return secretRows[i].Key < secretRows[j].Key })

	s.render(w, "store_environments.html", map[string]any{
		"Page":         "stores",
		"Tab":          "environments",
		"StoreName":    name,
		"StoreType":    st.Meta.Type,
		"StoreRemote":  storeRemote(st),
		"Environments": envs,
		"ActiveEnv":    selectedEnv,
		"Users":        userRows,
		"Secrets":      secretRows,
		"SecretCount":  len(secretRows),
		"UserCount":    len(userRows),
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

	redirect := fmt.Sprintf("/stores/%s/environments?env=%s", name, envName)
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

	redirect := fmt.Sprintf("/stores/%s/environments?env=%s", name, targetEnv)
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
