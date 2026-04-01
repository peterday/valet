# Valet UI Spec

Local web dashboard for managing stores, secrets, team access, and project setup. Runs as `valet ui` — single binary, localhost only, opens browser.

## Architecture

- **Server:** Go `net/http`, binds to `127.0.0.1` on a random available port
- **Templates:** Go `html/template` with `embed` for static assets
- **Styling:** Tailwind CSS (embedded), dark mode default
- **Interactivity:** htmx for dynamic updates (reveal, grant/revoke, save), ~50 lines custom JS (clipboard, auto-mask timer)
- **No JS framework.** No build pipeline. Everything embeds in the Go binary.
- **Lifecycle:** `valet ui` starts the server, opens browser, shuts down on 30 min idle or explicit close

## Entry Points

### CLI
```
valet ui                    # opens full dashboard
valet ui --port 8484        # specific port
```

If run inside a project directory (has `.valet.toml`), the Project tab is pre-loaded with that project.

### MCP
```
valet_setup_web             # starts server, opens browser to project setup page
                            # blocks until user submits, returns result
                            # used by Claude Code for zero-friction key entry
```

Same server, same UI. The MCP tool deep-links to the setup page and blocks until the form is submitted or times out (10 min).

## Navigation

Sidebar with icons:

| Nav Item     | Always visible | Description |
|-------------|----------------|-------------|
| **Stores**   | Yes            | All stores, secrets, environments |
| **Team**     | Yes            | Users, access matrix, invites |
| **Rotation** | Yes            | Secrets flagged for rotation |
| **Project**  | Yes            | Requirements, resolution, setup. Shows folder picker when no project selected |

Bottom of sidebar:
- Identity info (public key, truncated)
- Version number
- Link to docs

## Pages

### 1. Stores

**Store list** (default view)

Shows all stores from `~/.valet/stores/` plus the embedded store from the currently selected project (if any).

```
NAME                TYPE        SECRETS    ENVIRONMENTS
my-keys             local       12         dev, prod
work-keys           local        8         dev, staging, prod
acme-secrets        git         25         dev, staging, prod, canary
cmp_scanner/.valet  embedded     4         dev                          ← project
```

Each row is clickable.

**Store detail** (click into a store)

Tabs across the top for each environment. Each tab shows:

| SECRET | PROVIDER | SCOPE | UPDATED | UPDATED BY |
|--------|----------|-------|---------|------------|
| OPENAI_API_KEY | OpenAI | dev/default | 2 days ago | me |
| DATABASE_URL | — | dev/db | 1 week ago | alice |
| STRIPE_SECRET_KEY | Stripe | dev/runtime | 3 weeks ago | me |

- Values are masked by default: `sk-proj-****...7x3f`
- **Reveal button** per secret — shows full value for 30 seconds, then auto-masks
- **Copy button** — copies to clipboard without revealing visually. Toast: "Copied"
- Provider column shows provider badge/name if matched
- Never-revealed secrets show `••••••••` (no partial mask)

**Activity tab** (per store, for git-backed and embedded stores)

Rendered from git history:

```
2 hours ago    me      Set OPENAI_API_KEY           dev/default
1 day ago      alice   Granted staging access
3 days ago     me      Revoked bob from prod        (5 secrets flagged)
1 week ago     me      Added user alice
```

Not available for local-only stores with no git history.

**Key inventory** (sub-view or toggle)

Cross-store view — find a key across all stores:

```
OPENAI_API_KEY
  my-keys/dev          sk-proj-****...7x3f     2 days ago
  work-keys/dev        sk-proj-****...a1b2     3 months ago

STRIPE_SECRET_KEY
  my-keys/dev          sk_test_****...x9y0     3 weeks ago
  acme-secrets/prod    sk_live_****...m3n4     1 month ago
```

Useful for finding duplicates and knowing which store has the freshest version.

### 2. Team

**Access matrix** (default view)

Per-store dropdown at the top to select which store to manage. Shows users vs environments:

```
acme-secrets
                 dev      staging     prod
me                ✓         ✓          ✓
alice             ✓         ✓          ✗      [Grant prod]
bob               ✓         ✗          ✗      [Grant staging] [Grant prod]
```

- Click a ✗ to grant → htmx POST → refreshes the row
- Click a ✓ to revoke → confirmation modal: "Revoke alice from prod? This will flag 3 secrets for rotation." → htmx POST
- Visual distinction between store owner and granted users

**Invite management** (sub-tab)

```
ACTIVE INVITES
ID          ENVIRONMENTS    CREATED BY    EXPIRES       USES
a1b2c3d4    dev, staging    me            in 2 days     1/5

[Create invite]
```

Create invite form: select environments, set expiry, set max uses. Shows the invite key to copy.

Prune expired invites button.

### 3. Rotation

All stores, all environments. Shows secrets flagged for rotation:

```
5 secrets need rotation

SECRET              STORE           ENV     REASON              FLAGGED      GUIDANCE
STRIPE_SECRET_KEY   acme-secrets    prod    bob revoked         3 days ago   Rolling — 24h grace period
DATABASE_URL        acme-secrets    prod    bob revoked         3 days ago   —
API_KEY             work-keys       staging alice revoked       1 week ago   —
OPENAI_API_KEY      acme-secrets    prod    bob revoked         3 days ago   Create-then-revoke
SENTRY_DSN          acme-secrets    prod    bob revoked         3 days ago   —
```

- Guidance column pulls from the provider registry (rotation strategy, warnings)
- Link to provider's revoke/setup URL where available
- "Mark as rotated" button per secret (clears the flag after user confirms they've rotated the key)

Empty state: "No secrets need rotation. Looking good."

### 4. Project

**Folder picker** at the top:
- Shows current project path (from launch directory, or manually selected)
- Browse button opens native file dialog
- Text input for pasting a path
- Clear button to deselect project
- When no project is selected: "Open a project folder to see its requirements, resolution, and setup."

**Requirements** (default tab when project is selected)

```
PROJECT: cmp_scanner
STORE: embedded (.valet/)
ENVIRONMENT: dev

REQUIREMENTS
STATUS    SECRET              PROVIDER     DESCRIPTION                    SOURCE
  ✓       ANTHROPIC_API_KEY   Anthropic    —                              my-keys/dev (linked)
  ✓       BRIGHTDATA_PROXY    —            EU geo-fenced proxy            .valet/dev/default
  ✗       OPENAI_API_KEY      OpenAI       —                              MISSING
  -       SENTRY_DSN          Sentry       Error tracking (optional)      not set
```

Environment selector to switch between dev/staging/prod.

**Resolution** (tab)

Visual representation of where each secret comes from in the resolution chain:

```
OPENAI_API_KEY
  ✗ .valet.local/dev       —
  ✗ .valet.local/*         —
  ✗ .valet/dev             —
  ✗ .valet/*               —
  ✓ my-keys/dev            sk-proj-****...7x3f    ← winner
  ✗ acme-secrets/dev       —
```

Shows the full priority chain with the winning source highlighted. Helps debug "why is this key coming from that store?"

**Setup** (tab — the paste-keys page)

Shows only missing/unconfigured requirements. This is the page the MCP `valet_setup_web` tool deep-links to.

```
3 secrets to configure

┌─────────────────────────────────────────────────┐
│  OPENAI_API_KEY                                 │
│  OpenAI — AI API for GPT models, embeddings     │
│  $5 free credit on signup                       │
│  [Get a key at platform.openai.com →]           │
│                                                 │
│  ┌───────────────────────────────────────────┐  │
│  │                                           │  │
│  └───────────────────────────────────────────┘  │
│  Format: sk-...                                 │
└─────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────┐
│  DATABASE_URL                                   │
│  Postgres connection string                     │
│                                                 │
│  ┌───────────────────────────────────────────┐  │
│  │                                           │  │
│  └───────────────────────────────────────────┘  │
└─────────────────────────────────────────────────┘

                                       [Save all]
```

- Provider cards with logo/name/description/free tier/setup link
- Input validation: prefix checking in real-time (sk- for OpenAI, sk_test_ for Stripe)
- Valid format → green checkmark
- Invalid format → amber warning (not blocking — might be a new format)
- Save encrypts values directly into the store
- On save success: green toast, card collapses, progress updates
- Optional secrets show "skip" option

**Linked stores** (tab)

Shows which stores are linked and how:

```
LINKED STORES
NAME            TYPE       KEY FILTER    ENV MAPPING     CONFIG
my-keys         personal   all keys      —               .valet.local.toml
acme-secrets    shared     3 keys        dev→staging     .valet.toml

my-keys provides: OPENAI_API_KEY, ANTHROPIC_API_KEY, ...
acme-secrets provides: DATABASE_URL, STRIPE_SECRET_KEY, CACHE_URL
```

## Visual Design

**Aesthetic:** Linear meets 1Password. Clean, modern, trustworthy.

- **Dark mode default** (light mode toggle)
- **Font:** Inter (sans) + JetBrains Mono (monospace for secret names, keys)
- **Colors:**
  - Background: slate-900 (dark), white (light)
  - Sidebar: slate-950 (dark), slate-50 (light)
  - Cards: slate-800 (dark), white with border (light)
  - Accent: indigo-500 for actions
  - Success: emerald-500
  - Warning: amber-500
  - Error: rose-500
- **Spacing:** generous — 16px minimum padding on cards, 24px between sections
- **Borders:** subtle, 1px, slate-700 (dark) / slate-200 (light)
- **Radius:** 8px on cards, 6px on inputs, 4px on badges
- **Shadows:** minimal in dark mode, subtle in light mode
- **Transitions:** 150ms ease for hover states, 200ms for reveals

**Status indicators:**
- ✓ configured: emerald dot
- ✗ missing: rose dot
- - optional/unset: slate dot
- ⚠ needs rotation: amber dot with pulse animation

**Provider badges:**
- Small pill with provider name: `[OpenAI]` `[Stripe]` `[Supabase]`
- Color-coded by category (AI = purple, payments = blue, cloud = cyan, etc.)

**Secret reveal animation:**
- Click reveal → value fades in over 200ms
- 30 second countdown ring around the reveal button
- Auto-fades back to masked at 0

**Toast notifications:**
- Bottom-right, slides up
- Auto-dismiss after 3 seconds
- "Copied to clipboard" / "Secret saved" / "Access granted"

## API Routes (internal, localhost only)

```
GET  /                              → redirect to /stores
GET  /stores                        → store list
GET  /stores/:name                  → store detail (env tabs, secrets)
GET  /stores/:name/activity         → git log for store
GET  /stores/:name/inventory        → cross-env key view
POST /stores/:name/secrets/:key/reveal  → returns decrypted value (htmx)
POST /stores/:name/secrets/:key/copy    → returns value for clipboard (htmx)

GET  /team                          → access matrix
POST /team/grant                    → grant access (store, user, env)
POST /team/revoke                   → revoke access (store, user, env)
GET  /team/invites                  → invite list
POST /team/invites                  → create invite
POST /team/invites/:id/prune        → delete expired invite

GET  /rotation                      → flagged secrets across all stores
POST /rotation/:store/:scope/:key/clear  → clear rotation flag

GET  /project                       → project view (requirements, resolution)
POST /project/select                → set project path
GET  /project/setup                 → setup page (missing requirements)
POST /project/setup                 → save secrets from setup form
GET  /project/resolution            → resolution chain view
GET  /project/links                 → linked stores view
```

## MCP Integration

New MCP tool: `valet_setup_web`

```
Description: Open a browser-based setup page for entering missing secret values.
             Values go from browser → local server → encrypted store.
             Never passes through the AI context.
             Blocks until the user submits the form or 10 minute timeout.

Parameters:
  keys (optional): comma-separated list of specific keys to set up.
                   If omitted, shows all missing requirements.

Returns:
  On submit: "Configured 3 secrets: OPENAI_API_KEY, STRIPE_SECRET_KEY, DATABASE_URL"
  On timeout: "Setup page timed out. User can run 'valet ui' to try again."
  On cancel: "User closed the setup page without saving."
```

Flow:
1. MCP tool starts the web server (if not already running)
2. Opens browser to `localhost:PORT/project/setup?keys=OPENAI_API_KEY`
3. Tool call blocks on a channel
4. User fills in form, clicks Save
5. POST handler saves secrets, sends result on the channel
6. Tool call returns with the result
7. Server stays running (for `valet ui` use) or shuts down after idle timeout

## Security

- **Localhost only** — binds to `127.0.0.1`, never `0.0.0.0`
- **Random port** — no predictable port for other processes to probe
- **CSRF token** — all POST routes require a token from the page
- **No secret values in HTML** — reveal is a separate POST that returns the value via htmx swap, never in the initial page load
- **Auto-shutdown** — 30 minute idle timeout
- **No persistent state** — server is stateless, reads from stores on each request

## File Structure

```
internal/ui/
  server.go          # HTTP server, router, lifecycle
  handlers.go        # route handlers
  stores.go          # store-related handlers
  team.go            # team/access handlers
  rotation.go        # rotation handlers
  project.go         # project handlers + setup
  mcp.go             # MCP setup_web integration
  templates/
    layout.html      # base layout with sidebar
    stores.html
    store_detail.html
    team.html
    rotation.html
    project.html
    setup.html
    components/
      secret_row.html
      access_matrix.html
      provider_card.html
      toast.html
  static/
    style.css        # Tailwind output (built once, embedded)
    htmx.min.js      # ~14kb
    app.js           # clipboard, auto-mask, dark mode toggle
    favicon.svg
```

All embedded via `//go:embed` in the binary.

## Build

One-time Tailwind build:
```
npx tailwindcss -i static/input.css -o static/style.css --minify
```

The output CSS is committed and embedded. No runtime build step. `npx` is only needed when changing styles during development.

## Not in scope (for now)

- Secret value editing in the UI (use CLI: `valet secret set`)
- Store creation/deletion (use CLI: `valet store create`)
- Provider catalog browsing (use CLI/MCP: `valet providers list`)
- Import from .env (use MCP: `valet_import`)
- Push/pull git operations (use CLI: `valet push`, `valet pull`)
- Mobile-responsive layout (localhost, always desktop)
