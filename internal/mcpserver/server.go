package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/peterday/valet/internal/config"
	"github.com/peterday/valet/internal/domain"
	"github.com/peterday/valet/internal/identity"
	"github.com/peterday/valet/internal/provider"
	"github.com/peterday/valet/internal/store"
)

// Serve starts the MCP server over stdio.
func Serve(version string) error {
	s := server.NewMCPServer(
		"valet",
		version,
		server.WithToolCapabilities(true),
	)

	s.AddTool(initTool, initHandler)
	s.AddTool(scanTool, scanHandler)
	s.AddTool(statusTool, statusHandler)
	s.AddTool(walletSearchTool, walletSearchHandler)
	s.AddTool(linkTool, linkHandler)
	s.AddTool(copyTool, copyHandler)
	s.AddTool(requireTool, requireHandler)
	s.AddTool(providerSearchTool, providerSearchHandler)
	s.AddTool(helpTool, helpHandler)

	return server.ServeStdio(s)
}

// --- valet_init: initialize valet in a project ---

var initTool = mcp.NewTool("valet_init",
	mcp.WithDescription(`Set up Valet in the current project. Call this when the user asks to add secrets management or when valet_status reports no .valet.toml.

Two modes:
- Without 'mode': returns project info, available stores, and options — present these to the user and ask how they want to store secrets
- With 'mode': runs the init command with the chosen configuration

Always call without 'mode' first so the user can choose.`),
	mcp.WithString("mode", mcp.Description("How to store secrets. Options: 'embedded' (in .valet/, committed encrypted), 'personal' (link existing personal store), 'shared' (link team store). Omit to see options first.")),
	mcp.WithString("store", mcp.Description("Store name or URI for personal/shared mode (e.g. my-keys, github:acme/secrets)")),
)

func initHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return errResult("cannot get working directory: %v", err)
	}

	// Check if already initialized.
	if _, err := config.FindValetToml(cwd); err == nil {
		return mcp.NewToolResultText("Project already initialized — .valet.toml exists.\n\nUse valet_status to see current state, or valet_scan to detect .env files."), nil
	}

	mode := req.GetString("mode", "")
	storeName := req.GetString("store", "")

	// If no mode specified, return options for the user to choose.
	if mode == "" {
		return initOptionsHandler(cwd)
	}

	// Run init in-process (no subprocess — avoids macOS codesign issues).
	id, err := identity.LoadOrInit()
	if err != nil {
		return errResult("identity init failed: %v", err)
	}

	var initMsg string

	switch mode {
	case "embedded":
		storeRoot := filepath.Join(cwd, ".valet")
		if _, err := os.Stat(filepath.Join(storeRoot, "store.json")); err == nil {
			return errResult("valet already initialized in this directory")
		}
		s, err := store.Create(storeRoot, "default", domain.StoreTypeEmbedded, id)
		if err != nil {
			return errResult("creating store: %v", err)
		}
		s.AddUser("me", "", id.PublicKey)
		s.CreateProject("default")
		s.CreateEnvironment("default", "dev")
		s.CreateScope("default", "dev/default")

		vc := &domain.ValetConfig{Store: ".", Project: "default", DefaultEnv: "dev"}
		tomlPath := filepath.Join(cwd, ".valet.toml")
		if err := config.WriteValetToml(tomlPath, vc); err != nil {
			return errResult("writing .valet.toml: %v", err)
		}
		initMsg = "Initialized embedded store in .valet/"

	case "personal":
		if storeName == "" {
			return errResult("'store' is required for personal mode (e.g. my-keys)")
		}
		if _, err := store.FindStoreByName(storeName, id); err != nil {
			return errResult("personal store %q not found — create it with: valet store create %s", storeName, storeName)
		}
		vc := &domain.ValetConfig{Store: storeName, Project: "default", DefaultEnv: "dev"}
		tomlPath := filepath.Join(cwd, ".valet.toml")
		if err := config.WriteValetToml(tomlPath, vc); err != nil {
			return errResult("writing .valet.toml: %v", err)
		}
		lc := &domain.LocalConfig{Stores: []domain.StoreLink{{Name: storeName}}}
		if err := config.WriteLocalConfig(cwd, lc); err != nil {
			return errResult("writing .valet.local.toml: %v", err)
		}
		initMsg = fmt.Sprintf("Linked personal store %q", storeName)

	case "shared":
		if storeName == "" {
			return errResult("'store' is required for shared mode (e.g. github:acme/secrets)")
		}
		uri := store.ParseStoreURI(storeName)
		link := domain.StoreLink{Name: uri.StoreName}
		if uri.IsRemote {
			link.URL = uri.Remote
		}
		vc := &domain.ValetConfig{
			Store: storeName, Project: "default", DefaultEnv: "dev",
			Stores: []domain.StoreLink{link},
		}
		tomlPath := filepath.Join(cwd, ".valet.toml")
		if err := config.WriteValetToml(tomlPath, vc); err != nil {
			return errResult("writing .valet.toml: %v", err)
		}
		initMsg = fmt.Sprintf("Linked shared store %q", storeName)

	default:
		return errResult("unknown mode %q — use 'embedded', 'personal', or 'shared'", mode)
	}

	snippet := generateClaudeMDSnippet(cwd)

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", initMsg)
	fmt.Fprintf(&b, "\n---\n\n")
	fmt.Fprintf(&b, "Add this to the project's CLAUDE.md (create if it doesn't exist):\n\n")
	fmt.Fprintf(&b, "```markdown\n%s```\n\n", snippet)
	fmt.Fprintf(&b, "Next steps:\n")
	fmt.Fprintf(&b, "1. Write the CLAUDE.md snippet above to the project\n")
	fmt.Fprintf(&b, "2. Call valet_scan to detect existing .env files and match against wallet/providers\n")
	fmt.Fprintf(&b, "3. Based on scan results, link wallet and/or ask user to type: ! valet import .env\n")
	fmt.Fprintf(&b, "4. Call valet_require for each key to declare requirements")

	return mcp.NewToolResultText(b.String()), nil
}

func initOptionsHandler(cwd string) (*mcp.CallToolResult, error) {
	var b strings.Builder

	// Detect project type.
	runCmd := detectRunCommand(cwd)
	fmt.Fprintf(&b, "Project detected: run command is `%s`\n\n", runCmd)

	// Check for existing .env files.
	envFiles := findEnvFiles(cwd)
	if len(envFiles) > 0 {
		fmt.Fprintf(&b, "Existing .env files found:\n")
		for _, f := range envFiles {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		fmt.Fprintf(&b, "\n")
	}

	// List available personal stores.
	id, err := identity.Load()
	if err == nil {
		stores, _ := store.ListAllStores(id)
		if len(stores) > 0 {
			fmt.Fprintf(&b, "Available personal stores:\n")
			for _, s := range stores {
				fmt.Fprintf(&b, "  %s\n", s.Meta.Name)
			}
			fmt.Fprintf(&b, "\n")
		}
	}

	fmt.Fprintf(&b, "Ask the user how they want to store secrets:\n\n")
	fmt.Fprintf(&b, "1. **Embedded** (recommended for most projects)\n")
	fmt.Fprintf(&b, "   Secrets encrypted in .valet/ inside the project. Safe to commit.\n")
	fmt.Fprintf(&b, "   → call valet_init with mode='embedded'\n\n")
	fmt.Fprintf(&b, "2. **Link personal store**\n")
	fmt.Fprintf(&b, "   Use keys from an existing personal store (e.g. my-keys).\n")
	fmt.Fprintf(&b, "   → call valet_init with mode='personal', store='<name>'\n\n")
	fmt.Fprintf(&b, "3. **Link shared/team store**\n")
	fmt.Fprintf(&b, "   Link a git-backed team store for shared secrets.\n")
	fmt.Fprintf(&b, "   → call valet_init with mode='shared', store='github:org/repo'\n")

	return mcp.NewToolResultText(b.String()), nil
}

// findEnvFiles returns .env file paths relative to dir.
func findEnvFiles(dir string) []string {
	candidates := []string{
		".env", ".env.local", ".env.development", ".env.staging",
		".env.production", ".env.test", ".env.dev", ".env.prod",
	}
	// Also check common subdirectories.
	subdirs := []string{"", "web", "apps/web", "frontend", "backend", "server", "api"}

	var found []string
	for _, sub := range subdirs {
		base := dir
		if sub != "" {
			base = filepath.Join(dir, sub)
		}
		for _, name := range candidates {
			path := filepath.Join(base, name)
			if _, err := os.Stat(path); err == nil {
				rel := name
				if sub != "" {
					rel = sub + "/" + name
				}
				found = append(found, rel)
			}
		}
	}
	return found
}

// generateClaudeMDSnippet returns the recommended CLAUDE.md content for a project.
func generateClaudeMDSnippet(dir string) string {
	runCmd := detectRunCommand(dir)

	var b strings.Builder
	b.WriteString("## Secrets\n\n")
	b.WriteString("This project uses [Valet](https://github.com/peterday/valet) for encrypted secrets management.\n\n")
	fmt.Fprintf(&b, "- Run commands with `valet run -- %s` to inject secrets at runtime\n", runCmd)
	b.WriteString("- Never put secrets in .env files, source code, environment variables, or shell commands\n")
	b.WriteString("- Never use `valet secret get`, `valet resolve --show`, or `--value` flags — keep secrets out of AI context\n")
	b.WriteString("- To set a secret, ask the user to type: `! valet secret set KEY`\n")
	b.WriteString("- To set up all missing secrets interactively: `! valet setup`\n")
	b.WriteString("\n")
	b.WriteString("When adding a dependency that uses an external API:\n")
	b.WriteString("1. Search for its provider: `valet_provider_search`\n")
	b.WriteString("2. Declare requirements: `valet_require` with provider name\n")
	b.WriteString("3. Check user's wallet: `valet_wallet_search`\n")
	b.WriteString("4. If missing, ask user to type: `! valet setup`\n")
	return b.String()
}

// detectRunCommand figures out the project's run command from its files.
func detectRunCommand(dir string) string {
	// Node.js
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			if _, ok := pkg.Scripts["dev"]; ok {
				return "npm run dev"
			}
			if _, ok := pkg.Scripts["start"]; ok {
				return "npm start"
			}
		}
		return "npm start"
	}
	// Python
	for _, f := range []string{"pyproject.toml", "requirements.txt"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			for _, entry := range []string{"app.py", "main.py", "manage.py"} {
				if _, err := os.Stat(filepath.Join(dir, entry)); err == nil {
					if entry == "manage.py" {
						return "python manage.py runserver"
					}
					return "python " + entry
				}
			}
			return "python app.py"
		}
	}
	// Go
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return "go run ."
	}
	// Rust
	if _, err := os.Stat(filepath.Join(dir, "Cargo.toml")); err == nil {
		return "cargo run"
	}
	return "<command>"
}

// --- valet_scan: scan project for existing secrets ---

var scanTool = mcp.NewTool("valet_scan",
	mcp.WithDescription(`Scan the project for existing .env files and report what secrets are defined. Call this right after valet_init to understand what the project already has.

Returns:
- Which .env files exist and what key names they contain (NEVER values)
- Which keys match known providers (OpenAI, Stripe, etc.)
- Which keys already exist in the user's personal wallet

Based on the results, present the user with options:
- Link their wallet for keys they already have (valet_link)
- Import .env into the project store (ask user to type: ! valet import .env)
- Declare requirements for all keys (valet_require)`),
)

func scanHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return errResult("cannot get working directory: %v", err)
	}

	// Find .env files including in subdirectories.
	envFilePaths := findEnvFiles(cwd)

	type envFileInfo struct {
		Name string
		Keys []string
	}
	var found []envFileInfo
	allKeys := make(map[string]string) // key → which file it came from

	for _, relPath := range envFilePaths {
		absPath := filepath.Join(cwd, relPath)
		keys, err := scanEnvFileKeys(absPath)
		if err != nil || len(keys) == 0 {
			continue
		}
		found = append(found, envFileInfo{Name: relPath, Keys: keys})
		for _, k := range keys {
			if _, exists := allKeys[k]; !exists {
				allKeys[k] = relPath
			}
		}
	}

	if len(found) == 0 {
		return mcp.NewToolResultText("No .env files found in the project directory.\n\nUse valet_provider_search to discover what API keys the project needs, then valet_require to declare them."), nil
	}

	var b strings.Builder

	// List files and keys.
	fmt.Fprintf(&b, "Found %d .env file(s) with %d unique key(s):\n\n", len(found), len(allKeys))
	for _, f := range found {
		fmt.Fprintf(&b, "  %s (%d keys): %s\n", f.Name, len(f.Keys), strings.Join(f.Keys, ", "))
	}

	// Group keys by provider.
	providerKeys := make(map[string][]string)   // provider name → keys
	providerInfo := make(map[string]*provider.Provider)
	var unmatchedKeys []string

	for key := range allKeys {
		p := provider.FindByEnvVar(key)
		if p != nil {
			providerKeys[p.Name] = append(providerKeys[p.Name], key)
			providerInfo[p.Name] = p
		} else {
			unmatchedKeys = append(unmatchedKeys, key)
		}
	}

	if len(providerKeys) > 0 {
		fmt.Fprintf(&b, "\nProvider matches:\n")
		for name, keys := range providerKeys {
			p := providerInfo[name]
			fmt.Fprintf(&b, "  %s: %s\n", p.DisplayName, strings.Join(keys, ", "))
		}
	}

	// Search wallet for each key.
	id, err := identity.Load()
	walletMatches := make(map[string][]string) // key → store names
	storesUsed := make(map[string]bool)
	if err == nil {
		for key := range allKeys {
			matches, err := store.SearchStoresForSecret(key, "dev", id)
			if err != nil || len(matches) == 0 {
				continue
			}
			for _, m := range matches {
				walletMatches[key] = append(walletMatches[key], m.StoreName)
				storesUsed[m.StoreName] = true
			}
		}
	}

	if len(walletMatches) > 0 {
		fmt.Fprintf(&b, "\nAlready in your wallet:\n")
		for key, stores := range walletMatches {
			fmt.Fprintf(&b, "  %-28s in %s\n", key, strings.Join(stores, ", "))
		}
		var storeNames []string
		for s := range storesUsed {
			storeNames = append(storeNames, s)
		}
		fmt.Fprintf(&b, "\nAsk the user: should I link your wallet (%s) or import everything from .env?\n", strings.Join(storeNames, ", "))
		fmt.Fprintf(&b, "  Link: call valet_link for each store — keys auto-update on rotation\n")
		fmt.Fprintf(&b, "  Import: ask user to type ! valet import .env — project owns copies\n")
		fmt.Fprintf(&b, "  Both: link wallet for keys already there, import .env for the rest\n")
	}

	// Import instructions for each .env file.
	fmt.Fprintf(&b, "\nTo import, ask the user to type:\n")
	for _, f := range found {
		env := "dev"
		if strings.Contains(f.Name, "production") || strings.Contains(f.Name, "prod") {
			env = "prod"
		} else if strings.Contains(f.Name, "staging") {
			env = "staging"
		} else if strings.Contains(f.Name, "test") {
			env = "test"
		}
		if env == "dev" {
			fmt.Fprintf(&b, "  ! valet import %s\n", f.Name)
		} else {
			fmt.Fprintf(&b, "  ! valet import %s -e %s\n", f.Name, env)
		}
	}

	// Declare requirements — give Claude exact tool calls.
	fmt.Fprintf(&b, "\nTo declare requirements, call valet_require:\n")
	for name := range providerKeys {
		fmt.Fprintf(&b, "  valet_require provider=%q    (declares all %s env vars)\n", name, providerInfo[name].DisplayName)
	}
	for _, key := range unmatchedKeys {
		fmt.Fprintf(&b, "  valet_require key=%q\n", key)
	}

	fmt.Fprintf(&b, "\nAfter import, .env files can be deleted (secrets are now encrypted in .valet/).")

	return mcp.NewToolResultText(b.String()), nil
}

// scanEnvFileKeys reads a .env file and returns just the key names (never values).
func scanEnvFileKeys(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var keys []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		key = strings.TrimPrefix(key, "export ")
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys, scanner.Err()
}

// --- valet_status: the "tell me everything" tool ---

var statusTool = mcp.NewTool("valet_status",
	mcp.WithDescription(`Show complete project status: configuration, environments, secrets, requirements, and team members. Call this when:
- You first open a project to understand its secrets setup
- A command fails with a missing environment variable
- After declaring new requirements to verify they're satisfied

Never returns secret values. If no .valet.toml exists, use valet_init to set up the project.`),
	mcp.WithString("env", mcp.Description("Environment to check (default: dev)")),
)

func statusHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	env := req.GetString("env", "dev")

	cwd, err := os.Getwd()
	if err != nil {
		return errResult("cannot get working directory: %v", err)
	}

	tomlPath, err := config.FindValetToml(cwd)
	if err != nil {
		return mcp.NewToolResultText("No .valet.toml found — this project hasn't been initialized with Valet.\n\nUse valet_init to set up encrypted secrets management.\nThen use valet_provider_search to discover what API keys the project needs."), nil
	}

	vc, err := config.LoadValetToml(tomlPath)
	if err != nil {
		return errResult("reading config: %v", err)
	}

	id, err := identity.Load()
	if err != nil {
		return errResult("no identity found — run 'valet identity init' first")
	}

	var b strings.Builder

	// Project config.
	fmt.Fprintf(&b, "Project: %s\n", vc.Project)
	fmt.Fprintf(&b, "Store: %s\n", vc.Store)
	fmt.Fprintf(&b, "Default environment: %s\n", vc.DefaultEnv)

	if len(vc.Stores) > 0 {
		fmt.Fprintf(&b, "Linked shared stores: %s\n", strings.Join(store.StoreLinkNames(vc.Stores), ", "))
	}
	tomlDir := filepath.Dir(tomlPath)
	lc, _ := config.LoadLocalConfig(tomlDir)
	if len(lc.Stores) > 0 {
		fmt.Fprintf(&b, "Linked personal stores: %s\n", strings.Join(store.StoreLinkNames(lc.Stores), ", "))
	}

	s, err := store.Resolve(id)
	if err != nil {
		return errResult("opening store: %v", err)
	}

	project, err := s.ResolveDefaultProject()
	if err != nil {
		return errResult("no project found: %v", err)
	}

	// Environments and secrets.
	envs, _ := s.ListEnvironments(project)
	if len(envs) > 0 {
		fmt.Fprintf(&b, "\nEnvironments:\n")
		for _, e := range envs {
			secrets, _ := s.ListSecretsInEnv(project, e)
			marker := ""
			if e == env {
				marker = " (active)"
			}
			fmt.Fprintf(&b, "  %s%s — %d secret(s)\n", e, marker, len(secrets))
			for name, scope := range secrets {
				fmt.Fprintf(&b, "    %-28s %s\n", name, scope)
			}
		}
	}

	// Team members.
	users, _ := s.ListUsers()
	if len(users) > 0 {
		fmt.Fprintf(&b, "\nTeam members:\n")
		for _, u := range users {
			fmt.Fprintf(&b, "  • %s", u.Name)
			if u.GitHub != "" {
				fmt.Fprintf(&b, " (@%s)", u.GitHub)
			}
			fmt.Fprintf(&b, "\n")
		}
	}

	// Requirements check.
	if len(vc.Requires) > 0 {
		stores, err := openAllProjectStores(id)
		if err != nil {
			return errResult("opening stores: %v", err)
		}
		resolved, _ := store.ResolveAllSecrets(stores, env)

		fmt.Fprintf(&b, "\nRequirements (%s):\n", env)
		missing := 0
		for name, r := range vc.Requires {
			if rs, found := resolved[name]; found {
				fmt.Fprintf(&b, "  ✓ %-28s from %s/%s\n", name, rs.StoreName, rs.ScopePath)
			} else if r.Optional {
				fmt.Fprintf(&b, "  - %-28s optional, not set\n", name)
			} else {
				hint := ""
				// Check provider registry for setup guidance.
				p := provider.FindByEnvVar(name)
				if p == nil && r.Provider != "" {
					p = provider.Get(r.Provider)
				}
				if p != nil {
					hint = fmt.Sprintf(" [%s — %s]", p.DisplayName, p.SetupURL)
					if p.FreeTier != "" {
						hint = fmt.Sprintf(" [%s — %s, free: %s]", p.DisplayName, p.SetupURL, p.FreeTier)
					}
				}
				fmt.Fprintf(&b, "  ✗ %-28s MISSING%s\n", name, hint)
				missing++
			}
		}
		if missing > 0 {
			fmt.Fprintf(&b, "\n%d required secret(s) missing.\n", missing)
			fmt.Fprintf(&b, "Use valet_wallet_search to check if the user already has these keys.\n")
			fmt.Fprintf(&b, "For missing keys, ask the user to type: ! valet setup")
		}
	} else {
		fmt.Fprintf(&b, "\nNo requirements declared. Use valet_require to declare what secrets this project needs.")
	}

	return mcp.NewToolResultText(b.String()), nil
}

// --- valet_wallet_search: check personal stores ---

var walletSearchTool = mcp.NewTool("valet_wallet_search",
	mcp.WithDescription(`Search the user's personal stores (wallet) for a secret by name. Call this after valet_require to check if the user already has the key — saves them from entering it again.

Never returns secret values — only reports which stores have the key.`),
	mcp.WithString("key", mcp.Required(), mcp.Description("Secret name to search for (e.g. OPENAI_API_KEY)")),
	mcp.WithString("env", mcp.Description("Environment to search (default: dev)")),
)

func walletSearchHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key := req.GetString("key", "")
	env := req.GetString("env", "dev")

	id, err := identity.Load()
	if err != nil {
		return errResult("no identity found — run 'valet identity init' first")
	}

	matches, err := store.SearchStoresForSecret(key, env, id)
	if err != nil {
		return errResult("searching stores: %v", err)
	}

	// Look up provider info for this key (used in both found and not-found paths).
	p := provider.FindByEnvVar(key)

	if len(matches) == 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "%s not found in any personal store.\n\n", key)
		if p != nil {
			fmt.Fprintf(&b, "This is a %s key.\n", p.DisplayName)
			fmt.Fprintf(&b, "Get one at: %s\n", p.SetupURL)
			if p.FreeTier != "" {
				fmt.Fprintf(&b, "Free tier: %s\n", p.FreeTier)
			}
			fmt.Fprintf(&b, "\n")
		}
		fmt.Fprintf(&b, "Present this to the user:\n")
		if p != nil {
			fmt.Fprintf(&b, "  \"You need a %s API key. ", p.DisplayName)
			if p.FreeTier != "" {
				fmt.Fprintf(&b, "They offer %s. ", p.FreeTier)
			}
			fmt.Fprintf(&b, "Get one at %s, then type `! valet secret set %s` to save it.\"\n", p.SetupURL, key)
		} else {
			fmt.Fprintf(&b, "  \"Type `! valet secret set %s` to enter the value.\"\n", key)
		}
		fmt.Fprintf(&b, "\nDo NOT pass the secret via --value or through your context.")
		return mcp.NewToolResultText(b.String()), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s found in %d store(s):\n", key, len(matches))
	for _, m := range matches {
		fmt.Fprintf(&b, "  • %s (scope: %s)\n", m.StoreName, m.ScopePath)
	}

	fmt.Fprintf(&b, "\nPresent these options to the user:\n")

	// Option: link/copy from wallet.
	if len(matches) == 1 {
		m := matches[0]
		fmt.Fprintf(&b, "  1. Link store %q — all its keys become available, auto-updates on rotation\n", m.StoreName)
		fmt.Fprintf(&b, "     → call valet_link with store=%q\n", m.StoreName)
		fmt.Fprintf(&b, "  2. Copy just this key — project owns its own copy\n")
		fmt.Fprintf(&b, "     → call valet_copy with key=%q from=%q\n", key, m.StoreName)
	} else {
		for i, m := range matches {
			fmt.Fprintf(&b, "  %d. Use from %s (link or copy)\n", i+1, m.StoreName)
			fmt.Fprintf(&b, "     Link → valet_link store=%q\n", m.StoreName)
			fmt.Fprintf(&b, "     Copy → valet_copy key=%q from=%q\n", key, m.StoreName)
		}
	}

	// Option: create a new key.
	if p != nil {
		fmt.Fprintf(&b, "  %d. Create a new key at %s", len(matches)+1, p.SetupURL)
		if p.FreeTier != "" {
			fmt.Fprintf(&b, " (%s)", p.FreeTier)
		}
		fmt.Fprintf(&b, "\n     → ask user to type: ! valet secret set %s\n", key)
	} else {
		fmt.Fprintf(&b, "  %d. Enter a new value manually\n", len(matches)+1)
		fmt.Fprintf(&b, "     → ask user to type: ! valet secret set %s\n", key)
	}

	return mcp.NewToolResultText(b.String()), nil
}

// --- valet_link: link a store to the project ---

var linkTool = mcp.NewTool("valet_link",
	mcp.WithDescription(`Link a personal or team store to this project. All secrets from the linked store become available via valet run. No secret values are exposed — this just creates a reference.

Use this after valet_wallet_search finds a key in a store. Linking makes ALL keys from that store available (not just one). The link is gitignored by default (personal).

Link vs copy:
- Link: key stays in source store, auto-updates on rotation, links entire store
- Copy (use valet_copy): project owns its own copy, self-contained, single key only`),
	mcp.WithString("store", mcp.Required(), mcp.Description("Store name to link (e.g. my-keys, work-keys)")),
	mcp.WithBoolean("shared", mcp.Description("If true, commits link to .valet.toml (team). Default: false (personal, gitignored)")),
)

func linkHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	storeName := req.GetString("store", "")
	shared := req.GetBool("shared", false)

	if storeName == "" {
		return errResult("store name is required")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return errResult("cannot get working directory: %v", err)
	}

	tomlPath, err := config.FindValetToml(cwd)
	if err != nil {
		return errResult("no .valet.toml found — use valet_init first")
	}
	tomlDir := filepath.Dir(tomlPath)

	id, err := identity.Load()
	if err != nil {
		return errResult("no identity found — run 'valet identity init' first")
	}

	// Verify the store exists.
	if _, err := store.FindStoreByName(storeName, id); err != nil {
		return errResult("store %q not found — check store name with valet_wallet_search", storeName)
	}

	link := domain.StoreLink{Name: storeName}

	if shared {
		vc, err := config.LoadValetToml(tomlPath)
		if err != nil {
			return errResult("reading config: %v", err)
		}
		if store.HasStoreLink(vc.Stores, link.Name) {
			return mcp.NewToolResultText(fmt.Sprintf("Store %q is already linked (shared).", link.Name)), nil
		}
		vc.Stores = append(vc.Stores, link)
		if err := config.WriteValetToml(tomlPath, vc); err != nil {
			return errResult("writing config: %v", err)
		}
		return mcp.NewToolResultText(fmt.Sprintf("Linked %q (shared, in .valet.toml). All its secrets are now available via `valet run -- <command>`.", link.Name)), nil
	}

	// Personal link (gitignored).
	lc, _ := config.LoadLocalConfig(tomlDir)
	if store.HasStoreLink(lc.Stores, link.Name) {
		return mcp.NewToolResultText(fmt.Sprintf("Store %q is already linked (personal).", link.Name)), nil
	}
	lc.Stores = append(lc.Stores, link)
	if err := config.WriteLocalConfig(tomlDir, lc); err != nil {
		return errResult("writing local config: %v", err)
	}

	return mcp.NewToolResultText(fmt.Sprintf("Linked %q (personal, gitignored). All its secrets are now available via `valet run -- <command>`.", link.Name)), nil
}

// --- valet_copy: copy a single key from a store into the project ---

var copyTool = mcp.NewTool("valet_copy",
	mcp.WithDescription(`Copy a single secret from a personal or team store into this project's embedded store. The value is re-encrypted for the project's recipients — no secret values are exposed to the AI.

Use this after valet_wallet_search finds a key. Copying makes the project self-contained (the key is committed encrypted in .valet/). Use valet_link instead if you want the key to stay in the source store and auto-update on rotation.`),
	mcp.WithString("key", mcp.Required(), mcp.Description("Secret name to copy (e.g. OPENAI_API_KEY)")),
	mcp.WithString("from", mcp.Required(), mcp.Description("Source store name (e.g. my-keys)")),
	mcp.WithString("env", mcp.Description("Environment (default: dev)")),
)

func copyHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key := req.GetString("key", "")
	fromStore := req.GetString("from", "")
	env := req.GetString("env", "dev")

	if key == "" || fromStore == "" {
		return errResult("both 'key' and 'from' are required")
	}

	id, err := identity.Load()
	if err != nil {
		return errResult("no identity found — run 'valet identity init' first")
	}

	// Open source store and find the secret.
	source, err := store.FindStoreByName(fromStore, id)
	if err != nil {
		return errResult("source store %q not found", fromStore)
	}
	sourceProject, err := source.ResolveDefaultProject()
	if err != nil {
		return errResult("source store has no project: %v", err)
	}
	secret, scopePath, err := source.GetSecretFromEnv(sourceProject, env, key)
	if err != nil || secret == nil {
		return errResult("%s not found in %s (%s environment)", key, fromStore, env)
	}

	// Open target (embedded/primary) store and write.
	target, err := store.Resolve(id)
	if err != nil {
		return errResult("opening project store: %v", err)
	}
	targetProject, err := target.ResolveDefaultProject()
	if err != nil {
		return errResult("no project found: %v", err)
	}

	targetScope := env + "/default"
	if err := target.SetSecret(targetProject, targetScope, key, secret.Value); err != nil {
		return errResult("copying secret: %v", err)
	}

	return mcp.NewToolResultText(fmt.Sprintf("Copied %s from %s/%s into %s. The project now owns its own copy.\n\nRun commands with `valet run -- <command>` to inject it.", key, fromStore, scopePath, targetScope)), nil
}

// --- valet_require: declare a dependency ---

var requireTool = mcp.NewTool("valet_require",
	mcp.WithDescription(`Declare that this project needs a secret. Adds a requirement to .valet.toml without storing any values.

IMPORTANT: When you know the provider name, use provider-only mode (omit 'key'). This declares ALL env vars the provider needs in one call. Only use 'key' for non-provider secrets like DATABASE_URL or custom env vars.

Examples:
- provider="openai" → declares OPENAI_API_KEY
- provider="stripe" → declares STRIPE_SECRET_KEY, STRIPE_PUBLISHABLE_KEY, STRIPE_WEBHOOK_SECRET
- key="DATABASE_URL" description="Postgres connection string" → single custom key`),
	mcp.WithString("key", mcp.Description("Secret name for non-provider keys (e.g. DATABASE_URL). OMIT this when using provider — let the provider define the key names")),
	mcp.WithString("provider", mcp.Description("Provider name — declares ALL its env vars automatically. Preferred over specifying key manually")),
	mcp.WithString("description", mcp.Description("Human-readable description of what this secret is for")),
	mcp.WithBoolean("optional", mcp.Description("Whether this secret is optional (default: false)")),
)

func requireHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key := req.GetString("key", "")
	providerName := req.GetString("provider", "")
	description := req.GetString("description", "")
	optional := req.GetBool("optional", false)

	if key == "" && providerName == "" {
		return errResult("provide a 'key' or a 'provider' (or both)")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return errResult("cannot get working directory: %v", err)
	}

	tomlPath, err := config.FindValetToml(cwd)
	if err != nil {
		return errResult("no .valet.toml found — run 'valet init' first")
	}

	vc, err := config.LoadValetToml(tomlPath)
	if err != nil {
		return errResult("reading config: %v", err)
	}

	if vc.Requires == nil {
		vc.Requires = make(map[string]domain.Requirement)
	}

	// Provider-only mode: declare all env vars from the provider.
	if key == "" && providerName != "" {
		p := provider.Get(providerName)
		if p == nil {
			return errResult("unknown provider %q — run 'valet providers update' then 'valet providers list'", providerName)
		}
		for _, ev := range p.EnvVars {
			r := domain.Requirement{Provider: providerName, Optional: optional}
			mergeRequirement(vc.Requires, ev.Name, r)
		}
		if err := config.WriteValetToml(tomlPath, vc); err != nil {
			return errResult("writing config: %v", err)
		}
		var names []string
		for _, ev := range p.EnvVars {
			names = append(names, ev.Name)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Added %d requirements from %s: %s\n", len(p.EnvVars), p.DisplayName, strings.Join(names, ", "))
		fmt.Fprintf(&b, "Setup: %s\n", p.SetupURL)
		if p.FreeTier != "" {
			fmt.Fprintf(&b, "Free tier: %s\n", p.FreeTier)
		}
		fmt.Fprintf(&b, "\nNext steps:\n")
		fmt.Fprintf(&b, "1. Use valet_wallet_search for each key to check if the user already has them\n")
		fmt.Fprintf(&b, "2. For missing keys, ask the user to type: ! valet setup\n")
		fmt.Fprintf(&b, "\nRemember: run commands with `valet run -- <command>` to inject secrets at runtime.")
		return mcp.NewToolResultText(b.String()), nil
	}

	// Single key mode.
	r := domain.Requirement{
		Provider:    providerName,
		Description: description,
		Optional:    optional,
	}
	mergeRequirement(vc.Requires, key, r)

	if err := config.WriteValetToml(tomlPath, vc); err != nil {
		return errResult("writing config: %v", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Added requirement: %s", key)
	if providerName != "" {
		fmt.Fprintf(&b, " [%s]", providerName)
		if p := provider.Get(providerName); p != nil {
			fmt.Fprintf(&b, "\nSetup: %s", p.SetupURL)
			if p.FreeTier != "" {
				fmt.Fprintf(&b, "\nFree tier: %s", p.FreeTier)
			}
		}
	}
	fmt.Fprintf(&b, "\n\nNext steps:\n")
	fmt.Fprintf(&b, "1. Use valet_wallet_search to check if the user already has this key\n")
	fmt.Fprintf(&b, "2. If found, ask the user to type: ! valet setup\n")
	fmt.Fprintf(&b, "3. If not found, ask the user to type: ! valet secret set %s\n", key)
	fmt.Fprintf(&b, "\nRemember: run commands with `valet run -- <command>` to inject secrets at runtime.")
	return mcp.NewToolResultText(b.String()), nil
}

// mergeRequirement adds or updates a requirement, preserving existing fields.
func mergeRequirement(requires map[string]domain.Requirement, key string, req domain.Requirement) {
	if existing, ok := requires[key]; ok {
		if req.Provider == "" {
			req.Provider = existing.Provider
		}
		if req.Description == "" {
			req.Description = existing.Description
		}
		if !req.Optional {
			req.Optional = existing.Optional
		}
	}
	requires[key] = req
}

// --- valet_provider_search: discover providers ---

var providerSearchTool = mcp.NewTool("valet_provider_search",
	mcp.WithDescription(`Search the provider registry to discover API providers and their env var names. Call this when:
- You add an import or dependency that uses an external API (e.g. openai, stripe, @supabase/supabase-js)
- You need to find the right provider for a use case (e.g. "payments", "email", "vector database")
- You want to know what env vars a provider needs before calling valet_require
- You're looking for alternatives in a category (e.g. all AI providers)

70+ providers across AI, payments, cloud, databases, search, monitoring, communication, and more.
Returns provider names, descriptions, categories, env var names, and setup URLs. Never returns secret values.
After finding a provider, use valet_require with the provider name to declare all its env vars at once.`),
	mcp.WithString("query", mcp.Description("Search term: provider name, package name, category (ai, payments, cloud, search, etc.), use case, or env var name")),
	mcp.WithString("category", mcp.Description("Filter by category: ai, payments, cloud, search, communication, monitoring, database, cms, auth, social, devtools, storage, maps")),
)

func providerSearchHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query := req.GetString("query", "")
	category := req.GetString("category", "")

	var results []*provider.Provider

	if category != "" {
		results = provider.FindByCategory(category)
	} else if query != "" {
		results = provider.Search(query)
	} else {
		// No filter — return all providers.
		all := provider.Search("")
		if len(all) == 0 {
			return mcp.NewToolResultText("No providers loaded. Run 'valet providers update' in the terminal to fetch the registry."), nil
		}
		results = all
	}

	if len(results) == 0 {
		msg := "No providers found"
		if query != "" {
			msg += fmt.Sprintf(" matching %q", query)
		}
		if category != "" {
			msg += fmt.Sprintf(" in category %q", category)
		}
		msg += ".\n\nRun 'valet providers update' to fetch the latest registry, then try again."
		return mcp.NewToolResultText(msg), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d provider(s):\n\n", len(results))

	for _, p := range results {
		fmt.Fprintf(&b, "## %s", p.DisplayName)
		if p.Category != "" {
			fmt.Fprintf(&b, " [%s]", p.Category)
		}
		fmt.Fprintf(&b, "\n")
		if p.Description != "" {
			fmt.Fprintf(&b, "%s\n", p.Description)
		}
		fmt.Fprintf(&b, "Setup: %s\n", p.SetupURL)
		if p.FreeTier != "" {
			fmt.Fprintf(&b, "Free tier: %s\n", p.FreeTier)
		}
		fmt.Fprintf(&b, "Env vars:")
		for _, ev := range p.EnvVars {
			fmt.Fprintf(&b, " %s", ev.Name)
		}
		fmt.Fprintf(&b, "\n")
		fmt.Fprintf(&b, "Require: valet require --provider %s\n\n", p.Name)
	}

	return mcp.NewToolResultText(b.String()), nil
}

// --- valet_help: full CLI reference ---

var helpTool = mcp.NewTool("valet_help",
	mcp.WithDescription("Get the full Valet CLI reference. Use this to discover commands for operations not covered by the other tools (team management, exports, CI/CD setup, environments, scopes, etc). All commands are run via the terminal."),
	mcp.WithString("topic", mcp.Description("Optional topic: setup, secrets, running, environments, users, bots, stores, providers, ai, security, or leave empty for full reference")),
)

func helpHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	topic := req.GetString("topic", "")
	return mcp.NewToolResultText(helpText(topic)), nil
}

// --- Helpers ---

func errResult(format string, args ...any) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError(fmt.Sprintf(format, args...)), nil
}

func openAllProjectStores(id *identity.Identity) ([]store.LinkedStore, error) {
	primary, err := store.Resolve(id)
	if err != nil {
		return nil, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot get working directory: %v\n", err)
		return []store.LinkedStore{{Store: primary}}, nil
	}

	tomlPath, err := config.FindValetToml(cwd)
	if err != nil {
		return []store.LinkedStore{{Store: primary}}, nil
	}

	vc, err := config.LoadValetToml(tomlPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: reading .valet.toml: %v\n", err)
		return []store.LinkedStore{{Store: primary}}, nil
	}

	tomlDir := filepath.Dir(tomlPath)
	lc, _ := config.LoadLocalConfig(tomlDir)
	localStore := store.OpenLocalStore(tomlDir, id)

	return store.OpenLinkedStores(lc.Stores, vc.Stores, primary, localStore, id), nil
}
