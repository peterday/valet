package ui

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/identity"
)

// SetupWeb starts the UI server, opens the browser to the setup page,
// and blocks until the user submits or timeout. Returns the result.
// This is called by the MCP valet_setup_web tool.
func SetupWeb(cfg *config.Config, id *identity.Identity, projectDir string, keys []string) (string, error) {
	srv, err := New(cfg, id, projectDir)
	if err != nil {
		return "", fmt.Errorf("creating UI server: %w", err)
	}

	port, err := srv.Start(0) // random port
	if err != nil {
		return "", fmt.Errorf("starting UI server: %w", err)
	}
	defer srv.Stop()

	// Build the setup URL with optional keys filter.
	url := fmt.Sprintf("http://127.0.0.1:%d/project/setup", port)
	if len(keys) > 0 {
		url += "?keys=" + strings.Join(keys, ",")
	}

	if err := OpenBrowser(url); err != nil {
		return "", fmt.Errorf("opening browser: %w (URL: %s)", err, url)
	}

	// Block until setup completes or timeout.
	result := srv.WaitForSetup(setupTimeout)
	if result.Error != nil {
		return "", result.Error
	}

	return fmt.Sprintf("Configured %d secrets: %s", len(result.Keys), strings.Join(result.Keys, ", ")), nil
}

// singleton server for sharing between CLI and MCP.
var (
	sharedServer *Server
	sharedMu     sync.Mutex
)

// GetOrStartServer returns an existing server or starts a new one.
// Used when MCP tool wants to reuse an already-running `valet ui` server.
func GetOrStartServer(cfg *config.Config, id *identity.Identity, projectDir string) (*Server, int, error) {
	sharedMu.Lock()
	defer sharedMu.Unlock()

	if sharedServer != nil {
		// Server already running — return it.
		port := sharedServer.listener.Addr().(*net.TCPAddr).Port
		return sharedServer, port, nil
	}

	srv, err := New(cfg, id, projectDir)
	if err != nil {
		return nil, 0, err
	}

	port, err := srv.Start(0)
	if err != nil {
		return nil, 0, err
	}

	sharedServer = srv
	return srv, port, nil
}
