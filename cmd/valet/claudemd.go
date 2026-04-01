package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// projectHint holds detected information about the project.
type projectHint struct {
	runCommand string // e.g. "npm start", "go run .", "python app.py"
	devCommand string // e.g. "npm run dev", "go run ."
	framework  string // e.g. "Next.js", "Flask", "Express"
}

// detectProject looks at the current directory and tries to figure out
// what kind of project this is so we can generate a useful CLAUDE.md snippet.
func detectProject(dir string) projectHint {
	var h projectHint

	// Node.js / package.json
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			if cmd, ok := pkg.Scripts["dev"]; ok {
				h.devCommand = "npm run dev"
				// Detect framework from the dev command.
				switch {
				case strings.Contains(cmd, "next"):
					h.framework = "Next.js"
				case strings.Contains(cmd, "vite"):
					h.framework = "Vite"
				case strings.Contains(cmd, "nuxt"):
					h.framework = "Nuxt"
				}
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
	// Check for common entry points.
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

// valetClaudeMDSnippet generates the CLAUDE.md content for a valet-enabled project.
func valetClaudeMDSnippet(hint projectHint) string {
	driveExample := "valet drive -- <command>"
	if hint.devCommand != "" {
		driveExample = "valet drive -- " + hint.devCommand
	} else if hint.runCommand != "" {
		driveExample = "valet drive -- " + hint.runCommand
	}

	var b strings.Builder
	b.WriteString("## Secrets\n\n")
	b.WriteString("This project uses [Valet](https://github.com/peterday/valet) for secrets management.\n\n")
	b.WriteString("**Key rules:**\n")
	b.WriteString("- Never write secrets or API keys to `.env` files, source code, or config files\n")
	b.WriteString("- Use `valet secret set <NAME>` to store a secret (prompts the user for the value)\n")
	b.WriteString("- Use `valet require <NAME> --provider <provider>` to declare a new secret dependency in `.valet.toml`\n")
	b.WriteString("- Run the app with secrets injected: `" + driveExample + "`\n")
	b.WriteString("\n")
	b.WriteString("**Useful commands:**\n")
	b.WriteString("- `valet status` — check which required secrets are set vs missing\n")
	b.WriteString("- `valet setup` — interactive walkthrough to configure all missing secrets\n")
	b.WriteString("- `valet secret list` — list all secrets in the current environment\n")

	return b.String()
}

// writeClaudeMDSnippet appends or creates the CLAUDE.md with valet instructions.
// Returns true if the file was modified.
func writeClaudeMDSnippet(dir string, hint projectHint) (bool, error) {
	claudePath := filepath.Join(dir, "CLAUDE.md")
	snippet := valetClaudeMDSnippet(hint)

	existing, err := os.ReadFile(claudePath)
	if err == nil {
		// File exists — check if it already has valet instructions.
		if strings.Contains(string(existing), "valet") {
			return false, nil
		}
		// Append to existing file.
		content := string(existing)
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n" + snippet
		return true, os.WriteFile(claudePath, []byte(content), 0644)
	}

	if !os.IsNotExist(err) {
		return false, err
	}

	// Create new file.
	return true, os.WriteFile(claudePath, []byte(snippet), 0644)
}

// offerClaudeMD prompts the user to add valet instructions to CLAUDE.md.
func offerClaudeMD(dir string, hint projectHint) {
	claudePath := filepath.Join(dir, "CLAUDE.md")

	// Check if CLAUDE.md already has valet instructions.
	if existing, err := os.ReadFile(claudePath); err == nil {
		if strings.Contains(string(existing), "valet") {
			return
		}
	}

	fmt.Print("\nAdd valet instructions to CLAUDE.md for AI tools? [Y/n]: ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer != "" && answer != "y" && answer != "yes" {
		return
	}

	wrote, err := writeClaudeMDSnippet(dir, hint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write CLAUDE.md: %v\n", err)
		return
	}
	if wrote {
		if _, err := os.Stat(claudePath); err == nil {
			fmt.Println("Updated CLAUDE.md with valet instructions.")
		}
	}
}
