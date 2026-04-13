package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/ui"
)

var uiPort int

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Open the web dashboard",
	Long: `Starts a localhost web server and opens the valet dashboard in your browser.

Store-centric navigation: secrets (All/per-env), team, environments,
rotation, invites, activity. Supports adding secrets, managing users,
creating/cloning environments, and pushing to git remotes.

If run in a project directory with .env.example but no .valet.toml,
shows an adopt banner to bootstrap the project.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := loadIdentity()
		if err != nil {
			return fmt.Errorf("loading identity: %w", err)
		}

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		// Detect project directory.
		cwd, _ := os.Getwd()
		projectDir := ""
		if _, err := config.FindValetToml(cwd); err == nil {
			projectDir = cwd
		}

		ui.SetVersion(version)

		srv, err := ui.New(cfg, id, projectDir)
		if err != nil {
			return fmt.Errorf("creating UI server: %w", err)
		}

		port, err := srv.Start(uiPort)
		if err != nil {
			return fmt.Errorf("starting server: %w", err)
		}

		url := fmt.Sprintf("http://127.0.0.1:%d", port)
		fmt.Printf("Valet dashboard running at %s\n", url)

		if err := ui.OpenBrowser(url); err != nil {
			fmt.Printf("Open %s in your browser\n", url)
		}

		// Wait for interrupt.
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig

		fmt.Println("\nShutting down...")
		srv.Stop()
		return nil
	},
}

func init() {
	uiCmd.Flags().IntVar(&uiPort, "port", 0, "specific port (default: random)")
	rootCmd.AddCommand(uiCmd)
}
