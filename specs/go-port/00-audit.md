# mm-cli — TS surface audit

**Purpose.** Ground-truth inventory of the current TypeScript `mm-cli` before any Go port begins. Everything below is read from `src/` as of this commit, not from `specs/architecture.md` (which is partially stale — discrepancies are tracked in `06-improvements.md`).

**Status.** Authoritative as of 2026-05-22.

---

## 1. Distribution today

- Source: `~/Documents/dev/mm-cli/` (TS, 14 commands + 9 infra modules, ~5500 LOC total).
- Build: `bun build --compile --target=bun-<platform>` → single binary, ~104 MB (Bun runtime baked in).
- Just-added: `bun build --target=node --format=esm --outfile=dist/mm.mjs` → ~312 KB, runs under `node`.
- Distribution endpoint: nothing automated. Binaries built locally, copied to `~/.local/bin/mm`. There is no `chat.meta-me.uk/dist/mm/…` hosting yet.
- Hardware constraint of the Bun binary: requires SSE4.2 (2008+ Intel / all Apple Silicon). The Linux Mint MacBook Air (Core 2 Duo, 2008) cannot run it — `Illegal instruction`.

## 2. Entry point

`src/index.ts` (312 lines).

- Reads `process.argv.slice(2)`.
- Global flags it understands: `--help`/`-h`, `--version`/`-v`, `--json`, `--refresh`, `--no-validate`, `--instance <uuid>`.
- Global flags are stripped from positional args; per-command flags pass through.
- Command dispatch is a flat `switch` on `positional[0].toLowerCase()`. ~26 cases plus a fallthrough that routes any registered app slug (from `apps.ts`) through `appDispatch`.
- Error policy: catches the top-level promise, prints `❌ <message>`, `process.exit(1)`. Always runs `shutdownDb()` in a `finally`.

## 3. Auth state (on-disk + at-runtime)

- Storage: `~/.config/mm/auth.json` (mode `0o600`).
- Shape (`AuthState` in `src/auth.ts`):
  ```json
  {
    "token": "mm_<40 hex>",
    "prefix": "mm_xxxxx",
    "userId": "<uuid>",
    "userName": "Jane Doe",
    "userEmail": "jane.doe@example.com",
    "createdAt": "2026-05-14T10:54:00.000Z"
  }
  ```
- `loadAuth()` returns `AuthState | null` (silently nulls on parse error).
- `logout` writes `{"loggedOut": true}` over the file (not a delete — preserves the directory + perms).
- **No HMAC.** All authenticated requests use `Authorization: Bearer <auth.token>` + `X-Hub-User-Id: <auth.userId>`. The hub validates the bearer against the platform auth service. The HMAC-signed `auth: hub` mode on apps (per cross-app spec) is unreachable from the CLI — see §4.7 in `architecture.md` (still valid).

## 4. Authentication flow (`mm login`)

`src/commands/login.ts` + `src/api.ts`.

OAuth 2.0 device flow. Two endpoints on `auth.meta-me.uk` (not `meta-me.uk`):

1. `POST https://auth.meta-me.uk/api/cli/device` → `{device_code, user_code, verification_uri, verification_uri_complete, expires_in, interval}`.
2. Browser opened (best-effort via `open`/`xdg-open`/`start`) to `verification_uri_complete`.
3. Poll `POST https://auth.meta-me.uk/api/cli/token {device_code, client_name}` every `interval` seconds.
   - `authorization_pending` → continue polling.
   - `expired_token` → error, exit 1.
   - Success → `{access_token, key: {id, name, prefix, scopes[]}}`.
4. `POST https://auth.meta-me.uk/api/cli/validate {token}` → `{user: {id, name, email, role}, key: {id, name, scopes[]}}`. Used to enrich the auth.json with user details.
5. Persist `AuthState`.

Notable: the polling UX writes a spinning dot animation to a single line via `\r`. Not blocking for the port — informational.

## 5. Wire surfaces (where every command goes)

Six distinct surfaces. Most CLI commands hit exactly one.

### 5.1 Hub mm-RPC (`POST https://meta-me.uk/api/mm`)
Body: `{feature, action, payload}`. Headers: `Authorization: Bearer …`, `X-Hub-User-Id: …`, `Content-Type: application/json`.
Response envelope: `{data: T}` on success, `{errors: [{code, title?, detail?}]}` on failure.

Wrapped by `src/hub.ts` `hubApi<T>(feature, action, payload)`.

Commands using this:
- `mm calendar list/new` → `calendar.list`, `calendar.create`
- `mm tasks list/add/done` → `tasks.list`, `tasks.add`, `tasks.complete`
- `mm drive ls/doc/mv` → `drive.list`, `drive.createDoc`, `drive.update`
- `mm email list/get/send/draft/resend` → `email.list`, `email.get`, `email.create`, `email.send`, `email.resend`
- `mm email search/read` (Gmail inbox) → `email.search`, `email.read`
- *(re-implementation note: `email.ts` re-implements `hubApi` inline rather than importing from `hub.ts`. Same shape, duplicate code — flag for cleanup in 06-improvements.)*

### 5.2 App `/api/v2` (universal dispatcher)
Body: `{feature, action, payload}`. Headers: `Authorization: Bearer …`, `X-Hub-User-Id: …`, optionally `X-Hub-Instance-Id: …`.

Wrapped by `src/dispatcher.ts` `dispatch(appSlug, "feature.action", payload, opts)`. Pre-validates against the cached manifest unless `--no-validate`.

Commands using this:
- `mm v2 <app> <feature.action>` (deprecated alias for the verbose form)
- `mm <app> ask "..."` → `dispatch(app, "agent.chat", {question})`
- `mm <app> find "..."` → `dispatch(app, "agent.search", {query})`
- `mm <app> do <tool> [k=v]` → resolves via card.tools, dispatches
- `mm <app> <feature> <action>` → raw fallback

Currently blocked at runtime for any action requiring `auth: hub|session|either` — see §4.7 of architecture.md (still accurate). Only `agent.card` (auth: public) works.

### 5.3 App `/api/rpc` (legacy per-app, kb + crm only)
Body: `{feature, action, payload}`. Headers: `Authorization: Bearer …`, `X-Hub-User-Id: …`.

Each app has its own client implementation (no shared helper). Same body shape as `/api/v2` but different path. Pre-dates the universal dispatcher.

Commands using this:
- `mm kb find/tree/peek/read/collections/status/<feature> <action>` → `kbApi(...)` in `commands/kb.ts`
- `mm crm surface/contacts/projects/log/context/peek/read/find/rpc` → `crmApi(...)` in `commands/crm.ts`

### 5.4 Hub direct REST (non-mm-RPC)
Specific binary endpoints on `meta-me.uk` outside the `/api/mm` dispatcher:

- `POST https://meta-me.uk/api/stt/transcribe` — `Authorization: Bearer …`, `Content-Type: application/octet-stream`, audio bytes in body. Response: `{text, duration_s, infer_ms}`. (`mm stt`)
- `POST https://meta-me.uk/api/tts/stream` — `Authorization: Bearer …`, JSON body `{text, voice}`. Response: SSE stream of `{type: "chunk", audio: <base64 PCM16 24kHz>}` events + a `{type: "done"}` terminator. (`mm tts`)
- `MM_HUB_URL` env var overrides `https://meta-me.uk` for both stt+tts.

### 5.5 Hub Postgres (direct DB)
`src/db.ts` opens a `postgres-js` connection to `MM_DATABASE_URL` / `DATABASE_URL`, sourced from process env or `~/.mm/.env`. SSL: `rejectUnauthorized: false` for non-localhost, `false` for localhost.

Commands using this:
- `mm sql "<query>"` — arbitrary SQL via `sql.unsafe(query)`. Writes allowed.
- `mm apps` — `SELECT … FROM app, app_label ORDER BY sort_order`.
- `mm app <slug> [enable|disable]` — read `app` row + `app_label` + count of `user_app`; optional `UPDATE app SET enabled=…`.
- `mm health` — six parallel `COUNT(*)` queries (users, apps, errors, feedback, digest).
- `mm errors` — parameterised `SELECT … FROM error WHERE last_seen > $since AND level=$level …`.
- `mm error <fingerprint> [<status>]` — read by fingerprint LIKE prefix; optional UPDATE with note/priority/log_quality.

All of these are admin-only by environment: no DB URL on a normal user's machine = clean exit-1 with explanatory message.

### 5.6 App agent-card + manifest (public, unauthenticated)
- `GET <app-url>/.well-known/agent.json` → `AgentCard` (name, description, capabilities, tools, aliases, mcpUrl, chatUrl). Cached at `~/.mm-cli/cards/<slug>.json`, 24h TTL.
- `GET <app-url>/api/v2/manifest` → `AppManifest` (appSlug, version, features: {feature: {action: {auth, description, input, output}}}). Cached at `~/.mm-cli/manifests/<slug>.json`, 24h TTL.

Commands:
- `mm cards [<app>] [--refresh]` — capability matrix or per-app card render.
- `mm manifest [<app>] [--refresh]` — wire-level surface.

Cache-bust: `--refresh` or 24h TTL.

### 5.7 Local agent REST + WebSocket (chat + project)
Base URL: `http://localhost:3142` (override with `MM_LOCAL_AGENT_URL`).
WS: `ws://localhost:3142/ws`.
With `--node <name>`: resolves to `https://<bare-host>.<MagicDNS-suffix>:31415` via hub `instance.list` + `tailscale status --json`. WS upgrades to `wss://`.

Commands using REST:
- `mm chat list/show/search/projects/nodes/models` — all `GET /api/threads`, `/api/threads/:id/messages`, `/api/messages/search`, `/api/projects`, `/api/models`. (Just refactored — no longer reads `~/.mm/meta-me-local-agent.db` directly.)
- `mm chat send "<msg>"` — `POST /api/threads` (if `--new`), then WebSocket `{type: 'send', threadId, content, modelId?, provider?, projectId?}`.
- `mm project list/overview/detail/add/rebuild` — `GET /api/projects`, `GET /api/projects/:id/overview`, `GET /api/projects/:id/index`, `POST /api/projects`, `POST /api/projects/:id/index/refresh`.

Commands using WS:
- `mm chat send` — listens for `delta`, `tool_start`, `tool_end`, `thinking_delta`, `status`, `done` (with `fullText`), `error` events.

Commands using the hub (not the agent):
- `mm chat nodes` — `instance.list` against the hub, then renders the merged list. No tailscale needed.

## 6. Command surface table

Compact map: command, subcommand, surface, special.

| Command | Subcommand | Surface | Notes |
|---|---|---|---|
| `login`        | — | auth.meta-me.uk device flow | Browser open via `open`/`xdg-open`/`start` |
| `logout`       | — | local file | Writes `{loggedOut: true}` |
| `whoami`       | — | local file | Reads auth.json |
| `status`       | — | local file + hub | See §7.2 |
| `kb`           | find / tree / peek / read / collections / status / `<feature> <action>` | App `/api/rpc` | Legacy per-app |
| `crm`          | surface / contacts / projects / log / context / peek / read / find / rpc | App `/api/rpc` | Legacy per-app |
| `chat`         | list / show / search / projects / send / nodes / models | Agent REST + WS, Hub instance.list | Just refactored to pure HTTP |
| `project`      | list / overview / detail / add / rebuild | Agent REST | |
| `email`        | list / get / send / draft / resend / search / read | Hub mm-RPC | search/read = Gmail inbox; list/get/send/draft/resend = admin platform log |
| `calendar`     | list / new | Hub mm-RPC | chrono-node for `--when` |
| `tasks`        | list / add / done | Hub mm-RPC | chrono-node for `--due` |
| `drive`        | ls / doc / mv | Hub mm-RPC | reads stdin for `doc` |
| `stt`          | (positional file) | `meta-me.uk/api/stt/transcribe` | binary upload, JSON response |
| `tts`          | (positional text) | `meta-me.uk/api/tts/stream` | SSE; ffmpeg shell-out for mp3; afplay/aplay for `--play` |
| `v2`           | `<app> <feature.action>` | App `/api/v2` | Deprecated alias for `mm <app> <f> <a>` |
| `manifest`     | `[<app>]` | App `/api/v2/manifest` | Cached `~/.mm-cli/manifests/` |
| `cards` / `card` | `[<app>]` | App `/.well-known/agent.json` | Cached `~/.mm-cli/cards/` |
| `sql`          | `"<query>"` | Postgres direct | Admin only; env var-driven |
| `apps`         | — | Postgres direct | Admin only |
| `app`          | `<slug> [enable|disable]` | Postgres direct | Admin only |
| `health`       | — | Postgres direct | Admin only |
| `errors`       | `[--since --limit --status --app --level --priority]` | Postgres direct | Admin only |
| `error`        | `<fingerprint> [<status>] [--note --priority --log-quality]` | Postgres direct | Admin only |
| `<app>`        | (any registered app slug → universal verbs) | App `/api/v2` | `ask`/`find`/`do`/`<f> <a>` |

## 7. Cross-cutting behaviour

### 7.1 Flag parsing — ad hoc, inconsistent

There is **no shared flag parser.** Every command file implements its own. Three observable patterns:

1. **`commands/calendar.ts`-style** — `parseFlags(args)` returns `Record<string,string>`, accepting `--key=value` and `--key value`. Bare booleans not supported.
2. **`commands/tasks.ts`-style** — `parseFlags(args)` returns `{flags, positional}`, supports bare booleans (`flag set to "true"` if next arg starts `--` or doesn't exist). Same in `drive.ts`.
3. **`commands/chat.ts`-style** — `getFlag(args, name)` per-flag lookup, plus `hasFlag(args, name)` for booleans. Hand-rolled.
4. **`commands/hub.ts`-style** — `parseFreeformFlags(args)` returns `{positional, flags}`, accepts both forms.

Plus there are `k=v` positional argument styles in `kb.ts pass-through` and `crm.ts rpc`. Same data, different syntax.

**Implication for Go port.** Pick one (probably `pflag`/`cobra`) and standardise. Most commands already implement the same logic four different ways — converging is a win, not a chore.

### 7.2 `mm status`

`src/commands/status.ts` (30 lines, not yet read by me). Almost certainly: print auth state + ping hub for capability check. Will read before architecture spec.

### 7.3 `--json` output

Every command has a `--json` branch that emits `JSON.stringify(data, null, 2)`. Always 2-space indented. Some commands emit a wrapping object (`mm chat list --json`), others emit a bare array.

### 7.4 stdout vs stderr

Help text → stdout. Errors → stderr (`process.stderr.write` or `console.error`). Tabular/markdown output → stdout. Generally consistent.

### 7.5 Exit codes

`0` on success, `1` on any error. No granular codes (no "auth failed = 2", "network = 3"). Probably fine for a CLI of this size.

## 8. External dependencies

### Userland (npm)
- `chrono-node` (^2.9.1) — natural language date parsing. **Used only by `src/nl-date.ts`, consumed by calendar + tasks.** No Go peer.
- `postgres` (^3.4.9) — Postgres driver for hub admin commands. Go equivalent: `jackc/pgx` (excellent).

### Node built-ins
- `node:fs`, `node:fs/promises`, `node:os`, `node:path`, `node:child_process`, `node:crypto` (transitive). All trivially mapped to Go stdlib (`os`, `path/filepath`, `os/exec`, `crypto/*`).

### Shelled-out binaries
- `tailscale` (or absolute path probe — see §9) for `tailscale status --json` (chat `--node` resolution).
- `ffmpeg` for wav→mp3 conversion in `mm tts --out *.mp3`.
- `afplay` (macOS) / `aplay` (Linux) for `mm tts --play`.
- `open` (macOS) / `xdg-open` (Linux) / `start` (Windows) for `mm login` browser open.

**No** other binaries shelled. No `git`, no `bash`, no shell interpolation outside the above. Good.

### External services (URLs hardcoded or env-overridable)
- `https://auth.meta-me.uk` — device-flow OAuth (hardcoded).
- `https://meta-me.uk` — hub (hardcoded; `MM_HUB_URL` overrides for stt+tts only — inconsistent).
- `https://kb.meta-me.uk` — KB (hardcoded).
- `https://crm.meta-me.uk` — CRM (hardcoded).
- `https://finances.meta-me.uk`, `https://grounded.ninja`, `https://analytics.meta-me.uk` — registered apps (in `apps.ts`).
- `http://localhost:3142` — local agent (default; `MM_LOCAL_AGENT_URL` overrides).
- `<node>.<magicDNS suffix>:31415` — remote agent via tailnet.

**Improvement opportunity:** unify the override env vars (`MM_HUB_URL`, `MM_LOCAL_AGENT_URL`, `MM_DATABASE_URL`) into a single config. Currently they're inconsistent in adoption.

## 9. Tailscale integration

`src/tailscale.ts`:

- Locates the `tailscale` binary via a fallback path list (handles macOS App Store install which doesn't put it on PATH).
- Runs `tailscale status --json`, parses MagicDNSSuffix (or derives from `Self.DNSName`).
- Caches both for the process lifetime.

The MagicDNS suffix lookup is critical: `app_instance.url` rows in the hub may carry stale suffixes (e.g., `taildd974e` while the current suffix is `tail69dfd7`); reconstructing as `<bare-host>.<current-suffix>:<port>` survives suffix rotation without DB writes.

Used only by `mm chat <verb> --node <name>`.

## 10. On-disk artifacts the CLI creates/touches

| Path | Purpose | Lifetime |
|---|---|---|
| `~/.config/mm/auth.json` | Bearer token + user details | Persistent across login/logout |
| `~/.mm-cli/cards/<slug>.json` | Agent Card cache | 24h TTL |
| `~/.mm-cli/manifests/<slug>.json` | App manifest cache (can be ~200KB for apps like gn with 999 actions; in-memory parse is fine, no streaming needed) | 24h TTL |
| `~/.mm/.env` | Read-only: env vars for hub DB connection (admin) | n/a |

That's the full footprint. No telemetry, no temp dirs (except `mm tts --play` which uses `mkdtemp`).

### Install symlinks (REQUIRED — both, not one)

The local agent's bash tool runs with `PATH=~/.mm/pi-agent/bin:/usr/bin:/bin:/usr/sbin:/sbin`. `~/.local/bin` is *not* on that PATH. Installing the binary at only `~/.local/bin/mm` means the agent can't find it — it burns an LLM turn doing `which mm` on every chat that needs the CLI.

The install must drop **both**:

- `~/.local/bin/mm` — for the user's shell.
- `~/.mm/pi-agent/bin/mm` — for the local agent's bash tool.

Either can be a symlink to a single canonical binary location (e.g. `~/.mm/bin/mm`). Verified working end-to-end this session.

## 11. Env vars consumed

| Var | Used by | Purpose |
|---|---|---|
| `MM_HUB_URL` | stt, tts | Override hub base URL (inconsistent — most commands hardcode `https://meta-me.uk`) |
| `MM_LOCAL_AGENT_URL` | chat | Override local agent base URL |
| `MM_DATABASE_URL` | hub admin commands | Postgres connection string |
| `DATABASE_URL` | hub admin commands | Postgres fallback |

`~/.mm/.env` is read by `src/db.ts` `maybeLoadUserEnv()` to populate the above from a file if the shell env is bare.

## 12. Code quality notes

Things that will become decision points in the Go port:

1. **No tests.** Zero. Any rewrite needs at least integration tests against the live hub + a mock agent.
2. **`commands/email.ts` duplicates `hubApi()`** — same code as `src/hub.ts` (lines 13-41 of email.ts ≡ src/hub.ts in shape). Removing the duplicate in TS is a 10-line refactor; in Go, do it once.
3. **`commands/kb.ts` and `commands/crm.ts` use `/api/rpc`**, not the new `/api/v2` dispatcher. Both could be `dispatch()` calls today (per `architecture.md` §4.4 — still pending). Go port should pick one path; recommend `/api/v2` because (a) it's the contract direction the platform is heading, (b) it lets us drop the per-app HTTP client code entirely.
4. **`mm app` collision** (admin Postgres-backed `mm app <slug>`) vs. **`mm <app>` fallthrough** (universal verbs for any registered slug). Currently disambiguated by hard-coded `case 'app':` precedence in `index.ts` — admin wins. If a user types `mm app whatever` and `app` is a registered slug, admin still wins. The audit confirms it's a fragile coexistence; will recommend `mm admin app <slug>` namespacing in 06-improvements.
5. **No request retries.** Network errors fail fast with exit 1. Probably correct for a CLI, but worth a config knob for `MM_RETRY=N`.
6. **No structured logging.** `console.error` for errors, `console.log` for everything else. Fine for v1.

## 13. Operations not covered by this audit

These exist in scope but I haven't fully read them — they're small and don't change the wire surface:

- `src/commands/status.ts` (30 lines) — read before architecture spec
- `src/commands/v2.ts` (87 lines) — generic dispatcher wrapper; same surface as `mm <app> <f> <a>` per index.ts
- `src/commands/manifest.ts` (55 lines) — thin wrapper around `manifest.ts` infra
- `src/commands/cards.ts` (105 lines) — thin wrapper around `agent-card.ts` infra
- `src/commands/app.ts` (283 lines) — universal-verb dispatcher; uses `dispatcher.ts` + `agent-card.ts` + `manifest.ts`. Worth re-reading at architecture spec time.
- `src/commands/project.ts` (560 lines) — already known to be pure HTTP against the agent; only flag-parsing changes for the Go port.

## 14. Things the Go port must not break

Working backward from "what would Joe notice":

- `mm login` keeps existing `~/.config/mm/auth.json` (same shape). Existing tokens stay valid.
- `mm chat send --node "MacBook Air" "..."` keeps working end-to-end (tailnet resolution + WS streaming).
- `mm sql "SELECT …"` keeps working with `MM_DATABASE_URL`.
- `mm calendar new --when "tomorrow 14:00"` keeps parsing natural language (Go-side NL parser must cover at least: relative days, weekday names, "in N hours", ISO).
- `--json` output keeps the same JSON shapes everywhere (don't refactor field names during the port).
- Exit codes (0 success, 1 failure) stay the same.

## 15. Discrepancies with `specs/architecture.md` (feed into `06-improvements.md`)

- **architecture.md §4.5** says `mm chat` reads `~/.mm/meta-me-local-agent.db` directly. **No longer true** — refactored 2026-05-22 to pure HTTP.
- **architecture.md §1** says `mm` is "Bun `--compile`". **Still true today but about to change** — node-bundle target landed 2026-05-22, Go port is the target after that.
- **architecture.md §4.4** says "Delete `mm v2 …` command file once raw fallback is wired". **Still present** — `commands/v2.ts` and `case 'v2':` both alive in `index.ts`.
- **architecture.md §4.1** "Add Agent Card fetching" marked done. Verified — `agent-card.ts` + `commands/cards.ts` both present.
- **architecture.md §7** "README.md + CLAUDE.md" marked done. Verified — both files exist.
- **architecture.md §4.6** "Aliases in Cards (cross-repo)" — pending in spec, no aliases honoured in current code path. `findAlias()` exists in `agent-card.ts` but isn't called from `app.ts`. Possibly dead code — verify before deleting.

---

## Next docs

- `01-wire.md` — every endpoint shape, response shape, error shape (the codegen target if we go that way).
- `02-auth.md` — device flow timing diagram, AuthState contract.
- `03-architecture.md` — Go package layout.
- `04-nl-dates.md` — chrono replacement.
- `05-distribution.md` — release pipeline + self-update.
- `06-improvements.md` — the cleanups this audit surfaced.
- `07-roadmap.md` — phased delivery + cutover.
