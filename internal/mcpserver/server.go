package mcpserver

import (
	"context"
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

	s.AddTool(statusTool, statusHandler)
	s.AddTool(walletSearchTool, walletSearchHandler)
	s.AddTool(requireTool, requireHandler)
	s.AddTool(helpTool, helpHandler)

	return server.ServeStdio(s)
}

// --- valet_status: the "tell me everything" tool ---

var statusTool = mcp.NewTool("valet_status",
	mcp.WithDescription(`Show complete project status: configuration, environments, secrets, requirements, and team members. This is the primary discovery tool — call it first to understand a project's secrets setup. Never returns secret values.

If no .valet.toml exists, the project hasn't been initialized. Run 'valet init' in the terminal to set up secrets management.`),
	mcp.WithString("env", mcp.Description("Environment to check (default: dev)")),
)

func statusHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	env := stringArg(req, "env", "dev")

	cwd, err := os.Getwd()
	if err != nil {
		return errResult("cannot get working directory: %v", err)
	}

	tomlPath, err := config.FindValetToml(cwd)
	if err != nil {
		return mcp.NewToolResultText("No .valet.toml found — this project hasn't been initialized with Valet.\n\nRun 'valet init' in the terminal to set up encrypted secrets management.\nThen use valet_require to declare what secrets the project needs."), nil
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
			fmt.Fprintf(&b, "Then run 'valet setup' in the terminal to configure interactively.")
		}
	} else {
		fmt.Fprintf(&b, "\nNo requirements declared. Use valet_require to declare what secrets this project needs.")
	}

	return mcp.NewToolResultText(b.String()), nil
}

// --- valet_wallet_search: check personal stores ---

var walletSearchTool = mcp.NewTool("valet_wallet_search",
	mcp.WithDescription("Search the user's personal stores (wallet) for a secret by name. Use this to check if the user already has an API key before asking them to enter it. Never returns secret values — only reports which stores have the key."),
	mcp.WithString("key", mcp.Required(), mcp.Description("Secret name to search for (e.g. OPENAI_API_KEY)")),
	mcp.WithString("env", mcp.Description("Environment to search (default: dev)")),
)

func walletSearchHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key := req.Params.Arguments["key"].(string)
	env := stringArg(req, "env", "dev")

	id, err := identity.Load()
	if err != nil {
		return errResult("no identity found — run 'valet identity init' first")
	}

	matches, err := store.SearchStoresForSecret(key, env, id)
	if err != nil {
		return errResult("searching stores: %v", err)
	}

	if len(matches) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("%s not found in any personal store.\n\nRun 'valet secret set %s' in the terminal to prompt the user for the value.", key, key)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s found in %d store(s):\n", key, len(matches))
	for _, m := range matches {
		fmt.Fprintf(&b, "  • %s (scope: %s)\n", m.StoreName, m.ScopePath)
	}
	if len(matches) == 1 {
		m := matches[0]
		fmt.Fprintf(&b, "\nOptions:\n")
		fmt.Fprintf(&b, "  • Run 'valet setup' in the terminal — searches wallet, lets user choose\n")
		fmt.Fprintf(&b, "  • Run 'valet link %s' to link the entire store (all its keys become available)\n", m.StoreName)
		fmt.Fprintf(&b, "  • Run 'valet secret copy %s --from %s' to copy just this key (project-owned copy)\n", key, m.StoreName)
		fmt.Fprintf(&b, "\nLink keeps the key in the source store (auto-updates on rotation).\nCopy makes the project self-contained (must re-copy on rotation).")
	} else {
		fmt.Fprintf(&b, "\nFound in multiple stores. Run 'valet setup' in the terminal — it will show the options and let the user choose.\n")
		fmt.Fprintf(&b, "Or copy a specific one: 'valet secret copy %s --from <store-name>'", key)
	}
	return mcp.NewToolResultText(b.String()), nil
}

// --- valet_require: declare a dependency ---

var requireTool = mcp.NewTool("valet_require",
	mcp.WithDescription("Declare that this project needs a secret. Adds a requirement to .valet.toml without storing any values. Use this when writing code that depends on an API key or secret."),
	mcp.WithString("key", mcp.Required(), mcp.Description("Secret name (e.g. OPENAI_API_KEY, DATABASE_URL)")),
	mcp.WithString("provider", mcp.Description("Provider name (e.g. openai, stripe, aws)")),
	mcp.WithString("description", mcp.Description("Human-readable description of what this secret is for")),
	mcp.WithBoolean("optional", mcp.Description("Whether this secret is optional (default: false)")),
)

func requireHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key := req.Params.Arguments["key"].(string)
	provider := stringArg(req, "provider", "")
	description := stringArg(req, "description", "")
	optional := boolArg(req, "optional", false)

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

	r := domain.Requirement{
		Provider:    provider,
		Description: description,
		Optional:    optional,
	}

	// Merge with existing.
	if existing, ok := vc.Requires[key]; ok {
		if r.Provider == "" {
			r.Provider = existing.Provider
		}
		if r.Description == "" {
			r.Description = existing.Description
		}
		if !r.Optional {
			r.Optional = existing.Optional
		}
	}

	vc.Requires[key] = r

	if err := config.WriteValetToml(tomlPath, vc); err != nil {
		return errResult("writing config: %v", err)
	}

	msg := fmt.Sprintf("Added requirement: %s", key)
	if provider != "" {
		msg += fmt.Sprintf(" [%s]", provider)
	}
	msg += "\n\nNext steps:\n"
	msg += "1. Use valet_wallet_search to check if the user already has this key\n"
	msg += "2. If found: run 'valet setup' in the terminal to let the user choose and link it\n"
	msg += "3. If not found: run 'valet secret set " + key + "' in the terminal to prompt for the value"
	return mcp.NewToolResultText(msg), nil
}

// --- valet_help: full CLI reference ---

var helpTool = mcp.NewTool("valet_help",
	mcp.WithDescription("Get the full Valet CLI reference. Use this to discover commands for operations not covered by the other tools (team management, exports, CI/CD setup, environments, scopes, etc). All commands are run via the terminal."),
	mcp.WithString("topic", mcp.Description("Optional topic: setup, secrets, running, environments, users, bots, stores, providers, ai, security, or leave empty for full reference")),
)

func helpHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	topic := stringArg(req, "topic", "")
	return mcp.NewToolResultText(helpText(topic)), nil
}

// --- Helpers ---

func stringArg(req mcp.CallToolRequest, name, defaultVal string) string {
	if v, ok := req.Params.Arguments[name].(string); ok && v != "" {
		return v
	}
	return defaultVal
}

func boolArg(req mcp.CallToolRequest, name string, defaultVal bool) bool {
	if v, ok := req.Params.Arguments[name].(bool); ok {
		return v
	}
	return defaultVal
}

func errResult(format string, args ...any) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError(fmt.Sprintf(format, args...)), nil
}

func openAllProjectStores(id *identity.Identity) ([]*store.Store, error) {
	primary, err := store.Resolve(id)
	if err != nil {
		return nil, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return []*store.Store{primary}, nil
	}

	tomlPath, err := config.FindValetToml(cwd)
	if err != nil {
		return []*store.Store{primary}, nil
	}

	vc, err := config.LoadValetToml(tomlPath)
	if err != nil {
		return []*store.Store{primary}, nil
	}

	tomlDir := filepath.Dir(tomlPath)
	lc, _ := config.LoadLocalConfig(tomlDir)
	localStore := store.OpenLocalStore(tomlDir, id)

	return store.OpenLinkedStores(lc.Stores, vc.Stores, primary, localStore, id), nil
}
