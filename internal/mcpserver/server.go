package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	mcp.WithDescription(`Initialize Valet in the current project. Creates an encrypted secret store and returns a CLAUDE.md snippet to add to the project.

Call this when:
- Setting up a new project that will need API keys or secrets
- The user asks to add secrets management
- valet_status reports no .valet.toml

After init, use valet_provider_search to discover what API keys the project needs, then valet_require to declare them.`),
)

func initHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return errResult("cannot get working directory: %v", err)
	}

	// Check if already initialized.
	if _, err := config.FindValetToml(cwd); err == nil {
		return mcp.NewToolResultText("Project already initialized — .valet.toml exists.\n\nUse valet_status to see current state, or valet_require to declare secrets."), nil
	}

	// Run valet init via the CLI.
	valetPath, err := os.Executable()
	if err != nil {
		valetPath = "valet"
	}

	cmd := exec.Command(valetPath, "init")
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errResult("valet init failed: %s\n%s", err, string(output))
	}

	// Generate CLAUDE.md snippet with project-aware run command.
	snippet := generateClaudeMDSnippet(cwd)

	var b strings.Builder
	fmt.Fprintf(&b, "Valet initialized.\n\n%s", string(output))
	fmt.Fprintf(&b, "\n---\n\n")
	fmt.Fprintf(&b, "Add this to the project's CLAUDE.md (create if it doesn't exist):\n\n")
	fmt.Fprintf(&b, "```markdown\n%s```\n\n", snippet)
	fmt.Fprintf(&b, "Next steps:\n")
	fmt.Fprintf(&b, "1. Write the CLAUDE.md snippet above to the project\n")
	fmt.Fprintf(&b, "2. Call valet_scan to detect existing .env files and match against wallet/providers\n")
	fmt.Fprintf(&b, "3. Based on scan results, link wallet and/or import .env\n")
	fmt.Fprintf(&b, "4. Call valet_require for each key to declare requirements\n")
	fmt.Fprintf(&b, "5. For missing keys, ask the user to type: ! valet setup")

	return mcp.NewToolResultText(b.String()), nil
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

	// Find .env files.
	envFiles := []string{
		".env", ".env.local", ".env.development", ".env.staging",
		".env.production", ".env.test", ".env.dev", ".env.prod",
	}

	type envFileInfo struct {
		Name string
		Keys []string
	}
	var found []envFileInfo
	allKeys := make(map[string]string) // key → which file it came from

	for _, name := range envFiles {
		path := filepath.Join(cwd, name)
		keys, err := scanEnvFileKeys(path)
		if err != nil || len(keys) == 0 {
			continue
		}
		found = append(found, envFileInfo{Name: name, Keys: keys})
		for _, k := range keys {
			if _, exists := allKeys[k]; !exists {
				allKeys[k] = name
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

	// Match against providers.
	fmt.Fprintf(&b, "\nProvider matches:\n")
	providerMatches := 0
	for key := range allKeys {
		p := provider.FindByEnvVar(key)
		if p != nil {
			fmt.Fprintf(&b, "  %-28s → %s\n", key, p.DisplayName)
			providerMatches++
		}
	}
	if providerMatches == 0 {
		fmt.Fprintf(&b, "  (none matched known providers)\n")
	}

	// Search wallet for each key.
	id, err := identity.Load()
	walletMatches := make(map[string][]string) // key → store names
	if err == nil {
		for key := range allKeys {
			matches, err := store.SearchStoresForSecret(key, "dev", id)
			if err != nil || len(matches) == 0 {
				continue
			}
			for _, m := range matches {
				walletMatches[key] = append(walletMatches[key], m.StoreName)
			}
		}
	}

	if len(walletMatches) > 0 {
		fmt.Fprintf(&b, "\nAlready in your wallet:\n")
		storesUsed := make(map[string]bool)
		for key, stores := range walletMatches {
			fmt.Fprintf(&b, "  %-28s in %s\n", key, strings.Join(stores, ", "))
			for _, s := range stores {
				storesUsed[s] = true
			}
		}

		// Recommend linking.
		var storeNames []string
		for s := range storesUsed {
			storeNames = append(storeNames, s)
		}
		fmt.Fprintf(&b, "\nRecommendation:\n")
		fmt.Fprintf(&b, "1. Ask the user: should I link your wallet (%s) or import everything from .env?\n", strings.Join(storeNames, ", "))
		fmt.Fprintf(&b, "   - Link: call valet_link for each store — keys auto-update on rotation\n")
		fmt.Fprintf(&b, "   - Import: ask user to type ! valet import .env — project owns copies\n")
		fmt.Fprintf(&b, "   - Both: link wallet for keys already there, import .env for the rest\n")
	} else {
		fmt.Fprintf(&b, "\nNo matching keys found in wallet.\n\n")
		fmt.Fprintf(&b, "Recommendation:\n")
		fmt.Fprintf(&b, "1. Ask user to type: ! valet import .env\n")
	}

	fmt.Fprintf(&b, "2. Call valet_require for each key to declare requirements\n")
	fmt.Fprintf(&b, "3. After import, the .env file can be deleted (secrets are now encrypted in .valet/)")

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

	if len(matches) == 0 {
		// Check provider registry for setup guidance.
		msg := fmt.Sprintf("%s not found in any personal store.\n\n", key)
		p := provider.FindByEnvVar(key)
		if p != nil {
			msg += fmt.Sprintf("This is a %s key. ", p.DisplayName)
			if p.FreeTier != "" {
				msg += fmt.Sprintf("Free tier: %s. ", p.FreeTier)
			}
			msg += fmt.Sprintf("Get one at: %s\n\n", p.SetupURL)
		}
		msg += fmt.Sprintf("Ask the user to type: ! valet secret set %s\n", key)
		msg += "Do NOT pass the secret via --value or through your context — the ! prefix runs it interactively."
		return mcp.NewToolResultText(msg), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s found in %d store(s):\n", key, len(matches))
	for _, m := range matches {
		fmt.Fprintf(&b, "  • %s (scope: %s)\n", m.StoreName, m.ScopePath)
	}
	if len(matches) == 1 {
		m := matches[0]
		fmt.Fprintf(&b, "\nAsk the user: should I link the store %q (all its keys become available, auto-updates) or copy just %s (project-owned copy)?\n\n", m.StoreName, key)
		fmt.Fprintf(&b, "Based on their choice:\n")
		fmt.Fprintf(&b, "  Link: call valet_link with store=%q\n", m.StoreName)
		fmt.Fprintf(&b, "  Copy: call valet_copy with key=%q from=%q\n", key, m.StoreName)
	} else {
		fmt.Fprintf(&b, "\nAsk the user which store to use and whether to link or copy:\n")
		for _, m := range matches {
			fmt.Fprintf(&b, "  Link %s: call valet_link with store=%q\n", m.StoreName, m.StoreName)
			fmt.Fprintf(&b, "  Copy from %s: call valet_copy with key=%q from=%q\n", m.StoreName, key, m.StoreName)
		}
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
	mcp.WithDescription(`Declare that this project needs a secret. Adds a requirement to .valet.toml without storing any values. Call this when:
- You write code that imports an API client (openai, stripe, supabase, etc.)
- The project needs a new environment variable for an external service
- You want to declare all keys from a provider at once (omit 'key', just provide 'provider')

Use valet_provider_search first if you're not sure which provider or env var name to use.`),
	mcp.WithString("key", mcp.Description("Secret name (e.g. OPENAI_API_KEY). Omit to declare all keys from the provider")),
	mcp.WithString("provider", mcp.Description("Provider name (e.g. openai, stripe, supabase). Use valet_provider_search to discover providers")),
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
