package ui

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/store"
)

// accessMatrixRow represents a user's access across environments.
type accessMatrixRow struct {
	UserName  string
	GitHub    string
	PublicKey string              // primary key (for display)
	AllKeys   []domain.UserKey    // all keys with labels
	KeyCount  int
	IsYou     bool
	Cells     []accessCell
}

type accessCell struct {
	Env       string
	HasAccess bool
}

func (s *Server) handleStoreTeam(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	matrix, envs := buildAccessMatrix(st, s.id.PublicKey)

	s.render(w, "store_team.html", map[string]any{
		"Page":         "stores",
		"Tab":          "team",
		"StoreName":    name,
		"StoreType":    st.Meta.Type,
		"StoreRemote":  storeRemote(st),
		"Matrix":       matrix,
		"Environments": envs,
	})
}

func buildAccessMatrix(st *store.Store, userPubKey string) ([]accessMatrixRow, []string) {
	project, err := st.ResolveDefaultProject()
	if err != nil {
		return nil, nil
	}

	users, _ := st.ListUsers()
	envs, _ := st.ListEnvironments(project)

	// Build user → env → has access map.
	userEnvAccess := make(map[string]map[string]bool)
	for _, u := range users {
		userEnvAccess[u.Name] = make(map[string]bool)
	}

	for _, env := range envs {
		scopes, _ := st.ListScopes(project, env)
		for _, scope := range scopes {
			for _, r := range scope.Recipients {
				if access, ok := userEnvAccess[r.Name]; ok {
					access[env] = true
				}
			}
		}
	}

	var matrix []accessMatrixRow
	for _, u := range users {
		row := accessMatrixRow{
			UserName:  u.Name,
			GitHub:    u.GitHub,
			PublicKey: u.PrimaryKey(),
			AllKeys:   u.AllUserKeys(),
			KeyCount:  len(u.AllKeys()),
			IsYou:     u.HasKey(userPubKey),
		}
		for _, env := range envs {
			row.Cells = append(row.Cells, accessCell{
				Env:       env,
				HasAccess: userEnvAccess[u.Name][env],
			})
		}
		matrix = append(matrix, row)
	}

	return matrix, envs
}

func (s *Server) handleTeamGrant(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	userName := r.FormValue("user")
	env := r.FormValue("env")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, _ := st.ResolveDefaultProject()
	_, err = st.GrantEnvironment(project, env, userName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect back to referrer or team page.
	ref := r.Header.Get("Referer")
	if ref == "" {
		ref = "/stores/" + name + "/team"
	}
	http.Redirect(w, r, ref, http.StatusFound)
}

func (s *Server) handleTeamRevoke(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	userName := r.FormValue("user")
	env := r.FormValue("env")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, _ := st.ResolveDefaultProject()
	_, _, err = st.RevokeEnvironmentWithRotation(project, env, userName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ref := r.Header.Get("Referer")
	if ref == "" {
		ref = "/stores/" + name + "/team"
	}
	http.Redirect(w, r, ref, http.StatusFound)
}

func (s *Server) handleStoreInvites(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	var invites []domain.Invite
	invites, _ = st.ListInvites()

	s.render(w, "store_invites.html", map[string]any{
		"Page":      "stores",
		"Tab":       "invites",
		"StoreName": name,
		"StoreType":   st.Meta.Type,
		"StoreRemote": storeRemote(st),
		"Invites":   invites,
	})
}

func (s *Server) handleTeamInviteCreate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	envsStr := r.FormValue("environments")
	expiryStr := r.FormValue("expiry")
	maxUsesStr := r.FormValue("max_uses")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	envs := strings.Split(envsStr, ",")
	for i := range envs {
		envs[i] = strings.TrimSpace(envs[i])
	}

	expiry := 48 * time.Hour
	if expiryStr != "" {
		if d, err := time.ParseDuration(expiryStr); err == nil {
			expiry = d
		}
	}

	maxUses := 1
	if maxUsesStr != "" {
		if n, err := strconv.Atoi(maxUsesStr); err == nil {
			maxUses = n
		}
	}

	project, _ := st.ResolveDefaultProject()
	invite, tempKey, err := st.CreateInvite(project, envs, expiry, maxUses)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if isHTMX(r) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<div class="bg-slate-800 rounded-lg p-4 border border-emerald-500/30">
			<p class="text-emerald-400 font-medium mb-2">Invite created</p>
			<p class="text-slate-300 text-sm mb-1">ID: %s</p>
			<p class="text-slate-300 text-sm mb-1">Environments: %s</p>
			<div class="mt-3">
				<label class="text-slate-400 text-xs">Invite key (share with recipient):</label>
				<code class="block bg-slate-900 p-2 rounded mt-1 text-xs text-amber-400 break-all select-all">%s</code>
			</div>
		</div>`, invite.ID, strings.Join(invite.Environments, ", "), tempKey)
		return
	}

	http.Redirect(w, r, "/stores/"+name+"/invites", http.StatusFound)
}

func (s *Server) handleTeamInvitePrune(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	project, _ := st.ResolveDefaultProject()
	pruned, err := st.PruneExpiredInvites(project)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if isHTMX(r) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<span class="text-emerald-400">Pruned %d expired invite(s)</span>`, pruned)
		return
	}

	http.Redirect(w, r, "/stores/"+name+"/invites", http.StatusFound)
}

func (s *Server) handleUserAdd(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	userName := r.FormValue("user_name")
	github := r.FormValue("github")
	publicKey := r.FormValue("public_key")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	// If GitHub username provided but no public key, fetch all keys from GitHub.
	var allKeys []domain.UserKey
	if github != "" && publicKey == "" {
		keys, err := store.FetchGitHubKeys(github)
		if err != nil {
			msg := fmt.Sprintf(`<div class="bg-rose-500/10 border border-rose-500/30 rounded-md p-3 text-sm">
				<p class="text-rose-400 font-medium">Could not fetch SSH keys for @%s</p>
				<p class="text-slate-400 mt-1">%s</p>
				<p class="text-slate-500 mt-2 text-xs">They need to <a href="https://github.com/settings/keys" target="_blank" class="text-indigo-400 hover:text-indigo-300">add an SSH key to GitHub</a>, or you can add them manually with a public key below.</p>
			</div>`, github, err.Error())
			if isHTMX(r) {
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte(msg))
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		allKeys = keys
	} else if publicKey != "" {
		allKeys = []domain.UserKey{{Key: publicKey}}
	}

	if userName == "" {
		userName = github
	}

	if userName == "" || len(allKeys) == 0 {
		msg := `<div class="text-rose-400 text-sm">Provide a GitHub username or use the manual form below.</div>`
		if isHTMX(r) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(msg))
			return
		}
		http.Error(w, "name and public key (or GitHub username) required", http.StatusBadRequest)
		return
	}

	if _, err := st.AddUserWithKeys(userName, github, allKeys); err != nil {
		msg := fmt.Sprintf(`<div class="text-rose-400 text-sm">%s</div>`, err.Error())
		if isHTMX(r) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(msg))
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if isHTMX(r) {
		w.Header().Set("HX-Redirect", "/stores/"+name+"/team")
		return
	}
	http.Redirect(w, r, "/stores/"+name+"/team", http.StatusFound)
}

func (s *Server) handleUserRefresh(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	userName := r.PathValue("user")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	user, err := st.GetUser(userName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if user.GitHub == "" {
		http.Error(w, "user has no GitHub handle", http.StatusBadRequest)
		return
	}

	ghKeys, err := store.FetchGitHubKeys(user.GitHub)
	if err != nil {
		http.Error(w, fmt.Sprintf("fetching keys for @%s: %s", user.GitHub, err.Error()), http.StatusBadGateway)
		return
	}

	// Separate: GitHub-sourced keys sync with GitHub, everything else stays.
	isSSH := func(key string) bool {
		return strings.HasPrefix(key, "ssh-") || strings.HasPrefix(key, "ecdsa-")
	}
	var otherKeys []domain.UserKey
	var oldGHKeys []domain.UserKey
	for _, k := range user.AllUserKeys() {
		if k.Source == "github" || k.Source == "ssh" || (k.Source == "" && isSSH(k.Key)) {
			oldGHKeys = append(oldGHKeys, k)
		} else {
			// Clean up bad labels from previous buggy syncs.
			if strings.Contains(k.Label, "removed from GitHub") {
				k.Label = ""
			}
			otherKeys = append(otherKeys, k)
		}
	}

	newGHSet := make(map[string]bool)
	for _, k := range ghKeys {
		newGHSet[k.Key] = true
	}

	var syncKeys []domain.UserKey
	syncKeys = append(syncKeys, otherKeys...)
	syncKeys = append(syncKeys, ghKeys...)
	for _, k := range oldGHKeys {
		if !newGHSet[k.Key] {
			k.Label = k.Label + " (removed from GitHub)"
			syncKeys = append(syncKeys, k)
		}
	}

	_, _, _, err = st.SyncUserKeys(userName, syncKeys)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/stores/"+name+"/team", http.StatusFound)
}

func (s *Server) handleUserUpdate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	userName := r.PathValue("user")
	github := r.FormValue("github")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	updates := map[string]string{"github": github}
	if err := st.UpdateUser(userName, updates); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/stores/"+name+"/team", http.StatusFound)
}


func (s *Server) handleUserRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	userName := r.PathValue("user")

	st, err := s.resolveStoreByName(name)
	if err != nil {
		http.Error(w, "store not found", http.StatusNotFound)
		return
	}

	// Revoke from all environments first (re-encrypts vaults).
	project, _ := st.ResolveDefaultProject()
	envs, _ := st.ListEnvironments(project)
	for _, env := range envs {
		st.RevokeEnvironmentWithRotation(project, env, userName)
	}

	// Remove the user file.
	if err := st.RemoveUser(userName); err != nil {
		if isHTMX(r) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<span class="text-rose-400 text-sm">%s</span>`, err.Error())
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/stores/"+name+"/team", http.StatusFound)
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}
