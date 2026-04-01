# Valet

Encrypted secrets management for developers and teams. Store locally, share through git, inject at runtime.

```bash
curl -fsSL https://raw.githubusercontent.com/peterday/valet/main/install.sh | sh
```

## 30-Second Start

```bash
valet identity init                                    # one time
cd ~/code/my-api
valet init                                             # creates encrypted store
valet secret set OPENAI_API_KEY --value sk-abc123
valet secret set DATABASE_URL --value postgres://localhost/mydb
valet drive -- uvicorn main:app --reload                # run with secrets injected
```

Secrets are encrypted in `.valet/`, injected at runtime by `valet drive`, and `.valet.toml` is safe to commit. Need a `.env` file? `valet sync .env`.

## Stores

A store is where your encrypted secrets live. Three types — start simple, scale up.

### Embedded

Secrets live in `.valet/` inside your project. Encrypted values are safe to commit. Best for solo devs and small teams.

```bash
cd ~/code/my-api
valet init
valet secret set OPENAI_API_KEY --value sk-abc123
valet drive -- npm start
```

To share with a teammate, add them and push:

```bash
valet user add bob --github bob-smith
valet env grant bob -e dev
git add . && git commit -m "grant bob" && git push
# Bob: git pull && valet drive -- npm start
```

### Personal

A standalone store for keys you reuse across projects. Your OpenAI key, your AWS credentials — enter once, use everywhere.

```bash
valet store create my-keys
valet secret set OPENAI_API_KEY --value sk-abc -s my-keys
valet secret set ANTHROPIC_API_KEY --value sk-ant-xyz -s my-keys
```

Link to any project:

```bash
cd ~/code/my-api
valet link my-keys
valet drive -- npm start                           # OPENAI_API_KEY injected from my-keys
```

Back up to a private GitHub repo and sync across machines:

```bash
valet store create github:pday/my-keys             # creates repo + pushes
# On another machine:
valet join github:pday/my-keys                     # clones your personal store
```

### Team

A standalone store backed by a git repo. The whole team shares it. Best for secrets used across multiple repos — monitoring keys, shared databases, third-party services.

```bash
# Alice creates the store and adds secrets
valet store create github:acme/api-secrets
valet secret set DATADOG_API_KEY --value dd-key -s api-secrets

# Alice adds Bob (fetches SSH key, invites to GitHub repo)
valet user add bob --github bob-smith -s api-secrets
valet env grant bob -e dev -s api-secrets
valet push -s api-secrets

# Bob joins and links to his project
valet join github:acme/api-secrets
cd ~/code/my-project && valet link api-secrets
valet drive -- npm start
```

See [Sharing with a Team](#sharing-with-a-team) for the full walkthrough.

## Environments

Every project has environments — `dev`, `staging`, `prod`. Each holds its own secrets. `valet init` creates `dev` automatically.

```bash
valet secret set DATABASE_URL --value postgres://localhost/mydb
valet drive -- npm start                               # uses dev

valet env create staging
valet env create prod
valet secret set DATABASE_URL -e staging --value postgres://staging/mydb
valet secret set DATABASE_URL -e prod --value postgres://prod/mydb

valet drive -e staging -- npm start
valet sync .env -e prod
```

Secrets are isolated per environment. Grant a teammate `dev` without exposing `prod`.

## Local Overrides

Like `.env.local` in Next.js — local developer overrides that are never committed.

```bash
valet secret set CACHE_URL --local                     # set for dev (default)
valet secret set DATABASE_URL --local -e '*'           # set for all environments
valet secret set API_KEY --local -e dev,staging         # set for specific envs
```

Local overrides live in `.valet.local/` (gitignored). They take highest priority in resolution — above the embedded store, above linked stores.

```bash
valet resolve
  DATABASE_URL        postg...b/dev     .valet.local/*         ← local wildcard
  OPENAI_API_KEY      sk-pr...xyz       .valet.local/dev       ← local override
  CACHE_URL           redis...379       team-backend/dev       ← from team store
  DATADOG_API_KEY     dd-ab...123       .valet/dev             ← from project
```

### Resolution order

For a given key + environment (highest priority first):

```
1. --set overrides          (command line, ephemeral)
2. .valet.local/{env}       (local developer overrides)
3. .valet.local/*           (local wildcard)
4. .valet/{env}             (shared project values)
5. .valet/*                 (shared project wildcard)
6. linked stores/{env}      (team/personal)
7. linked stores/*          (team/personal wildcard)
```

### Wildcard environment

Set a value once, applies to all environments:

```bash
valet secret set DATADOG_API_KEY -e '*'                # same key everywhere
valet secret set DATABASE_URL -e dev                   # except dev uses this
```

### Inspecting resolution

```bash
valet resolve                                          # all secrets, masked
valet resolve --show                                   # all secrets, values shown
valet resolve DATABASE_URL --show                      # single key (pipeable)
valet resolve DATABASE_URL --verbose                   # full provenance chain
```

## Using Personal Keys in a Project

You have API keys in a personal store. Your project needs them. Two ways to connect them:

### Link your personal store (keys stay personal)

```bash
cd ~/code/my-api
valet init --local my-keys
```

Now `valet drive` and `valet sync` pull from both your personal store and the project's embedded store. Nothing is copied — your keys stay in `my-keys`, and the link is in `.valet.local.toml` (gitignored, so teammates don't see it).

### Copy a single key into the project store

```bash
valet secret copy STRIPE_KEY --from my-keys
```

Copies one secret into the embedded store. The project owns its own copy — self-contained, shareable with the team via git. Use this for project-specific keys.

### Copy all keys into the project store (self-contained)

```bash
valet secret sync --to .
```

Copies all resolved secrets into the embedded store. The project becomes self-contained — no personal store link needed.

### When to link vs copy

**Link** — key stays in the source store, resolves at runtime. If you rotate the key in your personal store, every linked project gets the update. Best for personal dev keys you reuse everywhere (OpenAI, Anthropic, etc.).

**Copy** — value is re-encrypted into the project's store. Self-contained, shareable with teammates via git. If the source key rotates, you must re-copy. Best for project-specific keys (this app's Stripe account, database URLs).

### Copy keys into a team store (share with team)

```bash
valet secret sync --to api-secrets
valet push
```

Copies your personal keys into the team store so teammates get them too.

## Sharing with a Team

### Embedded store (simplest)

Secrets live in the same repo as the code. Bob gets secrets on `git pull`.

```bash
# Alice
cd ~/code/my-api && valet init
valet secret set OPENAI_API_KEY --value sk-abc123
valet user add bob --github bob-smith              # or --key age1qy2x3...
valet env grant bob -e dev
git add . && git commit -m "add valet" && git push

# Bob
git clone my-api && cd my-api
valet drive -- npm start                           # just works
```

### Team store (multi-repo)

For secrets shared across repos, use a standalone git-backed store.

```bash
# Alice creates and populates the store
valet store create github:acme/api-secrets
valet secret set DATADOG_API_KEY --value dd-key -s api-secrets

# Alice adds Bob — fetches his SSH key, invites him to the GitHub repo
valet user add bob --github bob-smith -s api-secrets
valet env grant bob -e dev -s api-secrets
valet push -s api-secrets
→ Fetched SSH key for github.com/bob-smith
→ Added user "bob"
→ Invited bob-smith as collaborator on acme/api-secrets
→
→ Tell bob to run:
→   valet join github:acme/api-secrets
```

```bash
# Bob joins — auto-accepts the GitHub invite if pending
valet join github:acme/api-secrets
→ Found pending invite for acme/api-secrets. Accepting...
→ Accepted!
→ Joined as "bob"
→
→ Secrets you can access:
→   dev: (1 scope(s), 1 secret(s))
→     DATADOG_API_KEY
→
→ Link to a project:
→   cd ~/code/my-project && valet link api-secrets

# Bob can rename the store locally if he wants
valet join github:acme/api-secrets --as team-keys
```

```bash
# Bob links and runs
cd ~/code/api
valet link api-secrets
valet drive -- npm start
```

### Adding teammates

Three ways to add someone, from easiest to most manual:

**1. GitHub SSH key (easiest)** — Alice does everything. Bob does nothing.

```bash
# Alice (embedded store):
valet user add bob --github bob-smith
valet env grant bob -e dev
git add . && git commit -m "grant bob" && git push

# Alice (team store — also invites Bob to the private repo):
valet user add bob --github bob-smith -s api-secrets
valet env grant bob -e dev -s api-secrets
valet push -s api-secrets
→ Invited bob-smith as collaborator on acme/api-secrets
→
→ Tell bob to run:
→   valet join github:acme/api-secrets

# Bob (embedded):
git pull && valet drive -- npm start

# Bob (team store):
valet join github:acme/api-secrets              # auto-accepts GitHub invite
cd ~/code/my-project && valet link api-secrets
valet drive -- npm start
```

Alice fetches Bob's public key from `github.com/bob-smith.keys` and encrypts secrets to it. For team stores, valet also invites Bob as a GitHub collaborator on the private repo and shows Bob the exact join command. Check if someone has SSH keys at `github.com/<username>.keys`.

**2. Invite** — For teammates without SSH keys on GitHub.

```bash
# Alice creates an invite
valet invite create -e dev
# → AGE-SECRET-KEY-1XYZ...  (share this privately with Bob)

# Bob uses the invite (embedded store — already cloned the repo):
valet join --invite AGE-SECRET-KEY-1XYZ...

# Bob uses the invite (team store — clone + join in one step):
valet join github:acme/api-secrets --invite AGE-SECRET-KEY-1XYZ...
```

Bob gets immediate access. The invite key is single-use and expires (default 7 days). No key exchange needed — Alice shares one string, Bob runs one command.

**3. Manual key exchange** — Fallback when neither of the above works.

```bash
# Bob:
valet identity init && valet identity export       # → age1qy2x3...

# Alice:
valet user add bob --key age1qy2x3...
valet env grant bob -e dev
```

### Invite options

```bash
valet invite create -e dev                         # single-use, expires in 7 days
valet invite create -e dev -e staging              # grant multiple environments
valet invite create -e dev --expires 3d            # custom expiry
valet invite create -e dev --max-uses 5            # multi-use (e.g. onboard a whole team)
valet invite list                                  # show pending invites
valet invite prune                                 # remove expired invites
```

Expired invites are auto-pruned on `valet push`. Once used, the temporary key is removed from all vaults — it can never be reused.

### Granting and revoking

```bash
valet env grant bob -e dev
valet env grant bob -e prod
valet env revoke bob -e prod --rotate              # re-encrypts + flags secrets
```

## Bots & CI

Create a bot identity for CI runners, deploy pipelines, or any automated process.

```bash
valet bot create ci-runner --grant dev
→ VALET_KEY=AGE-SECRET-KEY-1QFNZ...

gh secret set VALET_KEY --body 'AGE-SECRET-KEY-1QFNZ...'
valet push
```

### GitHub Actions

```yaml
- uses: actions/checkout@v4
- run: curl -fsSL https://raw.githubusercontent.com/peterday/valet/main/install.sh | sh
- run: valet drive -- npm test
  env:
    VALET_KEY: ${{ secrets.VALET_KEY }}
```

Embedded stores need no extra setup — the vault is in the checkout. Standalone stores need `valet pull` first.

### Other platforms

Set `VALET_KEY` as an env var, then `valet drive -- <command>`. Works on Vercel, Fly, Railway, etc.

### Managing bots

```bash
valet bot list
valet bot create deploy-bot --grant prod
valet bot revoke old-bot --rotate
```

## Docker

```bash
# Simplest — sync to .env, Compose reads it automatically
valet sync .env && docker compose up

# Generate a compose override (no .env on disk)
valet sync docker-compose.override.yml --format compose-override --service api
valet drive -- docker compose up

# Docker run with flags
docker run $(valet sync --format docker -e prod) my-image
```

## Migrating from .env

Already have `.env` files? Import them:

```bash
valet identity init                                    # one time
cd ~/code/my-api
valet init                                             # creates encrypted store
valet import .env                                      # imports all key=value pairs
valet drive -- npm start                               # run with secrets injected
```

Your secrets are now encrypted. Delete the `.env` file or keep it for non-secret config. To generate a `.env` at any time: `valet sync .env`.

If you have per-environment `.env` files, import each one:

```bash
valet import .env                                      # → dev (default)
valet import .env.staging -e staging                   # → staging
valet import .env.production -e prod                   # → prod
```

Now you can run any environment:

```bash
valet drive -- npm start                               # dev
valet drive -e staging -- npm start                    # staging
valet drive -e prod -- npm start                       # prod
```

Other options:

```bash
valet import .env --overwrite                          # replace existing secrets
valet import .env --scope dev/runtime                  # import into specific scope
```

Valet handles comments (`#`), quoted values (`"val"`), and `export` prefixes.

## Declaring Requirements

`.valet.toml` declares what secrets a project needs. Committed to git — the contract.

```bash
valet require OPENAI_API_KEY --provider openai
valet require DATABASE_URL --description "Postgres connection string"
valet require SENTRY_DSN --optional
```

For providers with multiple keys, declare them all at once:

```bash
valet require --provider stripe                        # STRIPE_SECRET_KEY, STRIPE_PUBLISHABLE_KEY, STRIPE_WEBHOOK_SECRET
valet require --provider supabase                      # SUPABASE_URL, SUPABASE_ANON_KEY, SUPABASE_SERVICE_ROLE_KEY
valet require --provider supabase --optional           # all optional
```

```toml
default_env = 'dev'

[requires.OPENAI_API_KEY]
provider = 'openai'

[requires.DATABASE_URL]
description = 'Postgres connection string'

[requires.SENTRY_DSN]
optional = true

[requires.STRIPE_SECRET_KEY]
provider = 'stripe'

[requires.STRIPE_PUBLISHABLE_KEY]
provider = 'stripe'

[requires.STRIPE_WEBHOOK_SECRET]
provider = 'stripe'
```

`valet status` shows what's resolved. `valet setup` fills in the gaps interactively.

## Store Linking

Linked stores make their secrets available to a project. The simplest link is just a name — all keys, environments matched by name:

```toml
[[stores]]
name = "team-backend"
url = "git@github.com:acme/secrets-backend.git"
```

### Key filtering and name mapping

Filter to specific keys, or remap names when the source store uses different conventions:

```toml
[[stores]]
name = "team-infra"
url = "git@github.com:acme/secrets-infra.git"
keys = [
  "CACHE_URL",
  { local = "DATABASE_URL", remote = "POSTGRES_PRIMARY_URL" },
  { local = "DATABASE_URL_RO", remote = "POSTGRES_REPLICA_URL" },
]
```

Without `keys`, all secrets from the linked store are available.

### Environment mapping

When local and remote environment names differ:

```toml
[[stores]]
name = "team-backend"
url = "git@github.com:acme/secrets-backend.git"
environments = [
  { local = "dev", remote = "staging" },
]
```

Unmapped environments match by name — `production` → `production` is implicit.

### Resolution order

For a given key + environment:

1. **Embedded store** (`.valet/`) — local overrides always win
2. **Linked stores** — in declaration order (shared, then personal)
3. **Conflict** — if multiple linked stores provide the same key, `valet status` shows an error

### Link types

When `valet setup` finds a missing secret in another store, you choose how to connect it:

| Choice | What happens | Stored in | Who benefits |
|---|---|---|---|
| **Copy** | Value copied into embedded store | `.valet/secrets` (committed) | Everyone with project access |
| **Link (just me)** | Personal link to source store | `.valet.local.toml` (gitignored) | Only this developer |
| **Link (project)** | Project declares store dependency | `.valet.toml` (committed) | All contributors |

"Link (project)" only appears for git-backed stores — you can't ask teammates to use a store that only exists on your laptop.

## Provider Registry

Valet has a built-in registry of common API providers. When a required secret matches a known provider, `valet setup` opens the right browser page, validates the key format, and confirms it works.

Supported providers: **OpenAI**, **Anthropic**, **Stripe**, **Supabase**, **Fly.io**, **Exa**, **ElevenLabs**.

Providers are matched by env var name (not pattern inference):

```
OPENAI_API_KEY      → openai
STRIPE_SECRET_KEY   → stripe
SUPABASE_URL        → supabase
```

For non-standard names, set the provider explicitly:

```bash
valet require MY_AI_KEY --provider openai
```

Key rotation varies by provider — `valet rotate` guides you through the right process for each.

## CLI Reference

### Setup

```bash
valet identity init                                    # generate age keypair (one time)
valet init                                             # embedded store (default)
valet init --shared github:acme/secrets/api            # link team store + project
valet init --local my-keys                             # link personal store
valet init --shared github:acme/secrets --local my-keys  # both at once
valet import .env                                      # import from .env file
valet import .env -e prod --overwrite                  # import into prod, overwrite existing
valet require KEY [--provider X] [--optional]          # declare one requirement
valet require --provider stripe                        # declare all keys from a provider
valet setup                                            # interactive setup
valet status                                           # show resolved/missing
```

### Secrets

```bash
valet secret set KEY [--value val] [--provider X]      # set (prompts if no --value)
valet secret set KEY -e prod --value val               # set in specific environment
valet secret set KEY -s my-keys --value val             # set in specific store
valet secret get KEY                                   # get value
valet secret list                                      # list in current env
valet secret history KEY                               # version history
valet secret remove KEY --scope path                   # remove
valet secret copy KEY --from <store>                   # copy one secret into this project
valet secret sync --to <store>                         # copy all resolved secrets into a store
```

### Running & Exporting

```bash
valet drive -- <command>                               # inject secrets and run (alias: valet run)
valet drive -e prod -- <command>                       # specific environment
valet sync .env                                        # dotenv file
valet sync --format json                               # JSON
valet sync --format shell                              # export KEY=val
valet sync --format docker                             # --env KEY=val flags
valet sync --format k8s-secret                         # Kubernetes Secret YAML
valet sync --format github-actions                     # KEY=val for $GITHUB_ENV
valet sync --format compose                            # docker-compose environment: block
valet sync --format compose-override --service api     # docker-compose.override.yml
```

### Environments & Scopes

```bash
valet env create <name>                                # create environment
valet env list                                         # list environments
valet env grant <user> -e <env>                        # grant access
valet env revoke <user> -e <env> [--rotate]            # revoke access
valet scope create <env/path>                          # create scope (advanced)
valet scope list                                       # list scopes
valet scope add-recipient <user> --scope <path>        # fine-grained access
valet scope remove-recipient <user> --scope <path>     # fine-grained revoke
```

### Users & Invites

```bash
valet user add <name> --github <handle>                # add by GitHub SSH key
valet user add <name> --key <pubkey>                   # add by age public key
valet user list                                        # list users
valet invite create -e <env>                           # create invite (prints temp key)
valet invite create -e dev -e staging --expires 3d     # multi-env, custom expiry
valet invite create -e dev --max-uses 5                # multi-use invite
valet invite list                                      # list pending invites
valet invite prune                                     # remove expired invites
valet join <remote>                                    # clone shared store (auto-accepts GitHub invite)
valet join <remote> --as <local-name>                  # clone with custom local name
valet join --invite AGE-SECRET-KEY-...                 # join with invite (embedded)
valet join <remote> --invite AGE-SECRET-KEY-...        # clone + join with invite
valet push                                             # push to remote
valet pull                                             # pull from remote
```

### Bots & CI

```bash
valet bot create <name> --grant <env>                  # create bot, prints VALET_KEY
valet bot list                                         # list bots
valet bot revoke <name> [--rotate]                     # revoke + remove bot
```

### Stores

```bash
valet store create my-secrets                          # personal local store
valet store create github:acme/secrets                 # team git-backed store (auto-creates repo)
valet store list                                       # list stores
valet store delete my-secrets                          # delete a local store
valet store delete my-secrets --force                  # skip confirmation
```

## Key Concepts

**Store** — Where secrets live. **Embedded** (`.valet/` in your project), **personal** (`~/.valet/stores/`), or **team** (git-backed). Stores layer — `valet drive` merges them all.

**Project** — One app or service within a store. Embedded stores have a single implicit project. Standalone stores can hold multiple — reference via URI: `github:acme/secrets/api`.

**Environment** — `dev`, `staging`, `prod`. Each has its own secrets. Switch with `-e`.

**Scope** — Encryption and access boundary. Secrets in a scope share the same recipients. Most projects use the `default` scope per environment and never think about scopes.

**Recipients** — Users whose keys can decrypt a scope's vault. `valet env grant` adds to all scopes. `valet env revoke` re-encrypts without them.

**Hierarchy:** Store → Project → Environment → Scope → Secrets.

## Scopes (Advanced)

By default, each environment has a single `default` scope — you never need to think about it. Scopes exist for when you need finer-grained access within an environment.

```bash
valet scope create dev/runtime
valet scope create dev/db
valet scope create dev/payments

valet secret set OPENAI_API_KEY --scope dev/runtime --value sk-abc
valet secret set DATABASE_URL --scope dev/db --value postgres://localhost/mydb
valet secret set STRIPE_KEY --scope dev/payments --value sk_test_xxx

# Bob gets runtime + db, but NOT payments
valet scope add-recipient bob --scope dev/runtime
valet scope add-recipient bob --scope dev/db
```

Most projects don't need this. If everyone should see all secrets in an environment, just use `-e` and ignore scopes entirely.

## Multiple Stores per Project (Advanced)

A project can pull secrets from multiple stores — your personal keys, a team store, and the embedded store.

```bash
valet init --shared github:acme/api-secrets/api --local github:pday/my-keys
```

Or link after init:

```bash
valet link github:pday/my-keys                         # personal (gitignored)
valet link github:acme/api-secrets/api --shared        # team (committed)
```

Secrets merge — embedded store wins on conflict:

```
personal (my-keys)      → OPENAI_API_KEY
team (api-secrets)      → DATADOG_API_KEY
embedded (.valet/)      → DATABASE_URL               ← wins on conflict
```

The resulting `.valet.toml`:

```toml
[[stores]]
name = "api-secrets"
url = "git@github.com:acme/api-secrets.git"
```

And `.valet.local.toml` (gitignored):

```toml
[[stores]]
name = "my-keys"
```

## Security

- [age](https://age-encryption.org/) multi-recipient public key encryption per scope
- Secret names visible in manifest (listing without decryption); values only in `vault.age`
- Identity keys at `~/.valet/identity/` — never committed
- GitHub SSH keys as age recipients — no key ceremony
- `--rotate` on revoke flags secrets for rotation
- Version history — up to 10 previous versions per secret
- `VALET_KEY` env var for bots — no key files in CI

## AI Tool Integration (MCP)

Valet includes a [Model Context Protocol](https://modelcontextprotocol.io/) server that lets AI coding tools (Claude Code, Cursor, etc.) manage secrets directly.

### Setup

```bash
valet mcp install                                      # auto-detect and configure
valet mcp install --claude-code                        # Claude Code only
valet mcp install --cursor                             # Cursor only
```

This registers Valet as an MCP server. Start a new session to use it.

### MCP Tools

| Tool | Purpose |
|------|---------|
| `valet_init` | Initialize Valet in a project, returns CLAUDE.md snippet |
| `valet_scan` | Scan for .env files, match keys against providers and wallet |
| `valet_status` | Project config, environments, secrets, requirements |
| `valet_wallet_search` | Check if user already has a key in their personal stores |
| `valet_link` | Link a store to the project — all its keys become available |
| `valet_copy` | Copy a single key from a store into the project |
| `valet_require` | Declare secret dependencies — single key or entire provider |
| `valet_provider_search` | Discover 70+ providers by name, category, or use case |
| `valet_help` | Full CLI reference (9 topics) |

### Typical AI workflow

**New project:**
1. **Init** — `valet_init` → sets up encrypted store, returns CLAUDE.md snippet
2. **Discover** — `valet_provider_search` query="payments" → finds Stripe, PayPal, etc.
3. **Require** — `valet_require` provider="stripe" → declares all Stripe env vars
4. **Find** — `valet_wallet_search` → checks if user already has the keys
5. **Connect** — `valet_link` or `valet_copy` based on user choice
6. **Setup** — if not found, ask user to type `! valet setup`

**Existing project with .env files:**
1. **Init** — `valet_init` → sets up encrypted store
2. **Scan** — `valet_scan` → finds .env files, matches keys against providers and wallet
3. **Connect** — link wallet for keys already there, `! valet import .env` for the rest
4. **Require** — `valet_require` for each key to declare requirements
5. **Cleanup** — user deletes .env files (secrets are now encrypted in .valet/)

### Design principles

- **Never returns secret values** — tools return names, sources, and metadata only
- **Secrets stay out of AI context** — users enter values via `! valet secret set KEY` (interactive)
- **Provider-first** — search 70+ providers, then batch-declare all their env vars
- **CLAUDE.md as memory** — `valet_init` generates a snippet so future sessions know to use Valet

## Development

```bash
make build       # bin/valet
make test        # all tests
```

**Stack:** Go, age encryption, git, TOML.

## Roadmap

- [x] Embedded, personal, and team stores with layering
- [x] Store URIs with project: `github:org/repo/project`
- [x] Projects, environments, scopes, recipients
- [x] `valet run` / `valet drive` — inject and run
- [x] `valet sync` — 8 export formats
- [x] `valet bot` — bot identities with `VALET_KEY`
- [x] `valet require` + `valet setup` — project onboarding
- [x] `valet secret sync --to` — promote between stores
- [x] Provider metadata, version history, rotation flags
- [x] GitHub SSH key import
- [x] `valet invite` — temp-key invites for teammates without SSH keys
- [x] `valet join` — auto-accept GitHub invites, show accessible secrets, `--as` alias
- [x] `valet user add --github` — auto-invite as GitHub collaborator
- [x] `valet store delete` — clean up local stores
- [x] Provider registry — setup URLs, key validation, rotation guidance
- [x] Store linking — key filtering, name mapping, environment mapping
- [x] MCP server — AI tool integration (Claude Code, Cursor)
- [ ] Provider automation — create/rotate keys via provider APIs (OpenAI, Fly.io)
- [ ] Cloud-backed stores with audit logs
- [ ] Kubernetes operator

## License

MIT
