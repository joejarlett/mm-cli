# mm CLI alignment

Living spec for aligning `mm` with the meta-me cross-app contract so every app on the platform is reachable through a small, agent-intuitive verb set.

> ⚠️ Historical: sections that reference `src/*.ts` (`src/apps.ts`, `src/commands/*.ts`, `src/index.ts`, …) describe the **original TypeScript CLI, deleted 2026-06-11**. The live implementation is Go in `internal/cmd/*.go`. The *design* still holds; only those file paths are stale.

**Legend:** `[x]` shipped, `[~]` in progress, `[ ]` pending.

---

## 1. Context

### What `mm` is

A single compiled binary (`~/.local/bin/mm`, Bun `--compile`). One file to distribute. One `mm login` (browser OAuth flow, token at `~/.config/mm/auth.json`). One bearer token serves every app.

```
mm <app> <command> [args...]
```

The binary is the **distribution unit**. It stays thin — auth, discovery, dispatch. Per-app help, intent verbs, and aliases live with the apps themselves and travel via their published contract.

### The cross-app contract (canonical, on the server side)

Spec lives at [meta-me.uk/specs/cross-app-communication.md](../../meta-me.uk/specs/cross-app-communication.md). Every app onboarded to the platform exposes:

- `POST /api/v2` with `{feature, action, payload}` envelope.
- `GET /api/v2/manifest` — full surface (feature → action → schema, auth mode).
- `GET /.well-known/agent.json` — the **A2A Agent Card**: description, `capabilities`, curated `tools` with MCP annotations (`readOnlyHint`, `destructiveHint`).
- Reserved feature `agent` with at minimum `agent.chat` (Gemini mini-agent, returns `{intent, entities, writes[], markdown_snapshot}`) and ideally `agent.search` (deterministic entity lookup).
- Optional: `/mcp` streamable HTTP for external MCP clients.
- Auth: `Authorization: Bearer <mm_token>` (+ HMAC + `X-Hub-User-Id`, `X-Hub-Instance-Id` where relevant).

Shared SDK [`@meta-me/app-agent`](../../meta-me-app-agent/) packages this boilerplate. Every new app gets the full contract by adopting the SDK.

### What `mm` does NOT speak directly

Not everything `mm` exposes is a v2-contract app:

- `mm calendar`, `mm tasks`, `mm drive`, `mm email` (send) — go through the hub (`meta-me.uk/api/mm`) which proxies to the `gws-gateway` container for Google Workspace.
- `mm email` (admin list) — platform mail log on the hub.
- `mm chat` — reads/writes against the **local** `meta-me-local-agent` daemon (tailnet, SQLite). Not a v2 contract; its own REST + WS surface.
- `mm hub` (sql/apps/health/errors) — direct admin DB access on the hub.
- `mm stt`, `mm tts` — audio in/out via the hub.

These are built-ins. The alignment work below is about the apps that **are** on the v2 contract.

---

## 2. Current contract coverage (probed 2026-05-20)

| App | Card | Manifest | `agent.chat` | `agent.search` | `/mcp` | Card tools | Notes |
|---|---|---|---|---|---|---|---|
| `kb` | ✅ | 64 actions / 10 features | ✅ | ✅ | 303 | 3 | Full citizen. Still hit by `mm kb` over `/api/rpc` |
| `crm` | ✅ | 66 / 24 | ✅ | ✅ | 303 | 27 | Card `mcpUrl: null` despite 27 tools — declared not served |
| `finances` | ✅ | 6 / 3 | ✅ | ❌ | 200 | 0 | Server-opaque per spec Wave 6.2 |
| `gn` | ✅ | **999** / 30+ | ✅ | ❌ | 404 | 0 | APIRouter; Card under-advertises caps (no `search`/`writes` despite supporting both) |
| `analytics` | ✅ | 1 (`agent.chat`) | ✅ | ❌ | 200 | 0 | Smallest contract |
| ~~`pi`~~ | — | — | — | — | — | — | **Remove from registry** — pi-ui is being deprecated. mm-local-agent replaces it as a built-in (`mm chat`), not as a v2 app |

---

## 3. The target verb set

Three universal verbs per app, plus an escape hatch and a top-level federator:

```
mm <app>                         → render Agent Card (description + capabilities + tools)
mm <app> ask "..."               → POST agent.chat ; print markdown_snapshot
mm <app> find "..." [--type T]   → POST agent.search ; tabular results  (gated on caps.search)
mm <app> do <tool-name> [k=v…]   → invoke a Card-declared tool (typed write)
mm <app> <feature> <action> …    → raw escape hatch (replaces today's `mm v2 …`)

mm ask "..."                     → hub meta-agent (cross-app fan-out)
mm find "..." [--app a,b]        → parallel agent.search across capable apps

mm apps                          → list registered apps with capability badges
mm cards                         → fetch + show all live Agent Cards
```

**Why this is intuitive for an agent:**

- The verbs match the contract's mental model (`ask`/`find`/`do`).
- Capability-gated: missing `agent.search` → `mm <app> find` errors clearly and suggests `ask`.
- Tool names come from each Card's `tools[]` — the names the app's author considered worth exposing.
- One transport (`/api/v2`), one auth path. The "v2" name disappears from the user surface entirely.

---

## 4. Workstream

### 4.1 Registry & Card discovery

- [x] **Remove `pi` from [apps.ts](../src/apps.ts).** pi-ui is being deprecated.
- [x] **Add Agent Card fetching alongside manifest fetching.** [src/agent-card.ts](../src/agent-card.ts) — `loadAgentCard(slug)`, cache at `~/.mm-cli/cards/<slug>.json`, 24h TTL, `--refresh` bust.
- [x] **`mm cards` and `mm cards <app>`** — [src/commands/cards.ts](../src/commands/cards.ts). Capability matrix across all apps; full Card per app. `mm manifest` retained as the deeper wire-level view.
- [ ] **`mm apps`** — short capability matrix. **Deferred**: name collision with the hub-admin `mm apps` (Postgres-backed). Needs a small refactor moving admin verbs under an `mm admin …` namespace before this slot is free. `mm cards` covers the capability-matrix need in the meantime.

### 4.2 Universal verbs

- [x] **`mm <app>` (no args)** — renders the Card via `cardsDispatch`. Replaces `print<App>Help` for every registered app that doesn't have a hand-coded wrapper.
- [x] **`mm <app> ask "..."`** — `dispatch(app, 'agent.chat', {question})`. Prints `markdown_snapshot` + writes summary; `--json` for full envelope. **Blocked at runtime** by §4.7 auth gap (every app marks `agent.chat` as `auth: "hub"`).
- [x] **`mm <app> find "..."`** — `dispatch(app, 'agent.search', {query, limit?, types?})`. Tabular output. Capability-gated on `card.capabilities.includes('search')` — `mm analytics find` errors with a helpful pointer to `ask`. Same `auth: "hub"` block.
- [x] **`mm <app> do <tool> [k=v…]`** — resolves `<tool>` against `card.tools[]`, strips the `<app>.` prefix, dispatches.
- [x] **`mm <app> <feature> <action> [k=v…]`** — raw fallback, pre-validates against the manifest with precise error messages (`Unknown action 'bogus' on finances.transactions. Known: categorise, hmrcClassify`).

Implementation: [src/commands/app.ts](../src/commands/app.ts). Wired into [src/index.ts](../src/index.ts) as the generic fallthrough for any registered app slug that doesn't have an explicit case (kb/crm wrappers still take precedence).

### 4.3 Top-level federation

- [ ] **`mm ask "..."`** — Single POST to a hub endpoint that runs the meta-agent. **Blocked**: hub doesn't expose `agent.chat` at `/api/v2` today — only via `/api/chat` SSE bound to conversation persistence. Needs a new hub feature handler (e.g. `meta-me.uk/api/mm` `agent.chat`) or a v2 contract endpoint.
- [ ] **`mm find "..." [--app …]`** — Parallel `agent.search` across capable apps. Blocked by the same §4.7 auth gap.

### 4.4 Deprecate per-app wrappers

- [ ] **KB:** migrate `kbApi()` → `dispatch()`. Drop the `/api/rpc` codepath. Hand-coded verbs (`find`, `tree`, `peek`, `read`, `collections`) become aliases that call the universal verbs. Delete dead code.
- [ ] **CRM:** same — `crmApi()` → `dispatch()`. Map existing hand-coded verbs to universal verbs / Card tools.
- [ ] **Delete `mm v2 …` command file** once the raw fallback is wired. Keep as hidden alias for one release.

### 4.5 mm-local-agent integration (separate from v2 contract)

- [ ] **Design pass on `mm chat`** — today it reads the local agent's SQLite directly. It should also drive the agent over HTTP/WS:
  - `mm chat` → list threads (DB or `GET /api/threads`)
  - `mm chat new "..."` → `POST /api/threads` + first send via WS
  - `mm chat send <id> "..."` → WS `{type:'send', threadId, content}`
  - `mm chat tail <id>` → WS subscribe, stream deltas to stdout
- [ ] **Config knob `MM_LOCAL_AGENT_URL`** — default tailnet host, override for non-tailnet use.

### 4.6 Aliases in Cards (cross-repo, needs `@meta-me/app-agent` change)

Long-term, the only per-app code in `mm` should be the registry. Aliases (`find → agent.search`, `tree → collections.list`) belong in each app's Card:

```json
{
  "aliases": {
    "find":    { "feature": "documents", "action": "searchCorpus", "description": "Semantic search" },
    "tree":    { "feature": "collections", "action": "list", "description": "List notebooks" }
  }
}
```

- [ ] **Add `aliases` to `generateAgentCard()`** in `@meta-me/app-agent`.
- [ ] **Consume aliases in mm-cli** — `mm <app> <alias>` resolves through the Card before falling through to feature/action lookup.

### 4.7 Server-side gaps to flag

Not mm-cli's fix, but tracked so agents touching those repos know:

- [ ] **CLI bearer can't reach `auth: "session"|"either"|"hub"` actions on `/api/v2`.** **This is the load-bearing platform blocker.** Smoke-tested 2026-05-20:
  - `mm finances user me` → 401 `'user.me' requires 'session' auth`
  - `mm finances ask "..."` → 401 `'agent.chat' requires 'hub' auth`
  - `mm gn list list` → 401 `'list.list' requires 'either' auth`

  The cross-app spec's `verifyHubRequest` ([@meta-me/app-agent](../../meta-me-app-agent/)) accepts session cookies and HMAC-signed hub forwards — both originate from the hub itself. The CLI's `mm_…` bearer is validated against `auth.account` via `meta-me.uk/api/cli/validate`, but the SDK's v2 contract has no path that accepts it.

  Two viable fixes (need one):
  - **(a) Hub-side dispatch bridge.** Add `meta-me.uk/api/mm` feature `dispatch.run` (or expose `/api/v2`) that takes `{app, feature, action, payload}`, validates the CLI bearer, then HMAC-signs the downstream call to `<app>/api/v2`. mm-cli's `dispatch()` re-targets to the hub. One change, no per-app work.
  - **(b) SDK accepts CLI bearer as a fourth auth mode.** Extend `verifyHubRequest` to recognise `Bearer mm_…`, validate against the platform auth service, treat as session-equivalent. Every app picks it up via SDK upgrade.

  Until one of these lands, the universal verbs in §4.2 are wire-correct but blocked at runtime for anything beyond `auth: "public"` (which today is just `agent.card`). The `mm kb` / `mm crm` hand-coded wrappers still work because they hit each app's legacy `/api/rpc`, which has its own bearer-aware auth path.

- [ ] **Hub `agent.chat` not at `/api/v2`.** Hub's meta-agent is reachable only via `/api/chat` SSE bound to conversation persistence. Cross-app spec §4.1 says it should be at `/api/v2 {feature: 'agent', action: 'chat'}`. Blocks `mm ask` (§4.3).
- [ ] **gn Card honesty.** Advertise `["ask", "chat", "search", "writes"]` and either implement `agent.search` or drop the claim. 999 manifest actions vs `["ask", "chat"]` capabilities is a lie.
- [ ] **gn `either` auth rejects bearer.** Smoke test shows `gn` actions marked `auth: "either"` returning 401 — should be the laxest mode. SDK validation bug.
- [ ] **gn `Object.prototype` leakage** in manifest introspection (`getAvailableActions` using `Object.keys()` on a polluted object). Cosmetic; confuses agents reading the manifest.
- [ ] **gn `/mcp` 404** — either implement or remove `mcpUrl` field (currently absent, so OK).
- [ ] **CRM Card mcpUrl null** despite advertising 27 tools — decide whether to serve `/mcp` or remove tools from Card.
- [ ] **finances/gn/analytics agent.search** — none implement it. If `search` is a contract minimum, add façades over each app's existing search; otherwise the Card capability flag is the source of truth and mm-cli skips it gracefully.

---

## 5. Sequencing

Run §4.1–§4.2 in order (each unlocks the next). §4.3 lands once §4.2 works. §4.4 is mechanical cleanup after §4.2 — old wrappers are dead code by then. §4.5 is independent. §4.6 needs `@meta-me/app-agent` cooperation. §4.7 is a tracking list, not blocking.

Smallest viable cut: §4.1 + §4.2 ship the new verb set against the contract that's already live. Per-app wrappers can stay temporarily as aliases. That's the demo-able milestone.

---

## 6. Non-goals

- **Replacing per-repo CLIs** (`cli/kb.ts`, etc.). Those are dev tools inside each repo. `mm` is the user-facing multi-app surface; per-repo CLIs are scripting tools.
- **Manifest as full API documentation.** The Card is the agent-facing surface; the manifest is the wire schema. Full docs belong in each repo's README/CLAUDE.md.
- **MCP client transport in mm-cli (for now).** Pattern B (per cross-app spec §4.2): writes flow through `agent.chat`'s `writes[]`. Direct `/mcp` calls from mm-cli are deferred until there's a concrete need (e.g. Elicitation flow for destructive writes).
- **Fan-out at the CLI.** Cross-app questions go to `meta-me.uk` `agent.chat` and let the hub orchestrate. mm-cli stays a thin dispatcher.

---

## 7. Final task — repo discoverability

Future agents land in `~/Documents/dev/mm-cli/` with no map. Add the standard pair:

- [x] **`mm-cli/README.md`** — user-facing: what `mm` is, install, verb set, gotcha on the auth gap, source map. See [../README.md](../README.md).
- [x] **`mm-cli/CLAUDE.md`** — agent-facing: source map, how to add a new built-in vs a new v2 app, auth model + current gap, pointers to the cross-app contract and SDK. See [../CLAUDE.md](../CLAUDE.md).
