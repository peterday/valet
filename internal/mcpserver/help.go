package mcpserver

// helpText returns the CLI reference, optionally filtered by topic.
func helpText(topic string) string {
	switch topic {
	case "setup":
		return helpSetup
	case "secrets":
		return helpSecrets
	case "running":
		return helpRunning
	case "environments":
		return helpEnvironments
	case "users":
		return helpUsers
	case "bots":
		return helpBots
	case "stores":
		return helpStores
	case "ai":
		return helpAI
	case "providers":
		return helpProviders
	case "security":
		return helpSecurity
	default:
		return helpFull
	}
}

const helpSetup = `Setup commands:

  valet identity init                                  # generate age keypair (one time)
  valet init                                           # embedded store in .valet/ (default)
  valet init --shared github:acme/secrets/api          # link team store + project
  valet init --local my-keys                           # link personal store
  valet init --shared github:acme/secrets --local my-keys  # both at once
  valet import .env                                    # import from .env file
  valet import .env -e prod --overwrite                # import into prod, overwrite existing
  valet require KEY [--provider X] [--optional]        # declare one requirement
  valet require --provider stripe                      # declare all keys from a provider
  valet setup                                          # interactive setup for missing secrets
  valet status                                         # show required vs available`

const helpSecrets = `Secret commands:

  valet secret set KEY [--value val] [--provider X]    # set (prompts if no --value)
  valet secret set KEY -e prod --value val             # set in specific environment
  valet secret set KEY -e dev,staging --value val      # set in multiple envs at once
  valet secret set KEY -e '*' --value val              # set in wildcard (all envs)
  valet secret set KEY --local                         # local override (.valet.local/)
  valet secret set KEY -s my-keys --value val          # set in specific store
  valet secret get KEY                                 # get value
  valet secret list                                    # list in current env
  valet secret history KEY                             # version history
  valet secret remove KEY --scope path                 # remove
  valet secret copy KEY --from <store>                 # copy one secret into this project
  valet secret sync --to <store>                       # copy all resolved secrets into a store

  valet resolve                                        # show all resolved secrets + sources
  valet resolve --show                                 # show actual values
  valet resolve KEY --show                             # single key, raw value (pipeable)
  valet resolve KEY --verbose                          # full resolution chain
  valet resolve --set KEY=VALUE                        # preview with command-line override

Important: valet secret set without --value prompts the user interactively.
Use this to let the user enter sensitive values without exposing them.

Local overrides (--local):
  Stores in .valet.local/ (gitignored). Use for personal dev values that
  differ from the team (local DB, your own API key). Highest priority.

Wildcard environment (-e '*'):
  Applies to all environments unless a specific env overrides it.

Link vs Copy:
  Link (valet link <store>):
    - Project references the store; secrets resolve at runtime
    - Key stays in one place; updates propagate automatically
    - Links the entire store (all its keys become available)
    - Personal links are gitignored (.valet.local.toml)
    - Best for: personal dev keys, shared team keys

  Copy (valet secret copy KEY --from <store>):
    - Copies the value into the project's embedded store
    - Project is self-contained; no external dependency
    - Must manually re-copy if the source key is rotated
    - Committed (encrypted); teammates get it via git pull
    - Best for: project-specific keys, CI/CD, sharing with team`

const helpRunning = `Running and exporting:

  valet drive -- <command>                             # inject secrets and run (alias: valet run)
  valet drive -e prod -- <command>                     # specific environment
  valet drive --set KEY=VALUE -- <command>             # override a secret for this run
  valet sync .env                                      # dotenv file
  valet sync --format json                             # JSON
  valet sync --format shell                            # export KEY=val
  valet sync --format docker                           # --env KEY=val flags
  valet sync --format k8s-secret                       # Kubernetes Secret YAML
  valet sync --format github-actions                   # KEY=val for $GITHUB_ENV
  valet sync --format compose                          # docker-compose environment: block
  valet sync --format compose-override --service api   # docker-compose.override.yml`

const helpEnvironments = `Environment and scope commands:

  valet env create <name>                              # create environment
  valet env list                                       # list environments
  valet env grant <user> -e <env>                      # grant access
  valet env revoke <user> -e <env> [--rotate]          # revoke access
  valet scope create <env/path>                        # create scope (advanced)
  valet scope list                                     # list scopes
  valet scope add-recipient <user> --scope <path>      # fine-grained access
  valet scope remove-recipient <user> --scope <path>   # fine-grained revoke

Every project has environments (dev, staging, prod). Each holds its own secrets.
Scopes are for fine-grained access within an environment — most projects just
use the default scope and never think about scopes.`

const helpUsers = `User and invite commands:

  valet user add <name> --github <handle>              # add by GitHub SSH key
  valet user add <name> --key <pubkey>                 # add by age public key
  valet user list                                      # list users
  valet invite create -e <env>                         # create invite (prints temp key)
  valet invite create -e dev -e staging --expires 3d   # multi-env, custom expiry
  valet invite create -e dev --max-uses 5              # multi-use invite
  valet invite list                                    # list pending invites
  valet invite prune                                   # remove expired invites
  valet join <remote>                                  # clone shared store
  valet join <remote> --as <local-name>                # clone with custom local name
  valet join --invite AGE-SECRET-KEY-...               # join with invite
  valet push                                           # push to remote
  valet pull                                           # pull from remote

Adding teammates by GitHub handle is the easiest — it fetches their SSH key
automatically and invites them as a collaborator on team store repos.`

const helpBots = `Bot and CI commands:

  valet bot create <name> --grant <env>                # create bot, prints VALET_KEY
  valet bot list                                       # list bots
  valet bot revoke <name> [--rotate]                   # revoke + remove bot

For GitHub Actions:
  - uses: actions/checkout@v4
  - run: curl -fsSL https://raw.githubusercontent.com/peterday/valet/main/install.sh | sh
  - run: valet drive -- npm test
    env:
      VALET_KEY: ${{ secrets.VALET_KEY }}

For other platforms: set VALET_KEY as an env var, then valet drive -- <command>.`

const helpStores = `Store commands:

  valet store create my-secrets                        # personal local store
  valet store create github:acme/secrets               # team git-backed store
  valet store list                                     # list stores
  valet store delete my-secrets                        # delete a local store
  valet link <store-name>                              # link store (personal, gitignored)
  valet link <store-name> --shared                     # link store (shared, committed)

Store types:
  Local — lives on this machine only (~/.valet/stores/).
  Git-backed — lives in a git repo, can be shared.
  Embedded — .valet/ inside the project. Encrypted values safe to commit.

Stores layer: personal → shared → embedded. Embedded wins on conflict.

Store linking in .valet.toml:

  # Simplest — all keys, environments match by name:
  [[stores]]
  name = "team-backend"
  url = "git@github.com:acme/secrets-backend.git"

  # Filter to specific keys:
  [[stores]]
  name = "team-infra"
  url = "git@github.com:acme/secrets-infra.git"
  keys = ["CACHE_URL", {local = "DATABASE_URL", remote = "POSTGRES_PRIMARY_URL"}]

  # Map environment names:
  [[stores]]
  name = "team-backend"
  url = "git@github.com:acme/secrets-backend.git"
  environments = [{local = "dev", remote = "staging"}]

Resolution order for a key + environment:
  1. Embedded store (.valet/) — local overrides always win
  2. Linked stores in declaration order
  3. Conflict if multiple stores provide the same key → error

Link types when setting up a missing secret:
  Copy         — snapshot value into this project's embedded store
  Link (me)    — personal link in .valet.local.toml (gitignored)
  Link (project) — shared link in .valet.toml (only for git-backed stores)`

const helpProviders = `Provider registry:

Valet has a built-in registry of common API providers (OpenAI, Anthropic,
Stripe, Supabase, Fly.io, Exa, ElevenLabs). For each provider, Valet knows:
  - Setup URL (shortest path to getting a key)
  - Environment variable names
  - Key prefix for format validation
  - Validation endpoint
  - Free tier details
  - Rotation characteristics

When a required secret matches a known provider (by env var name or explicit
provider flag), valet setup opens the right browser page, validates the key
format, and confirms it works before storing.

Providers are matched by env var name lookup (not pattern inference):
  OPENAI_API_KEY → openai
  STRIPE_SECRET_KEY → stripe
  SUPABASE_URL → supabase

Unknown env var names fall back to a plain prompt. You can always set a
provider explicitly: valet require MY_AI_KEY --provider openai

Key rotation varies by provider:
  OpenAI    — create-then-revoke (programmatic via admin API)
  Anthropic — create-then-revoke (manual, console only)
  Stripe    — rolling rotation (old key has 24h grace period)
  Supabase  — nuclear (changing JWT secret invalidates ALL keys)
  Fly.io    — create-then-revoke (programmatic via CLI)
  Exa       — manual
  ElevenLabs — manual`

const helpSecurity = `Security model:

  - age (https://age-encryption.org/) public key encryption per scope
  - Secret names visible in manifest; values only in encrypted vault.age
  - Identity keys at ~/.valet/identity/ — never committed
  - GitHub SSH keys as age recipients — no key ceremony
  - --rotate on revoke flags secrets for rotation
  - Version history — up to 10 previous versions per secret
  - VALET_KEY env var for bots — no key files in CI`

const helpAI = `AI tool integration:

  valet mcp install                                    # register with Claude Code, Cursor, etc.
  valet mcp install --claude-code                      # Claude Code only
  valet mcp install --cursor                           # Cursor only

MCP tools:
  valet_init               — initialize valet, generate CLAUDE.md snippet
  valet_scan               — scan for .env files, match keys against providers/wallet
  valet_status             — project config, environments, secrets, requirements
  valet_wallet_search      — check if user already has a key
  valet_link               — link a store (all its keys become available)
  valet_copy               — copy a single key from a store into the project
  valet_require            — declare secret dependencies (single key or entire provider)
  valet_provider_search    — discover providers by name, category, or use case
  valet_help               — full CLI reference

Workflow for existing projects with .env files:
  1. valet_init to set up the project + write CLAUDE.md
  2. valet_scan to detect .env files and match keys against wallet/providers
  3. Link wallet for keys already there, ! valet import .env for the rest
  4. valet_require for each key to declare requirements

Workflow for new projects:
  1. valet_init to set up the project + write CLAUDE.md
  2. valet_provider_search to discover what keys are needed
  3. valet_require --provider <name> to declare all env vars
  4. valet_wallet_search to check if user has them
  5. If found: ask user link or copy, then valet_link or valet_copy
  6. If not found, ask user to type: ! valet setup

Important: never use --value flag or valet secret get — keep secrets out of AI context.
Run commands with: valet run -- <command>`

const helpFull = helpSetup + "\n\n" + helpSecrets + "\n\n" + helpRunning + "\n\n" + helpEnvironments + "\n\n" + helpUsers + "\n\n" + helpBots + "\n\n" + helpStores + "\n\n" + helpProviders + "\n\n" + helpAI + "\n\n" + helpSecurity
