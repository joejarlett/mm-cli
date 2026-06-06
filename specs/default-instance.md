# mm default instance — per-app, pref-backed, UI-settable

Living spec for how `mm` resolves *which instance* a command targets, and how the user sets that default once and has it stick across CLI, tray, and web.

**Legend:** `[x]` shipped, `[~]` in progress, `[ ]` pending.

**Status:** Draft (2026-05-22), revised 2026-06-06 (see Update). Companions: [architecture.md](architecture.md), [`crm-v2/specs/cli-capture-ergonomics.md`](../../crm-v2/specs/cli-capture-ergonomics.md) §"Fix 1 — multi-instance owners".

---

## Update 2026-06-06 — kb write-target bug found & half-fixed; canonical key corrected

From a real incident: a `mm kb` notebook was silently created in the wrong workspace ("El Terreno KB" instead of "Joe's KB"). Two corrections to this spec:

1. **kb is not purely merge-all.** Reads merge across instances, but **writes resolve a single target.** The kb backend (`knowledgebase-v1` `hooks.server.ts` → `makeSessionCtx`) takes `instances[0]` from the bearer-validate response as `writeInstanceId`. So kb genuinely needs a **default write-target** — the original spec's "optional … if the app ever needs to pick" is now a hard requirement.

2. **The canonical pref is `defaultInstance` (camelCase), value `{ "instanceId": "<uuid>" }`** — *not* `default_instance` with a bare-string value. This is what's already deployed: `crm` and `finances` rows use it; the hub writes it via `instance.setDefault` (`meta-me.uk/src/lib/server/mm/instance.ts:184`); the web/auth session reads it via `getUserInstances` (`auth.meta-me.uk/src/lib/server/workspaces.ts`). Align this spec **and any implementation** to `defaultInstance` — do not introduce a second key. (Older prose below still says `default_instance` in places; treat `defaultInstance` as canonical.)

**Shipped (auth commit `660edd7`):** `auth.meta-me.uk` `/api/cli/validate` now orders owned instances by the `defaultInstance` pref first (then `created_at`, then `name`), matching `getUserInstances`. Because the kb backend takes `instances[0]`, the CLI write-target now honours the user's `defaultInstance` **server-side**, no client header needed. Previously it ordered `ORDER BY name`, so writes went to the alphabetically-first instance.

**Still pending — the CLI lever (this spec's job):** there is *no CLI verb to set* `defaultInstance`. Today it's only settable via the web UI or a raw `instance.setDefault` dispatch. The `mm <app> use` verb in §3 closes that gap.

---

## TL;DR

> A user can own several instances of the same app (Joe owns 5 CRMs: Joe's, GroundedNinja, El Terreno, Book The Childcare, Roleplay). When the CLI sends no instance, the hub's `resolveActiveInstance` picks one *arbitrarily* — on 2026-05-22 it picked "Book The Childcare CRM" for CRM writes, which is wrong. The just-shipped `MM_CRM_INSTANCE` env pin (commit `d9a7d9c`) patches the CRM case but doesn't generalise and isn't UI-settable. This spec makes the default a **per-app hub preference** (`userPreference`, which already exists), so it's set once — from the tray, the web, or `mm <app> use <instance>` — and honoured by every surface. The env var stays as a per-invocation/agent override.

---

## Why this exists

`mm` is multi-app, and "which instance" is a cross-app concern (`X-Hub-Instance-Id`). Today there is no durable, per-app, user-controlled default:

- The hub fallback (`resolveActiveInstance`: `requestedId → only owned CRM → first CRM in session`) is non-deterministic-from-the-user's-view for multi-instance owners.
- `MM_CRM_INSTANCE` (commit `d9a7d9c`, `src/config.ts` + `src/http/client.ts`) works but is CRM-only, lives in `~/.mm/.env`, and can't be set from a UI.
- The crm-v2 capture spec explicitly defers this: *"either an `MM_CRM_INSTANCE` env / `~/.mm/config` pin, an LRU cookie, or a hub-side 'primary instance' flag."* This spec is that follow-up — and picks the option that scales.

## What already exists (build on, don't reinvent)

- **Hub pref store** — `meta-me.uk/src/lib/server/mm/prefs.ts` exposes `prefs.get / set / delete / list` over the `userPreference` table (`userId`, `appSlug`, `key`, `value` jsonb, `updatedAt`). It already implements **per-app override with global fallback** (per-app `appSlug` wins; `appSlug IS NULL` is the fallback). This is exactly the shape a per-app default needs — no schema change required.
- **Themes are the exact precedent — copy it.** `meta-me.uk/src/lib/server/mm/theme.ts` already stores a per-app preference in this same `userPreference` table (`key: 'theme'`, resolved per-app → global → legacy → default) and ships a `theme.options` handler bundling catalog + resolved-current + global + `isOverride` for the in-app picker. `default_instance` is the identical pattern with `key: 'default_instance'`; mirror `theme.options` as `instance.options`. **The home is the prefs table, not any client** — tray, web settings, and `mm <app> use` are peer clients that all read/write the same pref, exactly as the theme picker does. Themes also propagate via session refresh (`/api/session` `themeId`) so every surface picks up a change with no extra fetch — the instance default can ride the same channel.
- **CRM instance enumeration** — `crm` exposes `workspace.list` (returns `{id, name, slug, role, isOwner}` for every CRM the caller can switch to). The tray/CLI picker reads this.
- **Account-picker precedent** — `active-account.ts` already does cookie-pinned Google-account selection for `/me/*`; the instance picker is the same pattern, promoted to a synced pref.

## What changes

### 0. App instance models (the gate — not every app wants a default)

`default_instance` only makes sense for apps that have *one active instance at a time*. Each instance-bearing app declares its model (Agent Card capability / manifest flag):

- **single-active** (e.g. `crm`) — one instance at a time; `default_instance` selects it. The pref applies fully.
- **merge-all + write-target** (e.g. `kb`) — reads/merge across *all* the user's instances, but **writes go to exactly one.** `defaultInstance` does **not** gate reads; it **does** select the write-target (where new notebooks/docs land). Proven necessary 2026-06-06 (see Update): the backend takes `instances[0]`, so an unset default writes to whatever sorts first. Treat the write-target as **required** for kb.
- **none** — single-instance or instance-agnostic; the pref is irrelevant.

`mm <app> use` offers selection for single-active apps **and** merge-all-with-write-target apps (kb); for `none` it's hidden.

### 1. Canonical preference

`[x]` Storage + setter already exist on the hub — **the CLI calls them, it does not invent storage:**

```
instance.list      { slug }                  → [{ id, name, isPrimary }, …]   (the picker source)
instance.setDefault{ slug, instanceId }      → writes user_preference row:
                                                appSlug=slug, key="defaultInstance",
                                                value={ "instanceId": "<uuid>" }
```

Handlers: `meta-me.uk/src/lib/server/mm/instance.ts` (`instanceList`, `instanceSetDefault`), wired under the `instance` namespace in `meta-me.uk/src/routes/api/mm/+server.ts`. Per-app row; `prefs.ts` resolves per-app-over-global, so no special-casing. `[ ]` Add `instance.clearDefault` for the `--clear` verb (or accept an empty `instanceId`).

### 2. Resolution precedence (the contract)

`[ ]` On every instance-scoped command, `mm` resolves the target as:

1. `--instance <uuid>` flag (per-invocation, wins).
2. `MM_<APP>_INSTANCE` env (e.g. `MM_CRM_INSTANCE`) — keep for agents/scripts/CI and project-scoped overrides.
3. **`defaultInstance` pref** (set via `instance.setDefault`) — the durable user default.
4. Hub-side `resolveActiveInstance` fallback when nothing above is set.

This subsumes the `d9a7d9c` env behaviour as layer 2 and adds the synced, UI-settable layer 3.

**Two consumption models — important.** *Single-active* apps (crm) resolve client-side and send the chosen instance as the `X-Hub-Instance-Id` header per request (layers 1–4 above). *Merge-all-with-write-target* apps (kb) resolve the write-target **server-side**: the bearer-validate endpoint (`auth.meta-me.uk/api/cli/validate`) already orders owned instances by `defaultInstance` and the backend takes `instances[0]`, so for kb the CLI only needs to **set** the pref (layer 3) — layers 1–2 (per-call `--instance`/env override of the kb *write*-target) are **not yet wired** and would require the kb backend to read a header (see open questions).

### 3. CLI verbs

`[ ]` `mm <app> use <instance>` — resolve `<instance>` (uuid | name | slug) against `instance.list { slug }`, then `instance.setDefault { slug, instanceId }`. Print confirmation. (Alias: `mm <app> instance use`.)
`[ ]` `mm <app> use` (no arg) — call `instance.list { slug }` and print each instance, marking the current default.
`[ ]` `mm <app> use --clear` — clear the `defaultInstance` pref (via `instance.clearDefault`), falling back to layer 4.
`[ ]` Surface the resolved instance in `mm status` so it's debuggable ("crm → GroundedNinja CRM (pref)", "kb → Joe's Knowledge Base (pref)").

### 4. Clients (tray / web / CLI are peers)

`[ ]` The store is the `userPreference` table. The tray (`cmd/mm-tray/`), meta-me.uk settings, and `mm <app> use` are all just clients that read/write it via `prefs.get/set` (or the `instance.options` bundle) — same as the theme picker. Setting it in the tray immediately changes CLI behaviour because they share the one pref. No client owns the state.

### 5. Propagation / caching

`[ ]` Don't fetch a pref per command. Ride the theme channel: have `defaultInstance` travel on session refresh (`/api/session`, alongside `themeId`) so the CLI reads it from the cached session like the theme id — zero per-command round-trips, and the tray/web see changes on next refresh. `mm <app> use` busts/refreshes the session locally so the change is instant. (For kb specifically, the resolved write-target already rides the bearer-validate response as `instances[0]` — see §2.)

## Open questions / blocked-on

- `[~]` **Per-app instance enumeration.** `crm` has `workspace.list`; the hub also exposes the generic `instance.list { slug }` (used by §3). Confirm `instance.list` returns the same shape for every instance-bearing app (kb, finances) so the picker generalises without per-app code.
- `[x]` **Pref key namespace.** Resolved: the canonical key is **`defaultInstance`**, value `{ "instanceId": "<uuid>" }`, already in use by `crm`/`finances`. Do not add `default_instance`.
- `[ ]` **kb per-call override.** kb resolves its write-target server-side from `instances[0]`, so `--instance`/`MM_KB_INSTANCE` do *not* yet redirect a single `mm kb add`. To support layers 1–2 for kb writes, the kb backend would need to read an `X-Hub-Instance-Id` (or `x-mcp-instance-id`) header and prefer it over `instances[0]` in `makeSessionCtx`. Decide whether kb needs per-call write redirection or whether the pref alone suffices.
- `[ ]` **Precedence surfacing.** Should `mm status` show the *whole* resolution chain (flag/env/pref/fallback) or just the winner? Lean: winner + source tag, `--verbose` for the chain.
- `[ ]` Does the tray write prefs directly via hub dispatch, or through the CLI? Lean: hub dispatch (the tray already talks to the hub), CLI is just another reader.

## Acceptance

- `mm crm use "GroundedNinja CRM"` then bare `mm crm projects` returns GroundedNinja's projects; switching to another CRM via the tray changes the CLI result with no env edit.
- `MM_CRM_INSTANCE=<other> mm crm projects` still overrides the pref (layer 2 > layer 3).
- `mm kb use "Joe's Knowledge Base"` then `mm kb collections create` lands the new notebook in Joe's KB; the auth-validate ordering (`660edd7`) means even with no default set it falls back to `created_at`, not raw alphabetical.
- `mm status` shows the resolved instance + source for each instance-bearing app.
- Setting `defaultInstance` for `crm` does not affect `kb` or other apps (per-app isolation via `appSlug`).
