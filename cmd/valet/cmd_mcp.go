package main

import (
	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/mcpserver"
)

var mcpCmd = &cobra.Command{
	Use:    "mcp",
	Short:  "Start MCP server over stdio",
	Long:   `Start a Model Context Protocol server for AI tool integration (Claude Code, Codex, etc).`,
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return mcpserver.Serve(version)
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}
