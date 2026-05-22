# mm-cli — agent notes

A single-binary Go CLI for the Meta-Me platform. Authenticates via a single bearer token and communicates with onboarded services.

## Before touching this repo, read

1. **[specs/architecture.md](specs/architecture.md)** — the alignment plan.
2. **[specs/feedback.md](specs/feedback.md)** — the lightweight feedback submission spec.

## What `mm` is and isn't

**Is:** a thin dispatcher. The platform contract is the same shape on every app — `POST /api/v2 {feature, action, payload}` — and `mm` does discovery, validation, and dispatch over that. Per-app logic belongs in the apps, surfaced through their Agent Cards and manifests.

**Isn't:** the per-repo CLIs (`cli/kb.ts` etc.). Those are dev tools inside each repo. `mm` is the user/agent-facing multi-app surface.

## Build, Test & Run

```bash
go run ./cmd/mm <args>                   # run directly via Go
go test ./...                            # run all Go tests
go build -o mm ./cmd/mm                  # build the release binary
cp mm ~/.local/bin/mm
```

## Source map

```
cmd/
└── mm/
    └── main.go       Entry point — sets up mentions preprocessor and registers commands
internal/
├── cmd/              Command line handlers (Cobra)
│   ├── admin/        Hub admin commands (sql, apps, health, errors)
│   ├── app.go        Universal/dynamic tool dispatcher
│   ├── chat.go       Local-agent thread command
│   ├── chat_send.go  Local-agent chat sending & WebSocket resilient client (cursor reconnect)
│   ├── feedback.go   Built-in feedback submission subcommand
│   ├── mentions.go   @<entity> mentions resolution & positional arg preprocessor
│   └── (others)      Calendar, tasks, email, drive, stt, tts, status, logic, version
├── http/             HTTP + Agent API REST/WebSocket client
├── wire/             JSON protocol request/response structures
├── config/           Config parser (~/.config/mm/auth.json)
└── version/          Version metadata constants
```

## Key Architectural Upgrades (Go Port)

### 1. `@<entity>` Mentions Preprocessor (`mentions.go`)
* **Preprocessing**: Intercepts `os.Args[1:]` before Cobra executes to translate positional mentions (e.g. `@fedora`) into `--node fedora` or `--project` flags to avoid "unknown argument" errors.
* **ScanMessageMentions**: Manual lookbehind-supported regex parsing to identify node or project references in chat prompts without matching email addresses, stripping leading blocks, and unescaping `@@`.
* **Testing**: Supports testing via package-level `loadNodesFunc` and `loadProjectsFunc` mocks.

### 2. Resilient WebSocket stream (`chat_send.go`)
* **Auto-Reconnection**: The client tracks the monotonic event `cursor`. If the connection drops mid-stream, it automatically reconnects and issues `{type: "resume", threadId, cursor}` to stream back-buffered events from the agent without duplicate stdout outputs.

### 3. Feedback Submission (`feedback.go`)
* **Built-in subcommands**: `mm feedback` or `mm feedback submit` posts friction reports, bugs, or ideas.
* **Options**: `--kind` (friction, bug, idea), `--app`, `--context` and `--json`.
* **Auto-captures**: Source (`"agent"` if `MM_AGENT` or `MM_SOURCE` is configured, otherwise `"cli"`) and CLI tool version.

## Conventions

- Cobra constraints: leaf commands with no positional parameters must enforce `Args: cobra.NoArgs` to reject arbitrary inputs gracefully.
- Test suites: Table-driven assertions utilizing mocked clients rather than making live HTTP/WebSocket tailnet connections.
