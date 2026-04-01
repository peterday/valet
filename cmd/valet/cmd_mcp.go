package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/mcpserver"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "AI tool integration via Model Context Protocol",
	Long: `Integrate Valet with AI coding tools (Claude Code, Cursor, etc.) via MCP.

  valet mcp install    # register Valet with your AI tools
  valet mcp serve      # start MCP server (used by AI tools, not run directly)`,
}

var mcpServeCmd = &cobra.Command{
	Use:    "serve",
	Short:  "Start MCP server over stdio (called by AI tools)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return mcpserver.Serve(version)
	},
}

var mcpInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Register Valet as an MCP server with your AI tools",
	Long: `Register Valet as an MCP server so AI coding tools can manage secrets directly.

  valet mcp install                    # auto-detect and configure
  valet mcp install --claude-code      # Claude Code only
  valet mcp install --cursor           # Cursor only`,
	RunE: func(cmd *cobra.Command, args []string) error {
		valetPath, err := os.Executable()
		if err != nil {
			valetPath = "valet"
		}

		installed := false

		// Claude Code
		if mcpClaudeCodeFlag || mcpAllFlag || (!mcpCursorFlag && !mcpClaudeCodeFlag) {
			if ok, err := installClaudeCode(valetPath); err != nil {
				fmt.Fprintf(os.Stderr, "Claude Code: %v\n", err)
			} else if ok {
				fmt.Println("✓ Claude Code — registered Valet MCP server (user scope)")
				installed = true
			}
		}

		// Cursor
		if mcpCursorFlag || mcpAllFlag || (!mcpCursorFlag && !mcpClaudeCodeFlag) {
			if ok, err := installCursor(valetPath); err != nil {
				fmt.Fprintf(os.Stderr, "Cursor: %v\n", err)
			} else if ok {
				fmt.Println("✓ Cursor — registered Valet MCP server")
				installed = true
			}
		}

		if !installed {
			fmt.Println("No AI tools detected. To configure manually, add this MCP server:")
			fmt.Printf("  command: %s\n", valetPath)
			fmt.Println("  args: [\"mcp\", \"serve\"]")
			fmt.Println("  transport: stdio")
			return nil
		}

		fmt.Println("\nValet is now available as an MCP tool. Start a new session to use it.")
		return nil
	},
}

func installClaudeCode(valetPath string) (bool, error) {
	// Check if claude CLI is available.
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return false, nil // Not installed, skip silently.
	}

	// Use `claude mcp add` with user scope (available across all projects).
	cmd := exec.Command(claudePath, "mcp", "add",
		"--transport", "stdio",
		"--scope", "user",
		"valet",
		"--",
		valetPath, "mcp", "serve",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("claude mcp add failed: %w", err)
	}
	return true, nil
}

func installCursor(valetPath string) (bool, error) {
	// Cursor uses ~/.cursor/mcp.json
	home, err := os.UserHomeDir()
	if err != nil {
		return false, nil
	}

	cursorDir := filepath.Join(home, ".cursor")
	if _, err := os.Stat(cursorDir); os.IsNotExist(err) {
		return false, nil // Cursor not installed.
	}

	mcpPath := filepath.Join(cursorDir, "mcp.json")

	// Read existing config or start fresh.
	var cfg map[string]any
	if data, err := os.ReadFile(mcpPath); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			cfg = make(map[string]any)
		}
	} else {
		cfg = make(map[string]any)
	}

	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		servers = make(map[string]any)
	}

	// Check if already configured.
	if _, exists := servers["valet"]; exists {
		return true, nil
	}

	servers["valet"] = map[string]any{
		"command": valetPath,
		"args":    []string{"mcp", "serve"},
	}
	cfg["mcpServers"] = servers

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, err
	}

	if err := os.WriteFile(mcpPath, data, 0644); err != nil {
		return false, err
	}

	return true, nil
}

var (
	mcpClaudeCodeFlag bool
	mcpCursorFlag     bool
	mcpAllFlag        bool
)

func init() {
	mcpInstallCmd.Flags().BoolVar(&mcpClaudeCodeFlag, "claude-code", false, "configure Claude Code only")
	mcpInstallCmd.Flags().BoolVar(&mcpCursorFlag, "cursor", false, "configure Cursor only")
	mcpInstallCmd.Flags().BoolVar(&mcpAllFlag, "all", false, "configure all detected tools")
	mcpCmd.AddCommand(mcpServeCmd)
	mcpCmd.AddCommand(mcpInstallCmd)
	rootCmd.AddCommand(mcpCmd)
}
