# mm-cli — agent notes

A single-binary CLI for the Meta-Me platform. Distributed as one compiled Bun binary; one `mm login` serves every app via a single bearer token.

## Before touching this repo, read

1. **[specs/architecture.md](specs/architecture.md)** — the alignment plan with checkboxes for what's shipped, in-flight, and blocked. Treat as the single source of truth for in-flight work.
2. **[meta-me.uk/specs/cross-app-communication.md](../meta-me.uk/specs/cross-app-communication.md)** — the platform contract every app conforms to (`/api/v2`, `/api/v2/manifest`, `/.well-known/agent.json`, reserved `agent.chat` / `agent.search`, HMAC auth model).
3. **[../meta-me-app-agent/](../meta-me-app-agent/)** — the shared SDK every onboarded app uses to expose the contract. Card generation, MCP tool helpers, HMAC verification, digest emission live here.

## What `mm` is and isn't

**Is:** a thin dispatcher. The platform contract is the same shape on every app — `POST /api/v2 {feature, action, payload}` — and `mm` does discovery, validation, and dispatch over that. Per-app logic belongs in the apps, surfaced through their Agent Cards and manifests.

**Isn't:** the per-repo CLIs (`cli/kb.ts` etc.). Those are dev tools inside each repo. `mm` is the user/agent-facing multi-app surface.

## Two CLIs spelled `mm`

There's another `mm` and they get confused often:

- **This binary** (`~/.local/bin/mm`, compiled from this repo) — Joe's user-facing CLI for KB / CRM / Google Workspace / STT / TTS / local-agent. Authenticates with `mm_…` bearer.
- **`npm run mm`** inside `~/Documents/dev/meta-me.uk/` — the hub's platform admin DB CLI (`cli/mm.ts`). Direct Postgres access. Local-only.

Some admin verbs (`mm sql`, `mm apps`, `mm app`, `mm errors`, …) have been ported from the hub CLI into this binary — they require `MM_DATABASE_URL`. If a verb is admin-shaped and only useful with a DB URL, it belongs in [src/commands/hub.ts](src/commands/hub.ts).

## Source map

```
src/
├── index.ts          entry point — command routing
├── auth.ts           bearer token + userId load/save (~/.config/mm/auth.json)
├── apps.ts           slug → base URL registry
├── manifest.ts       fetch + cache /api/v2/manifest
├── agent-card.ts     fetch + cache /.well-known/agent.json
├── dispatcher.ts     POST <app>/api/v2 with bearer + manifest pre-validation
├── hub.ts            POST meta-me.uk/api/mm for hub-proxied features
├── db.ts             optional direct Postgres (admin only, MM_DATABASE_URL)
├── nl-date.ts        chrono-node wrapper for --when / --due
└── commands/
    ├── app.ts        universal verbs (ask, find, do, raw <feature> <action>)
    ├── cards.ts      mm cards [<app>] — discovery
    ├── manifest.ts   mm manifest [<app>] — wire-level view
    ├── kb.ts, crm.ts legacy hand-coded wrappers (still active)
    ├── calendar.ts, tasks.ts, drive.ts, email.ts   Google Workspace via hub
    ├── chat.ts       local-agent (meta-me-local-agent) thread reader
    ├── project.ts    local-agent project index (overview/detail/add/rebuild)
    ├── hub.ts        admin DB commands (sql/apps/app/health/errors)
    ├── stt.ts, tts.ts   audio in/out
    ├── login.ts, status.ts, v2.ts   auth + deprecated dispatch alias
```

## Adding a new built-in command

For something that doesn't fit any app contract (audio, local files, hub admin):

1. New file under `src/commands/<name>.ts` exporting a dispatch function.
2. Add an `import` and a `case '<name>':` to the switch in [src/index.ts](src/index.ts).
3. Add a short blurb to `printHelp()`.

## Adding a new v2-contract app

You don't add code in this repo — you onboard the app upstream:

1. The app adopts [@meta-me/app-agent](../meta-me-app-agent/) per the cross-app spec's checklist (§4.5).
2. The app serves `/api/v2`, `/api/v2/manifest`, `/.well-known/agent.json`.
3. Add the slug + URL to `APPS` in [src/apps.ts](src/apps.ts).

That's it. `mm <slug>`, `mm <slug> ask`, `mm <slug> find`, `mm <slug> <feature> <action>`, `mm cards <slug>`, `mm manifest <slug>` all work generically through [src/commands/app.ts](src/commands/app.ts).

## Auth model (and the current gap)

The CLI bearer `mm_…` is the user's API key, validated against the platform `auth.account` table via `meta-me.uk/api/cli/validate`. The dispatcher attaches `Authorization: Bearer …` + `X-Hub-User-Id: …` + optional `X-Hub-Instance-Id: …`.

**Important gap (2026-05-20):** the v2 contract's SDK (`verifyHubRequest`) accepts session cookies or HMAC-signed hub forwards — not the CLI bearer. So actions marked `auth: "session" | "either" | "hub"` are unreachable from the CLI today. Only `auth: "public"` works through the generic dispatcher. The hand-coded `mm kb` / `mm crm` wrappers continue to work because they hit each app's legacy `/api/rpc` which has its own bearer-aware auth.

See [specs/architecture.md](specs/architecture.md) §4.7 for the two viable fixes (hub-side dispatch bridge vs SDK accepting CLI bearer).

## Build + run

```bash
bun run dev <args>                       # iterate (TS direct)
bun run build:macos-arm64                # → dist/mm-macos-arm64
cp dist/mm-macos-arm64 ~/.local/bin/mm
```

The compiled binary is the distribution unit. Type-checking happens implicitly via `bun build --compile`.

## Conventions

- Per-command flag parsing is local to each command file; only `--help`, `-h`, `--json`, `--version`, `-v`, `--refresh`, `--no-validate`, `--instance` are stripped globally (see [src/index.ts](src/index.ts) `GLOBAL_FLAGS`).
- All commands honour `--json` for parseable output.
- Manifest cache TTL is 24h; `--refresh` busts. Same for Card cache.
- Errors render as `✗ HTTP <code> [<code>] <message>` when the response carries a JSON:API `{errors: [...]}` envelope; otherwise raw body.
- Pre-validation against the manifest is on by default for the generic dispatcher; `--no-validate` skips.

## Non-contract surfaces

These don't speak the v2 contract and shouldn't be forced into it:

- **`mm chat`** — reads `meta-me-local-agent`'s local SQLite. The daemon has its own REST + WS surface ([../meta-me-local-agent/README.md](../meta-me-local-agent/README.md)). Roadmap: also drive the daemon over HTTP/WS.
- **`mm project`** — hits the local-agent's REST surface for the project index (overview / detail / add / rebuild). Default base URL `http://localhost:3142`, override with `MM_LOCAL_AGENT_URL`. No auth — localhost/tailnet trust by design. Pair with the agent's `project_index_query` tool: same machinery, terminal view.
- **`mm calendar`, `mm tasks`, `mm drive`, `mm email send`** — go through `meta-me.uk/api/mm`, which proxies to the `gws-gateway` container for Google Workspace. The hub holds the OAuth refresh tokens.
- **`mm email` list/get** — platform mail log on the hub.
- **`mm sql`, `mm apps`, `mm app`, `mm health`, `mm errors`** — admin, direct Postgres via `MM_DATABASE_URL`.

If a new feature is hub-proxied (Google or otherwise), it goes through `src/hub.ts`'s `hubApi()` to `meta-me.uk/api/mm`, not through the v2 dispatcher.

## Where things land

- Auth: `~/.config/mm/auth.json`
- Manifest cache: `~/.mm-cli/manifests/<slug>.json`
- Card cache: `~/.mm-cli/cards/<slug>.json`
- Binary: `~/.mm/mm` (canonical), symlinked into `~/.local/bin/mm` (shell PATH) and `~/.mm/pi-agent/bin/mm` (local-agent bash PATH).

## Local-agent integration

The `meta-me-local-agent` daemon (binary at `~/.mm/agent`, source at [../meta-me-local-agent/](../meta-me-local-agent/)) gives its chat agent a `bash` tool. When launched by launchd, that subprocess PATH is `~/.mm/pi-agent/bin:/usr/bin:/bin:/usr/sbin:/sbin` — `~/.local/bin` is *not* on it. Hence the second install symlink (`~/.mm/pi-agent/bin/mm → ~/.mm/mm`) — without it the agent burns an extra tool call doing `which mm` before every CLI invocation.

The agent loads `~/.mm/.env` at startup and inherits it to children (`HUB_HMAC_SECRET`, `MM_DATABASE_URL`, …). The env loader skips keys already in `process.env`, so `PATH` overrides from `.env` are ignored — extending PATH for child shells happens via the symlink trick, not env.

End-to-end verified 2026-05-20: agent successfully invokes `mm cards`, `mm whoami` etc. via its bash tool, single tool call per question.
