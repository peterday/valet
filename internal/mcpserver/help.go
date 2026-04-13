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

Core primitives: projects have requirements, stores have secrets.

  valet identity init                                  # generate age keypair (one time)
  valet init                                           # embedded store (auto-adopts .env.example)
  valet init --shared github:acme/secrets/api          # link team store + project
  valet init --local my-keys                           # link personal store
  valet adopt                                          # bootstrap from .env.example
  valet adopt --personal my-keys                       # personal-only (zero repo changes)
  valet migrate                                        # deduplicate .valet.toml vs .env.example
  valet import .env                                    # import from .env file
  valet require KEY [--provider X] [--optional]        # requirement override (shared)
  valet require KEY --personal                         # requirement override (gitignored)
  valet require --provider stripe                      # declare all keys from a provider
  valet setup                                          # interactive setup for missing secrets
  valet status                                         # show required vs available
  valet ui                                             # web dashboard

Requirements model:
  .env.example            → auto-detected baseline (source of truth)
  .valet.toml [requires]  → shared overrides (committed)
  .valet.local.toml [requires] → personal overrides (gitignored)
  Requirements merge at runtime. Add to .env.example to declare new secrets.`

const helpSecrets = `Secret commands:

  valet secret set KEY [--value val] [--provider X]    # set (prompts if no --value)
  valet secret set KEY -e prod --value val             # set in specific environment
  valet secret set KEY -e '*' --value val              # set in wildcard (all envs)
  valet secret set KEY --local                         # local override (.valet.local/)
  valet secret set KEY -s my-keys --value val          # set in specific store
  valet secret get KEY                                 # get value
  valet secret list                                    # list in current env
  valet secret history KEY                             # version history
  valet secret remove KEY --scope path                 # remove
  valet secret copy KEY --from <store>                 # copy one secret into this project

  valet resolve                                        # show all resolved secrets + sources
  valet resolve --show                                 # show actual values
  valet resolve KEY --verbose                          # full resolution chain

Important: valet secret set without --value prompts the user interactively.
Use this to keep secrets out of shell history and AI context.

Link vs Copy:
  Link (valet link <store>):
    - Project references the store; secrets resolve at runtime
    - Updates propagate automatically when source changes
    - Personal links are gitignored (.valet.local.toml)
    - Best for: personal dev keys, shared team keys

  Copy (valet secret copy KEY --from <store>):
    - Copies the value into the project's embedded store
    - Project is self-contained; teammates get it via git pull
    - Must re-copy if the source key is rotated
    - Best for: project-specific keys, CI/CD`

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

Every project has environments (dev, staging, prod). Each holds its own secrets.
Scopes are for fine-grained access within an environment — most projects just
use the default scope.`

const helpUsers = `User and invite commands:

  valet user add <name> --github <handle>              # add by GitHub (fetches ALL SSH keys)
  valet user add <name> --key <pubkey>                 # add by age public key
  valet user refresh <name>                            # sync SSH keys from GitHub
  valet user add-key <name> --key <pubkey>             # add additional key to user
  valet user revoke-key <name> <key-prefix>            # revoke a specific key
  valet user update <name> --github <handle>           # link GitHub to existing user
  valet user list                                      # list users
  valet invite create -e <env>                         # create invite (prints temp key)
  valet invite create -e dev -e staging --expires 3d   # multi-env, custom expiry
  valet invite create -e dev --max-uses 5              # multi-use invite
  valet invite list                                    # list pending invites
  valet join <remote>                                  # clone shared store
  valet join --invite AGE-SECRET-KEY-...               # join with invite
  valet push                                           # push to remote
  valet pull                                           # pull from remote

Multi-key support: users can have multiple SSH keys (work + personal laptop).
GitHub add fetches all SSH keys. Refresh syncs new keys automatically.
Revoked keys require explicit valet user revoke-key.`

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
  Embedded — .valet/ inside the project. Encrypted values safe to commit.
  Personal — ~/.valet/stores/. Your keys across all projects.
  Team — git-backed store, shared via GitHub. Multi-repo secrets.

Stores layer: personal → shared → embedded. Embedded wins on conflict.

Store linking in .valet.toml:

  # Simplest — all keys, environments match by name:
  [[stores]]
  name = "team-backend"
  url = "git@github.com:acme/secrets-backend.git"

  # Filter to specific keys:
  [[stores]]
  name = "team-infra"
  keys = ["CACHE_URL", {local = "DATABASE_URL", remote = "POSTGRES_PRIMARY_URL"}]

  # Map environment names:
  [[stores]]
  name = "team-backend"
  environments = [{local = "dev", remote = "staging"}]`

const helpProviders = `Provider registry:

70+ providers with setup URLs, env var names, key format prefixes,
validation endpoints, free tier details, and rotation strategies.

When a required secret matches a known provider (by env var name or explicit
provider flag), valet provides setup links, validates format, and shows
rotation guidance.

Providers are matched by env var name:
  OPENAI_API_KEY → openai
  STRIPE_SECRET_KEY → stripe
  SUPABASE_URL → supabase

Set a provider explicitly: valet require MY_AI_KEY --provider openai

Key rotation varies by provider:
  OpenAI    — create-then-revoke (programmatic via admin API)
  Anthropic — create-then-revoke (manual, console only)
  Stripe    — rolling rotation (old key has 24h grace period)
  Supabase  — nuclear (changing JWT secret invalidates ALL keys)

Rotation flags auto-clear when a secret value is updated.`

const helpSecurity = `Security model:

  - age (https://age-encryption.org/) public key encryption per scope
  - SSH keys (ed25519, RSA) supported as recipients via agessh
  - Secret names visible in manifest; values only in encrypted vault.age
  - Identity keys at ~/.valet/identity/ — never committed
  - GitHub SSH keys as recipients — no key ceremony needed
  - Multi-key users: multiple SSH keys per person (work + personal laptop)
  - --rotate on revoke flags secrets for rotation
  - Version history — up to 10 previous versions per secret
  - VALET_KEY env var for bots — no key files in CI
  - Key source tracking (github/age-identity/manual) for audit`

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
  valet_setup_web          — open browser for entering values (secrets stay out of AI context)
  valet_help               — full CLI reference

Workflow for existing projects with .env.example:
  1. valet_init — auto-adopts .env.example, sets up store
  2. valet_status — shows what's configured vs missing
  3. valet_wallet_search — checks if user already has keys
  4. If found: valet_link or valet_copy. If not: valet_setup_web

Workflow for new projects:
  1. valet_init to set up the project + write CLAUDE.md
  2. valet_provider_search to discover what keys are needed
  3. valet_require --provider <name> to declare all env vars
  4. valet_wallet_search to check if user has them
  5. If not found: valet_setup_web for browser-based entry

Important: never use --value flag or valet secret get — keep secrets out of AI context.`

const helpFull = helpSetup + "\n\n" + helpSecrets + "\n\n" + helpRunning + "\n\n" + helpEnvironments + "\n\n" + helpUsers + "\n\n" + helpBots + "\n\n" + helpStores + "\n\n" + helpProviders + "\n\n" + helpAI + "\n\n" + helpSecurity
