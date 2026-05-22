# Improvements surfaced by the audit

> Things the audit revealed that should change in the codebase or contracts, separate from the Go port work itself. Most are small. Some are urgent security work that needs platform-side fixes.

---

## Urgent — security

### 1. `X-MCP-User-ID` header trust on `kb` + `crm` (FIXED in repo, pending production deploy)

**Severity:** Unauthenticated impersonation. Anyone on the internet could `curl` `kb.meta-me.uk/api/rpc` or `crm.meta-me.uk/api/rpc` with `X-MCP-User-ID: <uuid>` + `X-MCP-Instance-ID: <uuid>` and act as the named user — read their docs, write to their KB, log CRM interactions, etc. No token, no signature, no validation.

**Root cause:** `handleMcpAuth` in `<app>/src/hooks.server.ts` was wired into the SvelteKit `sequence()` and set `event.locals.user` directly from request headers. The likely original intent was that these headers would only ever arrive on HMAC-signed requests from the hub, but the HMAC verification step was never added.

**Patch applied (2026-05-22):**
- [knowledgebase-v1/src/hooks.server.ts](../../../knowledgebase-v1/src/hooks.server.ts) — committed `360c737`
- [crm-v2/src/hooks.server.ts](../../../crm-v2/src/hooks.server.ts) — local edit, **uncommitted** because file has unrelated in-flight work (`activeWorkspaceId → activeInstanceId` rename + `handleBearerAuth` introduction). Developer to commit alongside that work.

`handleMcpAuth` function bodies are kept in place so reinstating behind HMAC verification later is a 5-line diff: verify signature → THEN trust headers.

**Deploy:** OrbStack rebuild. `cd ~/Documents/dev/infra && docker compose build gn-kb crm-v2 && docker compose up -d gn-kb crm-v2`.

**Other apps to audit:** `gn` source isn't local but the URL is in `apps.ts`. `finances` and `analytics` were grep'd — they don't contain this header path. The shared SDK `@meta-me/app-agent` also doesn't contain it. So the pattern was hand-rolled in KB and CRM independently — not via the SDK.

### 2. WS Origin check on local agent (FIXED + DEPLOYED to m4; pending fedora + air)

**Severity:** Drive-by attack on localhost. A malicious page in any browser on the host could `new WebSocket('ws://localhost:3142/ws')` and send `{type: 'send', threadId, content}` to drive an LLM turn, including tool calls (bash). Browsers don't apply CORS to WebSocket connections; the agent didn't check `Origin` on upgrade.

**Patch:** [meta-me-local-agent/src/server.ts:1055-1065](../../../meta-me-local-agent/src/server.ts#L1055-L1065). Origin header now checked against `ALLOWED_ORIGINS` (same list HTTP CORS uses). Absent Origin allowed (CLI clients don't send it).

Smoke-tested on m4: ✓ rejects `evil.example.com` with 403, ✓ accepts `chat.meta-me.uk`, ✓ accepts no-Origin.

Pending fedora + Air redeploy — both offline. See [meta-me-local-agent/TODO.md](../../../meta-me-local-agent/TODO.md).

---

## Defence-in-depth (lower urgency)

### 3. `<app>/api/v2/manifest` is publicly enumerable

Anyone can fetch a full action list with `auth` modes from any app's manifest endpoint. Standard self-describing-API pattern (GraphQL/OpenAPI) and the actual gate is server-side enforcement. Not a vuln, but combined with the §4.7 platform auth gap (bearer can't reach `auth: hub|session|either`), the exposure of which actions need which auth makes the surface easier to map.

**Recommendation:** No fix needed today. Worth noting if the auth-bridge ever lands and these modes start being CLI-reachable.

### 4. Provider API keys at `~/.pi/agent/auth.json` plaintext (mode 0o600)

Standard CLI risk — any process running as the user can exfiltrate the keys. Fix would be OS keychain integration (macOS Keychain, Linux Secret Service). Not worth doing while this remains a single-user CLI.

---

## TS cleanups (mostly for the Go port to inherit a clean spec)

### 5. `commands/email.ts` duplicates `hubApi()`

Lines 13-41 of `commands/email.ts` re-implement the same `hubApi()` function that already exists in `src/hub.ts`. Identical behaviour. Remove the duplicate, import from `hub.ts`. ~10-line cleanup. Falls out of the "one HTTP client" refactor in 03-architecture.md anyway.

### 6. Flag parser reinvented four times

`commands/calendar.ts`, `commands/tasks.ts`, `commands/chat.ts`, `commands/hub.ts` each have their own `parseFlags()`. Standardise on one. Probably `clipanion` or a hand-rolled `src/flags.ts`. Falls out of the TS refactor pass.

### 7. `mm v2` is a deprecated alias

`architecture.md` §4.4 says "Delete the `mm v2 …` command file". Still present. Currently passes through to the same `dispatch()` as `mm <app> <feature> <action>`. Drop `commands/v2.ts` + the `case 'v2':` in `index.ts`. Tiny commit.

### 8. `mm app` (admin) vs `mm <app>` (universal verbs) name collision

`commands/hub.ts` exports `appDispatch` (admin: enable/disable an app row) which `index.ts` aliases to handle `case 'app':`. `appDispatch` (universal verbs) from `commands/app.ts` is the fallthrough for any registered slug. They occupy the same top-level name; admin wins via switch precedence.

If a future app is ever registered with slug `'app'` (unlikely but possible), the universal-verbs path is unreachable.

**Recommendation:** rename the admin verbs under `mm admin <verb>` (e.g. `mm admin app <slug>`, `mm admin sql`, `mm admin health`, etc.). Frees the top-level slots, matches the convention every other modern CLI uses for "dangerous mutate ops behind a namespace."

### 9. Env-var naming inconsistency

- `MM_HUB_URL` — read only by `commands/stt.ts` and `commands/tts.ts`. Other hub-touching commands hardcode `https://meta-me.uk`.
- `MM_LOCAL_AGENT_URL` — read by `commands/chat.ts`. Consistent.
- `MM_DATABASE_URL` / `DATABASE_URL` — read by `src/db.ts`. Two fallback layers, OK.

Promote `MM_HUB_URL` to all hub callers (touch `src/hub.ts` + `src/api.ts`). Falls out of the TS config-module refactor.

---

## Stale claims in `specs/architecture.md`

The architecture spec was last touched 2026-05-20 and has drifted in places. Either update or retire:

- **§1** "single compiled binary (Bun `--compile`)" — still true today, but about to be superseded by node-bundle + eventual Go port. Update when Go ships.
- **§4.1** "`mm apps` Deferred: name collision" — still deferred. Resolution proposed in #8 above (`mm admin <verb>`).
- **§4.4** "Delete `mm v2 …` command file once raw fallback is wired" — still present. Should action (see #7).
- **§4.5** "today it reads the local agent's SQLite directly" — **no longer true.** Refactored to pure HTTP on 2026-05-22. Update or remove the bullet.
- **§4.6** Aliases in Cards — `findAlias()` exists in `agent-card.ts` but isn't called from `commands/app.ts`. Either wire it up (and have the SDK emit aliases per #4.6) or remove the dead function.
- **§7** "README.md + CLAUDE.md" marked done — verified present. No action.

---

## Out-of-scope (noted, not actioned)

- **Migrate `kb`+`crm` from `/api/rpc` to `/api/v2`** (architecture.md §4.4). Blocked by the platform auth gap (§4.7). Bearer doesn't reach `/api/v2` for non-public actions; until the hub-side dispatch bridge or SDK-side bearer-as-session lands, `/api/rpc` is the only path that works for these two apps.
