package ui

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/identity"
	"github.com/peterday/valet/internal/provider"
	"github.com/peterday/valet/internal/store"
)

//go:embed templates/*.html templates/components/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

const (
	idleTimeout    = 30 * time.Minute
	setupTimeout   = 10 * time.Minute
	revealDuration = 30 * time.Second
)

// Server is the valet web dashboard server.
type Server struct {
	cfg      *config.Config
	id       *identity.Identity
	registry *provider.Registry

	// Project context (optional — set when launched from a project dir).
	projectDir string
	valetCfg   *domain.ValetConfig
	localCfg   *domain.LocalConfig

	// CSRF token for the session.
	csrfToken string

	// HTTP server and lifecycle.
	srv       *http.Server
	listener  net.Listener
	templates *template.Template
	idleTimer *time.Timer
	mu        sync.Mutex

	// MCP setup_web channel: blocks until user submits or timeout.
	setupCh   chan SetupResult
	setupOnce sync.Once
}

// SetupResult is returned to the MCP tool after the user submits the setup form.
type SetupResult struct {
	Keys  []string // keys that were configured
	Error error
}

// New creates a new UI server.
func New(cfg *config.Config, id *identity.Identity, projectDir string) (*Server, error) {
	// Generate CSRF token.
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generating CSRF token: %w", err)
	}

	s := &Server{
		cfg:        cfg,
		id:         id,
		registry:   provider.NewRegistry(provider.ProvidersBaseDir()),
		projectDir: projectDir,
		csrfToken:  hex.EncodeToString(tokenBytes),
	}

	// Load project config if in a project directory.
	if projectDir != "" {
		s.loadProjectConfig()
	}

	// Parse templates.
	tmpl, err := s.parseTemplates()
	if err != nil {
		return nil, fmt.Errorf("parsing templates: %w", err)
	}
	s.templates = tmpl

	return s, nil
}

// Start starts the HTTP server on the given port (0 for random).
func (s *Server) Start(port int) (int, error) {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, fmt.Errorf("listen: %w", err)
	}
	s.listener = ln

	actualPort := ln.Addr().(*net.TCPAddr).Port

	s.srv = &http.Server{
		Handler:      s.middleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	s.resetIdleTimer()

	go s.srv.Serve(ln)

	return actualPort, nil
}

// Stop shuts down the server.
func (s *Server) Stop() {
	if s.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.srv.Shutdown(ctx)
	}
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
}

// WaitForSetup blocks until the setup form is submitted or timeout.
// Used by the MCP valet_setup_web tool.
func (s *Server) WaitForSetup(timeout time.Duration) SetupResult {
	s.setupOnce.Do(func() {
		s.setupCh = make(chan SetupResult, 1)
	})

	select {
	case result := <-s.setupCh:
		return result
	case <-time.After(timeout):
		return SetupResult{Error: fmt.Errorf("setup page timed out")}
	}
}

// OpenBrowser opens the default browser to the given URL.
func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return fmt.Errorf("unsupported platform")
	}
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Static files.
	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Redirect root to stores.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/stores", http.StatusFound)
	})

	// Stores.
	mux.HandleFunc("GET /stores", s.handleStores)
	mux.HandleFunc("GET /stores/{name}", s.handleStoreDetail)
	mux.HandleFunc("GET /stores/{name}/team", s.handleStoreTeam)
	mux.HandleFunc("GET /stores/{name}/environments", s.handleStoreEnvironments)
	mux.HandleFunc("POST /stores/{name}/environments/create", s.handleEnvCreate)
	mux.HandleFunc("POST /stores/{name}/environments/clone", s.handleEnvClone)
	mux.HandleFunc("POST /stores/{name}/environments/delete", s.handleEnvDelete)
	mux.HandleFunc("GET /stores/{name}/rotation", s.handleStoreRotation)
	mux.HandleFunc("GET /stores/{name}/invites", s.handleStoreInvites)
	mux.HandleFunc("GET /stores/{name}/activity", s.handleStoreActivity)
	mux.HandleFunc("GET /stores/{name}/inventory", s.handleStoreInventory)
	mux.HandleFunc("POST /stores/{name}/secrets/{key}/reveal", s.handleSecretReveal)
	mux.HandleFunc("POST /stores/{name}/secrets/{key}/delete", s.handleSecretDelete)
	mux.HandleFunc("POST /stores/{name}/secrets/{key}/set", s.handleSecretSet)
	mux.HandleFunc("POST /stores/{name}/secrets", s.handleSecretAdd)
	mux.HandleFunc("POST /stores/{name}/grant", s.handleTeamGrant)
	mux.HandleFunc("POST /stores/{name}/revoke", s.handleTeamRevoke)
	mux.HandleFunc("POST /stores/{name}/users", s.handleUserAdd)
	mux.HandleFunc("POST /stores/{name}/users/{user}/remove", s.handleUserRemove)
	mux.HandleFunc("POST /stores/{name}/users/{user}/refresh", s.handleUserRefresh)
	mux.HandleFunc("POST /stores/{name}/users/{user}/update", s.handleUserUpdate)
	mux.HandleFunc("POST /stores/{name}/invites", s.handleTeamInviteCreate)
	mux.HandleFunc("POST /stores/{name}/invites/{id}/prune", s.handleTeamInvitePrune)
	mux.HandleFunc("POST /stores/{name}/rotation/clear", s.handleRotationClear)
	mux.HandleFunc("POST /stores/{name}/push", s.handleStorePush)

	// Project.
	mux.HandleFunc("GET /project", s.handleProject)
	mux.HandleFunc("POST /project/select", s.handleProjectSelect)
	mux.HandleFunc("GET /project/pick-folder", s.handleProjectPickFolder)
	mux.HandleFunc("GET /project/adopt-preview", s.handleProjectAdoptPreview)
	mux.HandleFunc("POST /project/adopt", s.handleProjectAdoptApply)
	mux.HandleFunc("GET /project/migrate-preview", s.handleProjectMigratePreview)
	mux.HandleFunc("POST /project/migrate", s.handleProjectMigrateApply)
	mux.HandleFunc("GET /project/setup", s.handleProjectSetup)
	mux.HandleFunc("POST /project/setup", s.handleProjectSetupSave)
	mux.HandleFunc("GET /project/resolution", s.handleProjectResolution)
	mux.HandleFunc("GET /project/links", s.handleProjectLinks)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reset idle timer on every request.
		s.resetIdleTimer()

		// Security headers.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'")

		// CSRF check on POST requests.
		if r.Method == "POST" {
			token := r.FormValue("_csrf")
			if token == "" {
				token = r.Header.Get("X-CSRF-Token")
			}
			if token != s.csrfToken {
				http.Error(w, "invalid CSRF token", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) resetIdleTimer() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	s.idleTimer = time.AfterFunc(idleTimeout, func() {
		fmt.Println("Idle timeout reached, shutting down...")
		s.Stop()
	})
}

func (s *Server) loadProjectConfig() {
	tomlPath, err := config.FindValetToml(s.projectDir)
	if err != nil {
		return
	}
	vc, err := config.LoadValetToml(tomlPath)
	if err != nil {
		return
	}
	s.valetCfg = vc

	tomlDir := strings.TrimSuffix(tomlPath, "/"+config.ValetToml)
	lc, _ := config.LoadLocalConfig(tomlDir)
	s.localCfg = lc
}

func (s *Server) funcMap() template.FuncMap {
	return template.FuncMap{
		"maskSecret": maskSecret,
		"timeAgo":    timeAgo,
		"truncate":   truncate,
		"safeJS":     func(s string) template.JS { return template.JS(s) },
		"urlEncode":  urlEncodeStoreName,
		"upper":      strings.ToUpper,
		"add":        func(a, b int) int { return a + b },
		"seq":        func(n int) []int { r := make([]int, n); for i := range r { r[i] = i }; return r },
		"contains":   strings.Contains,
		"hasPrefix":  strings.HasPrefix,
		"join":       strings.Join,
		"dict": func(pairs ...any) map[string]any {
			m := make(map[string]any)
			for i := 0; i+1 < len(pairs); i += 2 {
				m[pairs[i].(string)] = pairs[i+1]
			}
			return m
		},
	}
}

// pageTemplates caches the parsed template for each page file.
var pageTemplateCache = make(map[string]*template.Template)

func (s *Server) parseTemplates() (*template.Template, error) {
	// Parse only layout + components as the base set.
	base, err := template.New("base").Funcs(s.funcMap()).ParseFS(templateFS,
		"templates/layout.html",
		"templates/components/*.html",
	)
	if err != nil {
		return nil, err
	}
	return base, nil
}

// pageTemplate returns a template that combines the layout + components with
// a specific page template. Each page defines {{define "content"}} which the
// layout calls. Cloning the base avoids cross-page "content" collisions.
func (s *Server) pageTemplate(page string) (*template.Template, error) {
	if t, ok := pageTemplateCache[page]; ok {
		return t, nil
	}
	t, err := s.templates.Clone()
	if err != nil {
		return nil, err
	}
	t, err = t.ParseFS(templateFS, "templates/"+page)
	if err != nil {
		return nil, err
	}
	pageTemplateCache[page] = t
	return t, nil
}

// render executes a page template with common data.
// tmpl is the page file name, e.g. "stores.html".
func (s *Server) render(w http.ResponseWriter, tmpl string, data map[string]any) {
	if data == nil {
		data = make(map[string]any)
	}
	data["CSRFToken"] = s.csrfToken
	data["Version"] = version
	data["Identity"] = s.id
	data["HasProject"] = s.valetCfg != nil
	data["ProjectDir"] = s.projectDir

	// For store detail pages, add the URL-safe encoded name.
	if storeName, ok := data["StoreName"].(string); ok && storeName != "" {
		data["StoreNameEncoded"] = urlEncodeStoreName(storeName)
		if _, hasRemote := data["StoreRemote"]; hasRemote {
			if st, err := s.resolveStoreByName(storeName); err == nil {
				data["HasUnpushed"] = storeHasUnpushed(st)
			}
		}
	}

	t, err := s.pageTemplate(tmpl)
	if err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// openStore opens a store by name from ~/.valet/stores/.
func (s *Server) openStore(name string) (*store.Store, error) {
	return store.FindStoreByName(name, s.id)
}

// openAllStores returns all linked stores in resolution order for the current project.
func (s *Server) openAllStores() []store.LinkedStore {
	if s.valetCfg == nil {
		return nil
	}

	primary, err := s.openEmbeddedStore()
	if err != nil {
		return nil
	}

	var localLinks []domain.StoreLink
	if s.localCfg != nil {
		localLinks = s.localCfg.Stores
	}

	localStore := store.OpenLocalStore(s.projectDir, s.id)
	return store.OpenLinkedStores(localLinks, s.valetCfg.Stores, primary, localStore, s.id)
}

// openEmbeddedStore opens the embedded .valet/ store in the project directory.
func (s *Server) openEmbeddedStore() (*store.Store, error) {
	if s.valetCfg == nil {
		return nil, fmt.Errorf("no project config")
	}
	if s.valetCfg.Store == "." {
		return store.Open(s.projectDir+"/.valet", s.id)
	}
	return store.FindStoreByName(s.valetCfg.Store, s.id)
}

// listAllStores returns all stores visible to the user (personal + project embedded).
func (s *Server) listAllStores() []*store.Store {
	stores, _ := store.ListAllStores(s.id)

	// Add embedded store if in a project.
	if s.valetCfg != nil && s.valetCfg.Store == "." {
		if embedded, err := store.Open(s.projectDir+"/.valet", s.id); err == nil {
			embedded.Meta.Name = s.projectDir + "/.valet"
			stores = append(stores, embedded)
		}
	}

	return stores
}

// version is set from the CLI command.
var version = "dev"

// SetVersion sets the version string shown in the UI.
func SetVersion(v string) {
	version = v
}

// --- Template helpers ---

func maskSecret(value string) string {
	if len(value) <= 8 {
		return "••••••••"
	}
	// Show first prefix chars and last 4.
	prefix := value[:7]
	suffix := value[len(value)-4:]
	return prefix + "****..." + suffix
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case d < 30*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	case d < 365*24*time.Hour:
		months := int(d.Hours() / 24 / 30)
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	default:
		return t.Format("Jan 2006")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
