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
valet drive -- uvicorn main:app --reload               # run with secrets injected
```

Secrets are encrypted in `.valet/`, injected at runtime by `valet drive`, and `.valet.toml` is safe to commit. Need a `.env` file? `valet sync .env`.

## Stores

A store is where your encrypted secrets live. Start simple, scale up as needed.

**Embedded** — `valet init` — secrets live in `.valet/` inside your project. Safe to commit. Best for solo devs and small teams.

**Personal** — `valet store create my-keys` — a standalone store at `~/.valet/stores/` for keys you reuse across projects. Back it up to GitHub with a private repo:

```bash
valet store create github:pday/my-keys             # creates + links to private repo
valet secret set OPENAI_API_KEY --value sk-abc -s my-keys
valet push -s my-keys                              # backs up to GitHub
```

Now your keys are encrypted on GitHub and sync across machines:

```bash
# On another machine
valet join github:pday/my-keys                     # clones your personal store
```

**Team** — `valet store create github:acme/api-secrets` — a standalone store backed by a git repo. The whole team shares it.

```bash
# Embedded (default)
cd ~/code/my-api && valet init

# Personal
valet store create my-keys
valet secret set OPENAI_API_KEY --value sk-abc -s my-keys

# Team
valet store create github:acme/api-secrets
```

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

## Using Personal Keys in a Project

You have API keys in a personal store. Your project needs them. Two ways to connect them:

### Link your personal store (keys stay personal)

```bash
cd ~/code/my-api
valet init --local my-keys
```

Now `valet drive` and `valet sync` pull from both your personal store and the project's embedded store. Nothing is copied — your keys stay in `my-keys`, and the link is in `.valet.local.toml` (gitignored, so teammates don't see it).

### Copy keys into the project store (self-contained)

```bash
valet secret sync --to .
```

This copies all resolved secrets into the embedded store. The project becomes self-contained — no personal store link needed. Good for when the project should have its own copy of the keys.

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

## Declaring Requirements

`.valet.toml` declares what secrets a project needs. Committed to git — the contract.

```bash
valet require OPENAI_API_KEY --provider openai
valet require DATABASE_URL --description "Postgres connection string"
valet require SENTRY_DSN --optional
```

```toml
default_env = 'dev'

[requires.OPENAI_API_KEY]
provider = 'openai'

[requires.DATABASE_URL]
description = 'Postgres connection string'

[requires.SENTRY_DSN]
optional = true
```

`valet status` shows what's resolved. `valet setup` fills in the gaps interactively.

## CLI Reference

### Setup

```bash
valet identity init                                    # generate age keypair (one time)
valet init                                             # embedded store (default)
valet init --shared github:acme/secrets/api            # link team store + project
valet init --local my-keys                             # link personal store
valet init --shared github:acme/secrets --local my-keys  # both at once
valet require KEY [--provider X] [--optional]          # declare a requirement
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
valet secret sync --to <store>                         # promote secrets between stores
```

### Running & Exporting

```bash
valet drive -- <command>                               # inject secrets and run
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

Secrets merge — project-specific wins on conflict:

```
personal (my-keys)      → OPENAI_API_KEY
team (api-secrets)      → DATADOG_API_KEY
embedded (.valet/)      → DATABASE_URL               ← wins on conflict
```

## Security

- [age](https://age-encryption.org/) multi-recipient public key encryption per scope
- Secret names visible in manifest (listing without decryption); values only in `vault.age`
- Identity keys at `~/.valet/identity/` — never committed
- GitHub SSH keys as age recipients — no key ceremony
- `--rotate` on revoke flags secrets for rotation
- Version history — up to 10 previous versions per secret
- `VALET_KEY` env var for bots — no key files in CI

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
- [x] `valet drive` — inject and run
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
- [ ] Provider automation — create/rotate keys via provider APIs
- [ ] Cloud-backed stores with audit logs
- [ ] Kubernetes operator

## License

MIT
