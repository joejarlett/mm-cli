# mm

A single-binary Go CLI for the Meta-Me platform. One `mm login`, one bearer token, every app reachable from one prompt.

```
mm [app] [command] [args...]
```

## Install

```bash
go build -o mm ./cmd/mm
cp mm ~/.mm/mm
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
mm [app]                          # short — same as `mm cards <app>`
```

### Universal verbs (work on any app that publishes the contract)

```bash
mm [app] ask "..."                # agent.chat — natural-language question
mm [app] find "..."               # agent.search — entity lookup (where supported)
mm [app] do <tool> [k=v…]         # invoke a Card-declared tool by name
mm [app] <feature> <action> [k=v…] # raw dispatch to <app>/api/v2
```

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

### Feedback submission
```bash
mm feedback "CRM logs need a default instance"
mm feedback submit "unintuitive error in kb status" --kind bug --app kb
```

### Local agent (mm chat)

```bash
mm chat                           # list recent local-agent threads
mm chat show <id>                 # print messages
mm chat search <q>                # substring search across messages
mm chat send "hello @fedora"      # send message to local-agent with @node mention
```

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

## Source map

- [cmd/mm/main.go](cmd/mm/main.go) — Entry point + dynamic mentions argument preprocessing.
- [internal/cmd/](internal/cmd) — Handlers and implementation of all subcommands.
- [internal/http/](internal/http) — REST and WebSocket clients.
- [internal/wire/](internal/wire) — Hub & Agent payload data structures.
- [internal/config/](internal/config) — Configuration state reader and writer.
