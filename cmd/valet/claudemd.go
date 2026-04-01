package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// projectHint holds detected information about the project.
type projectHint struct {
	runCommand string // e.g. "npm start", "go run .", "python app.py"
	devCommand string // e.g. "npm run dev", "go run ."
}

// detectProject looks at the current directory to figure out the run command.
func detectProject(dir string) projectHint {
	var h projectHint

	// Node.js / package.json
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			if _, ok := pkg.Scripts["dev"]; ok {
				h.devCommand = "npm run dev"
			}
			if _, ok := pkg.Scripts["start"]; ok {
				h.runCommand = "npm start"
			}
		}
		if h.devCommand == "" && h.runCommand == "" {
			h.runCommand = "npm start"
		}
		return h
	}

	// Python
	if _, err := os.Stat(filepath.Join(dir, "pyproject.toml")); err == nil {
		h.devCommand = detectPythonCommand(dir)
		return h
	}
	if _, err := os.Stat(filepath.Join(dir, "requirements.txt")); err == nil {
		h.devCommand = detectPythonCommand(dir)
		return h
	}

	// Go
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		h.runCommand = "go run ."
		return h
	}

	// Rust
	if _, err := os.Stat(filepath.Join(dir, "Cargo.toml")); err == nil {
		h.runCommand = "cargo run"
		return h
	}

	return h
}

func detectPythonCommand(dir string) string {
	for _, candidate := range []string{"app.py", "main.py", "manage.py", "server.py"} {
		if _, err := os.Stat(filepath.Join(dir, candidate)); err == nil {
			if candidate == "manage.py" {
				return "python manage.py runserver"
			}
			return "python " + candidate
		}
	}
	return "python app.py"
}

// printAITip prints a one-liner tip about adding valet context to AI tool configs.
func printAITip(hint projectHint) {
	cmd := "<command>"
	if hint.devCommand != "" {
		cmd = hint.devCommand
	} else if hint.runCommand != "" {
		cmd = hint.runCommand
	}

	fmt.Println()
	fmt.Println("Tip: Using AI tools? Add this to your CLAUDE.md, AGENTS.md, or similar:")
	fmt.Printf("  This project uses Valet for secrets. Run `valet status` for details, `valet drive -- %s` to run.\n", cmd)
}

// hasAIConfigFiles checks if any common AI tool config files exist.
func hasAIConfigFiles(dir string) bool {
	candidates := []string{
		"CLAUDE.md",
		"AGENTS.md",
		".github/copilot-instructions.md",
		".cursorrules",
		".cursor/rules",
	}
	for _, f := range candidates {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

// maybePrintAITip only shows the tip if the project has AI config files,
// suggesting the user already uses AI tools.
func maybePrintAITip(dir string, hint projectHint) {
	if hasAIConfigFiles(dir) {
		printAITip(hint)
	}
}
