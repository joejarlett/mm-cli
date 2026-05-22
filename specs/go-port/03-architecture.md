# mm-cli — Go architecture

> Package layout, library choices, error model. Maps the (now-converged) TS shape 1:1 into Go.

---

## 1. Module + repo

```
module github.com/jjarlett/mm-cli

go 1.23
```

Single repo. No internal/external split — this is a thin CLI, not a library. Anyone importing it is doing something we don't intend.

Build target: `mm` binary, cross-compiled for darwin/linux × amd64/arm64.

---

## 2. Package layout

```
mm-cli/
├── cmd/
│   └── mm/                 // package main — Cobra root + wiring
│       └── main.go
├── internal/
│   ├── auth/               // AuthState load/save, device flow client
│   │   ├── auth.go
│   │   └── device.go
│   ├── config/             // Config struct + env loader (mirrors src/config.ts)
│   │   └── config.go
│   ├── http/               // The unified client (mirrors src/http/client.ts)
│   │   ├── client.go       //   Client struct + Hub/V2/Rpc methods
│   │   ├── agent.go        //   agentBase, agentFetch, resolveNode, loadNodes
│   │   └── errors.go       //   WireError (unified error shape)
│   ├── wire/               // Request/response structs (mirrors src/wire/)
│   │   ├── hub.go
│   │   └── agent.go
│   ├── tailscale/          // status --json probe, suffix cache
│   │   └── tailscale.go
│   ├── nldate/             // Hand-rolled NL date parser (see 04-nl-dates.md)
│   │   ├── nldate.go
│   │   └── nldate_test.go
│   ├── card/               // AgentCard fetch + 24h cache
│   │   └── card.go
│   ├── manifest/           // AppManifest fetch + 24h cache
│   │   └── manifest.go
│   ├── apps/               // app registry (mirrors src/apps.ts)
│   │   └── apps.go
│   ├── db/                 // pgx Pool for admin commands
│   │   └── db.go
│   ├── update/             // self-update protocol (see 05-distribution.md)
│   │   └── update.go
│   └── cmd/                // one file per top-level command
│       ├── login.go
│       ├── whoami.go
│       ├── chat/
│       │   ├── chat.go     //   list/show/search/projects/nodes/models
│       │   ├── send.go     //   the WS streamer
│       │   └── mentions.go //   @<entity> parser
│       ├── project.go
│       ├── calendar.go
│       ├── tasks.go
│       ├── drive.go
│       ├── email.go
│       ├── stt.go
│       ├── tts.go
│       ├── v2.go           //   universal verbs: ask/find/do/<f> <a>
│       ├── cards.go
│       ├── manifest.go
│       └── admin/          //   sql/apps/app/health/errors/error
│           ├── admin.go
│           ├── sql.go
│           ├── apps.go
│           ├── health.go
│           └── errors.go
└── go.mod / go.sum
```

The `cmd/mm/main.go` wires Cobra commands; each verb's logic lives in `internal/cmd/<name>.go`. Cobra builds the command tree, we don't reinvent flag parsing.

---

## 3. Library choices

| Concern | Library | Why |
|---|---|---|
| CLI framework | `github.com/spf13/cobra` | The Go community default. Auto-generates help/completions/man pages. Mirrors the verb structure 1:1. |
| HTTP client | `net/http` stdlib | Zero deps. Wrap in our Client struct with default headers. |
| WebSocket | `github.com/coder/websocket` | The actively-maintained successor to nhooyr.io/websocket. Cleaner API than gorilla. ~zero deps. |
| Postgres | `github.com/jackc/pgx/v5` | Idiomatic Go, supports the same connection-string format as `postgres-js`. Pool config: `MaxConns: 2, MinConns: 0, IdleConnLifetime: 10s, ConnectTimeout: 5s`. |
| JSON | `encoding/json` stdlib | Native, fine for our payload sizes (manifest ~200KB max, fits comfortably). |
| Tailscale probe | `os/exec` | Same approach as TS — shell out to `tailscale status --json`. |
| Audio (TTS playback) | `os/exec` | Same — shell out to `afplay`/`aplay`. |
| MP3 conversion | `os/exec` | Shell out to `ffmpeg` (only when `--out *.mp3` or `--format mp3`). |
| Self-update | hand-rolled | ~50 LOC; see `05-distribution.md`. |
| Markdown rendering | none | Default output is plain text + markdown formatting in the shell. No need for a renderer. |
| Tabular output | hand-rolled | The TS uses `mdTable()` (49 LOC); port directly. No need for `tablewriter` or similar. |

**Total third-party deps: 3** (cobra, websocket, pgx). Plus their transitives — cobra pulls in `pflag` and `spf13/cast`; pgx is heavier but only on the admin path. Compiled binary size estimate: ~10-15 MB stripped.

**Anti-deps:** no `viper` (over-engineered for one config struct), no `logrus`/`zerolog` (our error path is `fmt.Fprintln(os.Stderr, err) + os.Exit(1)`), no `progressbar` libraries (the spinner in `mm login` is 4 lines of stdout writes), no testify (stdlib `testing` is fine).

---

## 4. The Client struct (mirrors src/http/client.ts)

```go
package http

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "time"
)

type Client struct {
    HTTP     *http.Client            // stdlib client; default Timeout: 30s
    Hub      string                  // from config
    LocalAgent string                // from config
    Apps     map[string]string       // slug → base URL
    Token    string                  // bearer; empty if not logged in
    UserID   string                  // for X-Hub-User-Id
}

// Hub posts to {Hub}/api/mm, unwraps `data`, throws on `errors`.
func (c *Client) Hub(ctx context.Context, feature, action string, payload any, out any) error { … }

// V2 posts to {app}/api/v2, returns raw envelope without unwrapping.
func (c *Client) V2(ctx context.Context, app, featureAction string, payload any, opts V2Opts) (V2Result, error) { … }

// Rpc posts to {app}/api/rpc, returns parsed JSON.
func (c *Client) Rpc(ctx context.Context, app, feature, action string, payload any, out any) error { … }

// AgentFetch GETs/POSTs to the local agent (or remote tailnet node).
func (c *Client) AgentFetch(ctx context.Context, node string, path string, init *AgentReq) (*http.Response, error) { … }

// AgentBase resolves to { HTTP, WS, DisplayName } for the targeted agent.
func (c *Client) AgentBase(ctx context.Context, node string) (AgentTarget, error) { … }

// LoadNodes hits instance.list, caches for process lifetime.
func (c *Client) LoadNodes(ctx context.Context) ([]wire.HubInstance, error) { … }
```

`out any` (Hub/Rpc) is a destination struct pointer (typical Go pattern: `var out wire.HubCalendarListResp; client.Hub(ctx, "calendar", "list", req, &out)`). Saves the caller a manual unmarshal.

---

## 5. Error model

One type, three sources:

```go
type WireError struct {
    Code     string  // from envelope.errors[0].code (where present)
    Title    string  // envelope.errors[0].title || HTTP status text
    Detail   string  // envelope.errors[0].detail || response body
    Status   int     // HTTP status
    URL      string  // for diagnostics
}

func (e *WireError) Error() string { … }  // prints Detail || Title || "<feature.action> failed (HTTP <Status>)"
```

Sources:

1. **Hub/v2 envelope errors** (`{errors: [...]}`) — populates Code/Title/Detail.
2. **HTTP errors** (4xx/5xx, no envelope) — Detail is the raw body, Title is `http.StatusText(Status)`.
3. **Network errors** (DNS, connection, timeout) — wrapped as `fmt.Errorf("fetch %s failed: %w", url, err)`.

Commands `fmt.Fprintln(os.Stderr, "❌ "+err.Error()); os.Exit(1)` — matches TS behaviour byte-for-byte (the `❌` prefix is preserved).

---

## 6. Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Any error (matches TS) |
| 2 | Reserved — Cobra uses this for "command not found" / arg parse failures by default |

We don't add granular codes (e.g. 3 for auth, 4 for network). Single non-zero is enough for shells.

---

## 7. stdout vs stderr

Match TS exactly:

- **stdout:** command output (lists, tables, JSON when `--json`).
- **stderr:** errors, warnings, progress (`mm login` spinner, `mm tts` "wrote N bytes").

Cobra's `Cmd.Println()` goes to stdout; `Cmd.PrintErrln()` to stderr. Use those, not `fmt.Println`.

---

## 8. Output formats

- **Default:** markdown tables (for admin commands), bullet lists (for chat/list-style verbs), plain key-value for single-record reads. Same shape as TS.
- **`--json`:** `json.MarshalIndent(out, "", "  ")` (2-space indent, matches TS).

The Go port reads `--json` from a Cobra persistent flag on the root command, accessible via `cmd.Root().Flags().GetBool("json")`.

---

## 9. Process lifecycle

- Boot: `Config.Load()` (reads env + `~/.mm/.env`), then build `http.Client` instance, then Cobra `rootCmd.Execute()`.
- Each command receives the shared `http.Client` (passed via `cmd.SetContext()` and unwrapped in `RunE` handlers).
- DB pool (admin only): lazy-init on first admin command, defer `pool.Close()` at the end of the process.
- Signal handling: `signal.NotifyContext(ctx, os.Interrupt)` so Ctrl-C cleanly cancels in-flight WS / SSE streams.

---

## 10. Tests

- `internal/nldate/` — fully unit-tested against the matrix in `04-nl-dates.md`.
- `internal/wire/` — golden JSON round-trip tests (marshal known shapes, compare to checked-in fixtures).
- `internal/http/` — `httptest.NewServer` per transport method, asserting headers + body shapes.
- `internal/cmd/` — minimal smoke tests using `cobra.Command.SetArgs() + Execute()` against the test server.
- **Integration:** a `tests/integration/` directory with shell scripts that run `mm whoami`, `mm chat nodes`, `mm calendar list --days 1` against the real hub and local agent. Run manually before each release. Not part of `go test ./...`.

Coverage goal: ≥80% on `internal/nldate/`, ≥60% on `internal/http/`, smoke-only elsewhere. We're shipping a CLI, not building a library — exhaustive tests are diminishing returns.

---

## 11. Versioning

`internal/version/version.go`:

```go
var (
    Version   = "dev"
    Commit    = "unknown"
    BuildDate = "unknown"
)
```

Set via `go build -ldflags "-X 'main.Version=v0.1.0' -X 'main.Commit=$(git rev-parse --short HEAD)' -X 'main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)'"` in the release script.

`mm --version` prints `mm vX.Y.Z (commit, builddate)`.

---

## 12. What we deliberately don't carry forward from TS

- **`mm v2 <app> <feature.action>`** alias. Already deprecated in TS, dropped in Go. Use `mm <app> <feature> <action>`.
- **`mm app <slug>` admin command** under the bare `app` name. Moves under `mm admin app <slug>` per `06-improvements.md` #8. Frees `app` for the universal-verbs path (today blocked by switch precedence).
- **Multiple flag-parser styles.** Cobra/pflag is the single source.
- **Inline duplicated HTTP clients.** Already collapsed in TS; Go starts clean.
- **`~/.mm-cli/manifests/` and `~/.mm-cli/cards/` cache locations.** Keep these for backward compat — existing caches stay valid across the TS→Go cutover.

---

## 13. What we add

- **`mm update`** — fetch and replace own binary. See `05-distribution.md`.
- **`mm version`** — explicit subcommand alongside `--version`. Mirrors `go version` muscle memory.
- **`mm completion <shell>`** — Cobra autogenerates bash/zsh/fish completions. Worth shipping.
- **Context cancellation everywhere.** TS doesn't have a `context.Context` analog — every API call in Go takes one, Ctrl-C cancels cleanly.
