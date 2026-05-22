# mm-cli — wire endpoint catalogue

> Every HTTP/WS endpoint mm-cli calls. Request shape, response shape, auth requirement, error envelope. The Go port either codegens structs from `src/wire/*.ts` or hand-translates this catalogue 1:1.

Pulled from `src/http/client.ts`, `src/api.ts`, `src/manifest.ts`, `src/agent-card.ts`, `src/wire/*.ts`, `src/commands/*.ts`.

---

## Transports

mm-cli speaks **three transports** plus the local-agent REST/WS:

| Transport | Helper | URL pattern | Auth |
|---|---|---|---|
| Hub mm-RPC | `hub(feature, action, payload)` | `POST {HUB}/api/mm` | Bearer + `X-Hub-User-Id` |
| App v2 (universal) | `v2(app, "feature.action", payload, opts)` | `POST {app.url}/api/v2` | Bearer + `X-Hub-User-Id` (+ `X-Hub-Instance-Id` if `opts.instanceId`) |
| App rpc (legacy kb+crm) | `rpc(app, feature, action, payload)` | `POST {app.url}/api/rpc` | Bearer + `X-Hub-User-Id` |
| Local agent REST | `agentFetch(node, path, init?)` | `{base}/{path}` | None — tailnet trust |
| Local agent WS | new `WebSocket("{base}/ws")` | tailnet trust |  None |

`{HUB}` = `MM_HUB_URL` || `https://meta-me.uk`. `{base}` = `MM_LOCAL_AGENT_URL` || `http://localhost:3142` (or `https://{bare}.{magicDNS-suffix}:31415` with `--node`).

---

## 1. Authentication (auth.meta-me.uk)

`{AUTH}` = `MM_AUTH_URL` || `https://auth.meta-me.uk`.

### 1.1 `POST {AUTH}/api/cli/device`

Initiate device-flow login. **No auth.**

- **Request:** empty body.
- **Response:**
  ```ts
  { device_code, user_code, verification_uri, verification_uri_complete, expires_in, interval }
  ```
- **Used by:** `mm login`.

### 1.2 `POST {AUTH}/api/cli/token`

Poll for token. **No auth.**

- **Request:** `{ device_code: string, client_name: string }`
- **Response on success:**
  ```ts
  { access_token: string, key: { id, name, prefix, scopes: string[] } }
  ```
- **Response while pending:** `{ error: "authorization_pending" }`. Status not 2xx.
- **Response on expiry:** `{ error: "expired_token" }`.
- **Used by:** `mm login` (poll loop).

### 1.3 `POST {AUTH}/api/cli/validate`

Validate a CLI bearer + enrich with user details. **No auth on the validate endpoint itself** — the token in the body *is* the auth material being validated.

- **Request:** `{ token: string }`
- **Response:** `{ user: { id, name, email, role }, key: { id, name, scopes: string[] } }` on success, non-ok response otherwise.
- **Used by:** `mm login` (post-token-exchange enrichment); also by KB/CRM internally when validating an incoming `Authorization: Bearer mm_…` header.

---

## 2. Hub mm-RPC (`{HUB}/api/mm`)

Single endpoint, body shape `{feature, action, payload}`. Response envelope `{data: T}` on success; `{errors: [{code, title?, detail?}]}` on failure.

Wire types: [src/wire/hub.ts](../../src/wire/hub.ts).

### 2.1 Calendar

| Endpoint | Req type | Resp type | Notes |
|---|---|---|---|
| `calendar.list` | `HubCalendarListReq` | `HubCalendarListResp` | Default 7-day window |
| `calendar.create` | `HubCalendarCreateReq` | `HubCalendarCreateResp` | NL date parsed locally before send |

### 2.2 Tasks

| Endpoint | Req type | Resp type | Notes |
|---|---|---|---|
| `tasks.list` | `HubTasksListReq` | `HubTasksListResp` | Groups by Google Tasks list |
| `tasks.add` | `HubTasksAddReq` | `HubTasksAddResp` | NL date parsed locally |
| `tasks.complete` | `HubTasksCompleteReq` | `HubTasksCompleteResp` (= `{ok: true}`) | |

### 2.3 Drive

| Endpoint | Req type | Resp type | Notes |
|---|---|---|---|
| `drive.list` | `HubDriveListReq` | `HubDriveListResp` | Drive query syntax in `q` |
| `drive.createDoc` | `HubDriveCreateDocReq` | `HubDriveCreateDocResp` | Server converts markdown → Doc |
| `drive.update` | `HubDriveUpdateReq` | `HubDriveUpdateResp` | Rename + parent shuffling |

### 2.4 Email — platform outbound log (admin)

| Endpoint | Req type | Resp type | Notes |
|---|---|---|---|
| `email.list` | `HubEmailListReq` | `HubEmailListResp` | Cursored, returns nextCursor |
| `email.get` | `HubEmailGetReq` | `HubEmailGetResp` | Includes body html+text |
| `email.create` | `HubEmailCreateReq` | `HubEmailCreateResp` | Draft row |
| `email.send` | `HubEmailSendReq` | `HubEmailSendResp` | Flips draft → sent |
| `email.resend` | `HubEmailResendReq` | `HubEmailResendResp` | Creates new row with `parent_id` |

### 2.5 Email — Gmail inbox (via gws-gateway)

| Endpoint | Req type | Resp type | Notes |
|---|---|---|---|
| `email.search` | `HubInboxSearchReq` | `HubInboxSearchResp` | Gmail query syntax |
| `email.read` | `HubInboxReadReq` | `HubInboxReadResp` | Full body + labels |

### 2.6 Instance discovery

| Endpoint | Req type | Resp type | Notes |
|---|---|---|---|
| `instance.list` | `HubInstanceListReq` | `HubInstanceListResp` | Used by `mm chat nodes` + `--node` resolution |

---

## 3. App `/api/v2` (universal verbs)

Per-app endpoint at `{app.url}/api/v2` (app.url from `src/apps.ts`).

Body shape: `{feature, action, payload}`. Response: arbitrary per-action (mm-cli returns `{ok, status, body}` without unwrapping — caller parses).

**Auth gap (load-bearing):** the CLI bearer only reaches `auth: "public"` actions today. `auth: "session"|"hub"|"either"` actions all 401. Tracked in `architecture.md` §4.7.

### 3.1 Manifest discovery (no auth)

- **`GET {app.url}/api/v2/manifest`** → `AppManifest` (`src/manifest.ts`).
  ```ts
  {
    appSlug: string,
    version: string,
    features: { [feature]: { [action]: { auth: "session"|"hub"|"either"|"public"|"install", description?, input, output } } }
  }
  ```
- Cached at `~/.mm-cli/manifests/<slug>.json`, 24h TTL, `--refresh` busts.
- Size: up to ~200 KB (gn has 999 actions). In-memory parse is fine.
- Used by: `mm manifest [<app>]`, `dispatch()` pre-validation.

### 3.2 Agent Card discovery (no auth)

- **`GET {app.url}/.well-known/agent.json`** → `AgentCard` (`src/agent-card.ts`).
  ```ts
  {
    name, description?, version?,
    capabilities?: ("ask"|"chat"|"search"|"writes")[],
    chatUrl?, mcpUrl?: string | null,
    tools?: { name, description?, readOnlyHint?, destructiveHint?, idempotentHint?, openWorldHint? }[],
    aliases?: { [verb]: { feature, action, description? } },
    auth?: string[]
  }
  ```
- Cached at `~/.mm-cli/cards/<slug>.json`, 24h TTL, `--refresh` busts.
- Used by: `mm cards [<app>]`, `mm <app> ask|find|do`.

### 3.3 Action dispatch

- **`POST {app.url}/api/v2`** with `{feature, action, payload}`.
- Response is per-action; not unwrapped. v2 callers parse their own shape.
- Used by: `mm v2 <app> <feature.action>`, `mm <app> ask|find|do|<feature> <action>`.

---

## 4. App `/api/rpc` (legacy — kb + crm only)

`POST {app.url}/api/rpc` with `{feature, action, payload}`. Response is parsed JSON, shape varies (most return `{data: ...}`, surface commands like `crm.surface` return `{meta: ..., data: ...}`).

The app's `handleBearerAuth` validates the bearer via `meta-me-auth/api/cli/validate` and uses *that* user. The `X-Hub-User-Id` header we send is ignored (defence-in-depth; not a vuln, but the header is dead weight).

### KB

Common verbs (full surface in `src/commands/kb.ts`):

| feature | action | payload | notes |
|---|---|---|---|
| `documents` | `searchCorpus` | `{ query }` | Semantic search |
| `documents` | `get` | `{ id, includeContent?: 'true' }` | Single doc |
| `collections` | `list` | — | Notebooks |
| `collections` | `get` | `{ name }` | Single notebook |
| `status` | `get` | — | KB health |
| `<any>` | `<any>` | `{ k=v key-value pairs }` | Pass-through with type coercion |

### CRM

| feature | action | payload | notes |
|---|---|---|---|
| `surface` | `list` | `{ limit? }` | Today's priorities |
| `tree` | `show` | — | Contact list |
| `project` | `list` | — | |
| `contact` | `context` | `{ target }` | Person context |
| `find` | `search` | `{ query }` | Cross-CRM search |
| `interaction` | `log` | `{ text }` | Log a meeting |
| `peek` | `show` | `{ target }` | Preview anything |
| `read` | `show` | `{ target }` | Full content |

---

## 5. Hub direct REST (non-mm-RPC)

### 5.1 `POST {HUB}/api/stt/transcribe`

- **Auth:** `Authorization: Bearer …`. No `X-Hub-User-Id` needed.
- **Request:** raw audio bytes (`Content-Type: application/octet-stream`).
- **Response:** `{ text: string, duration_s: number, infer_ms: number }`.
- **Used by:** `mm stt <file>`.

### 5.2 `POST {HUB}/api/tts/stream`

- **Auth:** `Authorization: Bearer …`.
- **Request:** `{ text: string, voice: string }` (defaults to `af_heart`).
- **Response:** SSE stream of events:
  - `{ type: "chunk", audio: <base64 PCM16 24kHz> }`
  - `{ type: "done" }` (terminal)
- **Used by:** `mm tts "<text>"`. Client concatenates audio chunks, wraps in 44-byte WAV header, optionally pipes through ffmpeg for mp3.

---

## 6. Hub Postgres direct (admin)

Not HTTP. `src/db.ts` uses `postgres-js` against `MM_DATABASE_URL` || `DATABASE_URL`. SSL: `rejectUnauthorized: false` for non-localhost.

Admin commands hitting the hub DB directly:

| Command | Query shape | Notes |
|---|---|---|
| `mm sql "<query>"` | arbitrary | Writes allowed |
| `mm apps` | `SELECT FROM app JOIN app_label ORDER BY sort_order` | List apps + labels + features |
| `mm app <slug> [enable|disable]` | `SELECT FROM app WHERE slug = $1` + optional UPDATE | |
| `mm health` | 6 parallel COUNTs (users, apps, errors24h, feedback, digest24h, last_digest) | |
| `mm errors` | Parameterised `SELECT FROM error WHERE last_seen > $since AND level = $level …` | |
| `mm error <fp> [<status>]` | `SELECT FROM error WHERE fingerprint LIKE $1 || '%'` + optional UPDATE | |

The Go port should keep these as `pgx` queries. Connection pooling: `max: 2, idle_timeout: 10, connect_timeout: 5` (same as the TS settings).

---

## 7. Local agent REST (`{base}/api/*`)

Default base: `http://localhost:3142`. With `--node <name>`: tailnet URL.

Wire types: [src/wire/agent.ts](../../src/wire/agent.ts). Reference source: `~/Documents/dev/meta-me-local-agent`.

### 7.1 Threads + messages

| Method + path | Req | Resp | Used by |
|---|---|---|---|
| `GET /api/threads?limit=N&project_id=<id-or-label>` | — | `AgentThreadsListResp` | `mm chat list`, prefix resolution |
| `POST /api/threads` | `{ title?, project_id? }` | `{ id }` | `mm chat send --new` |
| `GET /api/threads/:id/messages` | — | `AgentThreadMessagesResp` | `mm chat show` |
| `GET /api/messages/search?q=&limit=N` | — | `AgentMessageSearchResp` | `mm chat search` |
| `POST /api/threads/:id/project` | `{ project_id }` | `{ ok: true }` | (via WS send payload too) |
| `POST /api/threads/:id/model` | `{ provider, modelId }` | `{ ok: true }` | (via WS send payload too) |
| `POST /api/threads/:id/rename` | `{ title }` | `{ ok: true }` | (not yet exposed in mm-cli) |
| `POST /api/threads/:id/regenerate-title` | — | `{ title }` | (not yet exposed in mm-cli) |
| `POST /api/auto-title` | `{ userMessage, assistantMessage }` | `{ title }` | (not exposed in mm-cli) |

### 7.2 Projects

| Method + path | Req | Resp | Used by |
|---|---|---|---|
| `GET /api/projects` | — | `AgentProjectsListResp` | `mm chat projects`, `mm project list` |
| `POST /api/projects` | `{ root_path, label? }` | `AgentProject` | `mm project add` |
| `GET /api/projects/:id/overview?path=` | — | overview shape (see [src/index/](../../../meta-me-local-agent/src/index/)) | `mm project overview` |
| `GET /api/projects/:id/index?path=&deep=&search=&limit=&refresh=` | — | file-tier index shape | `mm project detail` |
| `POST /api/projects/:id/index/refresh` | `{ path? }` | `{ ok: true, refreshed: N }` | `mm project rebuild` |

### 7.3 Models + health + nodes

| Method + path | Req | Resp |
|---|---|---|
| `GET /api/models` | — | `AgentModelsListResp` |
| `GET /api/health` | — | `AgentHealthResp` |
| `GET /api/version` | — | `{ version: string }` |
| `POST /api/admin/restart` | — | (no response — process exits, service manager respawns) |

### 7.4 Auth providers (SPA-only, not mm-cli)

| Method + path | Notes |
|---|---|
| `GET /api/auth/providers` | List configured provider keys |
| `POST /api/auth/providers/:slug` | Set a provider key (`api_key`/`oauth`) |
| `DELETE /api/auth/providers/:slug` | Remove |

Currently called from chat.meta-me.uk SPA. mm-cli doesn't touch these today, but the Go port should keep them in mind for `mm agent set-key <provider>`.

### 7.5 Filesystem scope (SPA-only)

`GET /api/projects/:id/fs/{list,stat,read,write,readRaw}` + folder picker `/api/fs/browse`. Not used by mm-cli; consumed by the chat.meta-me.uk file tree.

## 8. Local agent WebSocket (`{base}/ws`)

Plain WS upgrade (no subprotocol). **Origin gate** (added 2026-05-22): rejects upgrade with 403 if `Origin` header is set and not in `ALLOWED_ORIGINS`. Absent Origin allowed (CLI clients don't send it).

### 8.1 Outbound (CLI → agent)

Wire types: `AgentWsOutbound` in [src/wire/agent.ts](../../src/wire/agent.ts).

```ts
| { type: "send", threadId, content, provider?, modelId?, projectId? }
| { type: "resume", threadId, cursor }
| { type: "ping" }
```

### 8.2 Inbound (agent → CLI)

```ts
| { type: "delta", threadId, cursor, text }
| { type: "thinking_delta", threadId, cursor, text }
| { type: "tool_start", threadId, cursor, toolName, args? }
| { type: "tool_end", threadId, cursor, toolName, result? }
| { type: "status", threadId, cursor, message }
| { type: "done", threadId, cursor, fullText?, fullThinking? }
| { type: "error", threadId, cursor, message }
| { type: "resume_empty", threadId }
```

### 8.3 Cursor + replay semantics

- Each turn writes to a per-thread `StreamBuffer` keyed by `threadId`. Every event has a monotonic `cursor`.
- On reconnect with `{type: "resume", threadId, cursor}`, the agent replays buffered events where `e.cursor > received` to that single socket.
- Buffer GCs 10 min after `done`.
- Broadcasts to dead sockets are caught and discarded.

The Go port's WS client should:
1. Open connection, immediately send `{type: "send", ...}`.
2. Read events; track `lastCursor` per thread.
3. On disconnect: re-open, send `{type: "resume", threadId, cursor: lastCursor}`. Continue stream.
4. Terminate on `done` or `error`.

---

## 9. URLs + env vars (single config struct)

Captured in `src/config.ts` `Config`:

```ts
interface Config {
  hubUrl: string;          // MM_HUB_URL || "https://meta-me.uk"
  authUrl: string;         // MM_AUTH_URL || "https://auth.meta-me.uk"
  localAgentUrl: string;   // MM_LOCAL_AGENT_URL || "http://localhost:3142"
  databaseUrl: string | undefined;   // MM_DATABASE_URL || DATABASE_URL
}
```

`~/.mm/.env` is merged into `process.env` at load time (explicit env wins).

The Go port's `Config` struct mirrors this 1:1.

---

## 10. Headers — every request mm-cli sends

| Header | Set on | Value |
|---|---|---|
| `Authorization` | Hub, App v2, App rpc, stt, tts | `Bearer ${auth.token}` |
| `X-Hub-User-Id` | Hub, App v2, App rpc | `${auth.userId}` |
| `X-Hub-Instance-Id` | App v2 (when `--instance` flag passed) | UUID |
| `Content-Type` | All POST/PUT | `application/json` (stt uses `application/octet-stream`) |
| `Accept` | Manifest + Card fetches | `application/json` |

The Go port's HTTP client embeds these defaults; per-request overrides for stt's binary upload.

---

## 11. Error envelope — every shape mm-cli might see

### Hub `/api/mm` and `/api/v2` envelope:

```ts
{ errors: [{ code: string, title?: string, detail?: string, status?: string }] }
```

mm-cli renders `detail || title || "<feature.action> failed (HTTP <status>)"`.

### Legacy `/api/rpc` (kb + crm):

No structured envelope on error. mm-cli throws on non-2xx with the raw response text (truncated to 200 chars).

### Auth flow `/api/cli/*`:

```ts
{ error: "authorization_pending"|"expired_token"|..., error_description?: string }
```

### Local agent:

Either a 403 `{ ok: false, error: "tcc_denied", tcc: {...} }` for FDA issues on macOS, or a 500 `{ ok: false, error: string }`. Other responses just text via `c.notFound()`.

The Go port should model errors as one `WireError` type that flattens these into `{Code, Title, Detail, HTTPStatus, Body}` and let the CLI print whichever fields are non-empty.
