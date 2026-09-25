# mm-cli - notes for coding agents

Instructions for AI coding agents (and humans) working in this repo. Most of `mm` was
written by agents under these notes; they record the conventions that kept that work
consistent.

`mm` is a single-binary **Go** CLI for the Meta-Me platform. It authenticates with one
bearer token and talks to every app on the platform. Command handlers live in
`internal/cmd/*.go`; the entry point is `cmd/mm/main.go`.

> History: `mm` began as a TypeScript CLI under `src/`. The Go port fully replaced it
> and the TypeScript tree was **deleted** (2026-06-11). Keeping it around had become a
> trap: grepping for a command name found the dead `.ts` file first. There is now one
> tree. If a spec mentions `src/*.ts`, it describes removed code; the design may still
> hold, the paths do not.

## Read first

1. [`specs/architecture.md`](specs/architecture.md) - the alignment plan: how `mm` maps
   onto the platform's cross-app contract, and what is shipped vs pending.
2. [`specs/feedback.md`](specs/feedback.md) - the feedback-submission spec.

Some specs link to sibling repos in the wider platform (the hub, the shared SDK). Those
links do not resolve outside that workspace; treat them as context, not as files you can
open.

## What `mm` is and isn't

**It is a thin dispatcher.** Every app on the platform exposes the same shape:
`GET /.well-known/agent.json` (its Agent Card), `GET /api/v2/manifest`, and
`POST /api/v2 {feature, action, payload}`. `mm` handles discovery, validation and
dispatch over that contract. Per-app logic belongs **in the apps**, surfaced through
their Cards and manifests, not in this repo.

**It is not a per-app dev tool.** Individual apps can have their own internal CLIs for
development. `mm` is the user- and agent-facing surface across all of them. When a
change would teach `mm` something about one app's internals, that is usually a sign the
app's Card or manifest should carry it instead.

## Build, test, run

```bash
go run ./cmd/mm <args>          # run from source
go test ./...                   # all tests
go vet ./...                    # CI runs this too
go build -o mm ./cmd/mm         # release binary (gitignored at the repo root)

./scripts/build-tray.sh         # macOS only: package the menu-bar tray app
```

CI (`.github/workflows/ci.yml`) runs vet, tests, and a cross-compile check for
darwin/linux (arm64, amd64) and windows/amd64. Keep all five targets building:
platform-specific code goes behind `_unix.go` / `_windows.go` / `_linux.go` build
suffixes, as `internal/cmd/run_*.go` and `internal/host/` already do.

## Source map

```
cmd/mm/main.go        Entry point: root command, @mention preprocessing, registration
internal/
  cmd/                Cobra command handlers, one file per command group
    admin/            Platform admin commands (sql, apps, health, errors, users, ...)
    app.go            Universal dispatcher: ask / find / do / raw feature.action
    desk.go           Local-agent threads (list, show, search, nodes, models)
    chat_send.go      Sending a turn + the resilient WebSocket stream client
    mentions.go       @<entity> resolution and positional-arg preprocessing
    feedback.go       Built-in feedback submission
  http/               REST + WebSocket clients
  wire/               JSON request/response structures
  auth/ config/       Token storage (~/.config/mm/auth.json) and config parsing
  card/ manifest/     Agent Card and manifest fetch + cache
  version/            Version metadata
```

## Design notes worth knowing before you change things

### `@<entity>` mentions (`mentions.go`)

- `PreprocessArgs` runs over `os.Args[1:]` **before** Cobra parses anything, turning
  positional mentions such as `@fedora` into `--node fedora` or `--project <name>`.
  Without this, Cobra rejects them as unknown arguments.
- `ScanMessageMentions` finds node/project references inside free-text prompts. It
  must not match email addresses, strips a leading block of mentions from the outbound
  prompt, and unescapes `@@name` to a literal `@name`. Only a mention that resolves
  **unambiguously** routes anything; everything else passes through as text.
- Tests swap the package-level `loadNodesFunc` / `loadProjectsFunc` for fakes. Follow
  that pattern rather than reaching the network.

### Resilient stream (`chat_send.go`)

The client tracks the agent's monotonic event `cursor`. If the WebSocket drops
mid-turn it reconnects and sends `{type: "resume", threadId, cursor}`, and the agent
replays only what was missed, so nothing is printed twice.

The agent also closes the socket when a turn *finishes*, and that arrives as an
ordinary read error. `isCleanClose` (`chat_stream_close.go`) tells the two apart;
treating a clean close as a drop once made every completed turn reconnect, once a
second, forever. Any new read path must go through it.

### Feedback (`feedback.go`)

`mm feedback` (or `mm feedback submit`) posts friction reports, bugs and ideas, with
`--kind friction|bug|idea`, `--app`, `--context` and `--json`. It records its source
automatically (`agent` when `MM_AGENT=true` or `MM_SOURCE=agent`, otherwise `cli`) and
the CLI version. The point is that an agent that hits friction can report it in one
command instead of working around it silently.

## Conventions

- **Leaf commands that take no positional arguments set `Args: cobra.NoArgs`**, so
  stray input is rejected instead of ignored.
- **Tests are table-driven against mocked clients.** No live HTTP or WebSocket
  connections in tests; CI has no network path to the platform.
- **Output is designed for agents as much as for people.** This is the rule set that
  matters most, because agents are `mm`'s heaviest users:
  - Every query, inspect and status command supports `--json` (a persistent root
    flag).
  - **stdout is for payload only.** Progress, retries, update notices and warnings go
    to stderr, so `mm ... --json | jq` never breaks.
  - With `MM_AGENT=true` or `MM_SOURCE=agent`, no ANSI colour and no spinners. Where
    there is no terminal at all (setup scripts, remote shells) prefer a non-interactive
    mode, as `mm login --headless` does.
  - Never open an interactive pager. Help and output print straight to stdout/stderr.
- Keep `mm` thin. If a feature needs app-specific knowledge, put it in the app's Card
  or manifest and let the universal verbs (`ask`, `find`, `do`, raw dispatch) reach it.
