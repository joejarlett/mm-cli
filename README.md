# mm

A single-binary CLI for the Meta-Me platform. One `mm login`, one bearer token, every app reachable from one prompt.

```
mm <app> <command> [args...]
```

## Install

```bash
bun run build:macos-arm64   # or build:macos-x64 / build:linux-x64
cp dist/mm-macos-arm64 ~/.mm/mm
chmod +x ~/.mm/mm
ln -sf ~/.mm/mm ~/.local/bin/mm           # shell PATH
ln -sf ~/.mm/mm ~/.mm/pi-agent/bin/mm     # so meta-me-local-agent's bash tool finds it
mm login
```

The token lands at `~/.config/mm/auth.json`. Manifest + Card caches at `~/.mm-cli/`.

The second symlink matters: the local agent (when started by launchd) gives its bash-tool subprocesses a stripped PATH that includes `~/.mm/pi-agent/bin` but *not* `~/.local/bin`. Without the symlink, the agent loses an LLM turn to `which mm` discovery on every chat that needs the CLI.

## What you can do

### Discovery

```bash
mm cards                          # capability matrix across all apps
mm cards kb                       # full Card for one app (description, tools, aliases)
mm manifest kb                    # deeper wire-level surface (every feature.action)
mm <app>                          # short — same as `mm cards <app>`
```

### Universal verbs (work on any app that publishes the contract)

```bash
mm <app> ask "..."                # agent.chat — natural-language question
mm <app> find "..."               # agent.search — entity lookup (where supported)
mm <app> do <tool> [k=v…]         # invoke a Card-declared tool by name
mm <app> <feature> <action> [k=v…] # raw dispatch to <app>/api/v2
```

`find` is capability-gated — `mm analytics find ...` will refuse cleanly and point you at `ask` because analytics doesn't advertise `search`.

> **Heads up — auth gap.** As of 2026-05-20, every app marks `agent.chat` / `agent.search` as `auth: "hub"`. The CLI bearer can't reach those directly — they require an HMAC-signed hub forward. Until a hub-side dispatch bridge ships (see [specs/architecture.md](specs/architecture.md) §4.7), the universal verbs are wire-correct but blocked at runtime for anything beyond `auth: "public"`. The hand-coded `mm kb …` and `mm crm …` wrappers still work because they hit each app's legacy `/api/rpc`.

### Per-app shortcuts (legacy, still working)

```bash
mm kb find <q>                    # semantic search
mm kb tree                        # list collections
mm kb peek <id>                   # preview a doc
mm kb read <id>                   # full doc body
mm kb collections                 # list collections

mm crm surface                    # today's priorities
mm crm contacts                   # list contacts
mm crm find <q>                   # search
mm crm log "<text>"               # log an interaction
mm crm context <person>           # person context
```

### Google Workspace (via the hub gateway)

```bash
mm calendar                       # agenda for 7 days
mm calendar new --title "..." --when "tomorrow 14:00"
mm tasks                          # pending tasks
mm tasks add "..." --due "next friday"
mm drive ls --q "name contains 'invoice'"
mm drive doc <name> --file path.md
```

Natural-language `--when` and `--due` accept chrono-node phrases. `--account` disambiguates across linked Google accounts.

### Local agent (mm chat)

```bash
mm chat                           # list recent local-agent threads
mm chat show <id>                 # print messages
mm chat search <q>                # substring search across messages
```

Currently reads the local `meta-me-local-agent` SQLite directly. Migrating to also drive the daemon over HTTP/WS is on the roadmap.

### Admin (requires `MM_DATABASE_URL`)

```bash
mm sql "<query>"                  # raw SQL on the hub Postgres
mm apps                           # list registered apps
mm app <slug> [enable|disable]    # inspect or toggle an app
mm health                         # hub health checks
mm errors                         # captured errors
```

### Misc

```bash
mm login | logout | whoami
mm status                         # auth + which apps the bearer sees
mm --json                         # append for parseable output on any command
mm stt <file>                     # transcribe audio
mm tts "<text>" --play            # synthesise speech
```

## How it works

mm is a thin dispatcher. The platform contract — `POST /api/v2 {feature, action, payload}` + `GET /api/v2/manifest` + `GET /.well-known/agent.json` — is the same shape on every app. mm fetches each app's Agent Card and manifest, validates locally, then dispatches with the bearer token.

The cross-app contract lives in [meta-me.uk/specs/cross-app-communication.md](../meta-me.uk/specs/cross-app-communication.md). The shared SDK every app uses to expose the contract is [@meta-me/app-agent](../meta-me-app-agent/).

Non-contract surfaces — Google Workspace, local agent, hub admin — go through their respective endpoints; mm just bundles them under one binary.

## Roadmap

See [specs/architecture.md](specs/architecture.md) for the full alignment plan with checkboxes. Headline open items:

- Hub-side dispatch bridge so CLI bearer can reach `auth: "hub"` actions.
- Top-level `mm ask "..."` for cross-app federation through the hub meta-agent.
- Migrate `mm chat` to drive `meta-me-local-agent` over HTTP/WS in addition to reading its SQLite.
- Card-driven aliases (in `@meta-me/app-agent`) so each app contributes its own ergonomic shortcuts.
- Delete the per-app wrappers once the universal verbs cover their cases.

## Source map

- [src/index.ts](src/index.ts) — entry point + top-level command routing.
- [src/dispatcher.ts](src/dispatcher.ts) — generic `POST <app>/api/v2` with bearer + manifest pre-validation.
- [src/manifest.ts](src/manifest.ts), [src/agent-card.ts](src/agent-card.ts) — fetch + cache the contract surfaces.
- [src/apps.ts](src/apps.ts) — app slug → base URL registry.
- [src/commands/app.ts](src/commands/app.ts) — universal verbs (`ask`/`find`/`do`/raw).
- [src/commands/cards.ts](src/commands/cards.ts), [src/commands/manifest.ts](src/commands/manifest.ts) — discovery.
- [src/commands/kb.ts](src/commands/kb.ts), [src/commands/crm.ts](src/commands/crm.ts) — legacy hand-coded wrappers (still the only path that works for non-public auth actions).
- [src/commands/calendar.ts](src/commands/calendar.ts), [tasks.ts](src/commands/tasks.ts), [drive.ts](src/commands/drive.ts), [email.ts](src/commands/email.ts) — Google Workspace via the hub.
- [src/commands/chat.ts](src/commands/chat.ts) — local-agent thread reader.
- [src/commands/hub.ts](src/commands/hub.ts) — admin DB commands.
- [src/commands/stt.ts](src/commands/stt.ts), [tts.ts](src/commands/tts.ts) — audio.
