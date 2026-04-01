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
  valet require KEY [--provider X] [--optional]        # declare a requirement
  valet setup                                          # interactive setup for missing secrets
  valet status                                         # show required vs available`

const helpSecrets = `Secret commands:

  valet secret set KEY [--value val] [--provider X]    # set (prompts if no --value)
  valet secret set KEY -e prod --value val             # set in specific environment
  valet secret set KEY -s my-keys --value val          # set in specific store
  valet secret get KEY                                 # get value
  valet secret list                                    # list in current env
  valet secret history KEY                             # version history
  valet secret remove KEY --scope path                 # remove
  valet secret sync --to <store>                       # promote secrets between stores

Important: valet secret set without --value prompts the user interactively.
Use this to let the user enter sensitive values without exposing them.`

const helpRunning = `Running and exporting:

  valet drive -- <command>                             # inject secrets and run (alias: valet run)
  valet drive -e prod -- <command>                     # specific environment
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
  valet link <store-name>                              # link store to current project
  valet link <store-name> --shared                     # link as shared (committed)

Store types:
  Embedded — .valet/ in your project. Encrypted values safe to commit.
  Personal — ~/.valet/stores/. Your keys, reused across projects.
  Team — git-backed. Shared via a private repo.

Stores layer: personal → team → embedded. Embedded wins on conflict.`

const helpSecurity = `Security model:

  - age (https://age-encryption.org/) public key encryption per scope
  - Secret names visible in manifest; values only in encrypted vault.age
  - Identity keys at ~/.valet/identity/ — never committed
  - GitHub SSH keys as age recipients — no key ceremony
  - --rotate on revoke flags secrets for rotation
  - Version history — up to 10 previous versions per secret
  - VALET_KEY env var for bots — no key files in CI`

const helpFull = helpSetup + "\n\n" + helpSecrets + "\n\n" + helpRunning + "\n\n" + helpEnvironments + "\n\n" + helpUsers + "\n\n" + helpBots + "\n\n" + helpStores + "\n\n" + helpSecurity
