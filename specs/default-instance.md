# mm default instance — per-app, pref-backed, UI-settable

Living spec for how `mm` resolves *which instance* a command targets, and how the user sets that default once and has it stick across CLI, tray, and web.

**Legend:** `[x]` shipped, `[~]` in progress, `[ ]` pending.

**Status:** Draft (2026-05-22). Companions: [architecture.md](architecture.md), [`crm-v2/specs/cli-capture-ergonomics.md`](../../crm-v2/specs/cli-capture-ergonomics.md) §"Fix 1 — multi-instance owners".

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
- **merge-all** (e.g. `kb`) — reads/merges across *all* the user's instances; there is no single active one. `default_instance` does **not** gate reads. At most it's an optional *default write-target* (where new docs land) if the app ever needs to pick. Don't force a single-active model onto it.
- **none** — single-instance or instance-agnostic; the pref is irrelevant.

`mm <app> use` offers selection only for single-active apps; for merge-all it's a no-op (or sets the write-target hint), for none it's hidden.

### 1. Canonical preference

`[ ]` Store the default as a hub preference:

```
prefs.set { app: "<slug>", key: "default_instance", value: "<instance-uuid>" }
```

Per-app row (`appSlug = "crm"`, `"kb"`, …). `prefs.ts` already resolves per-app-over-global, so no special-casing.

### 2. Resolution precedence (the contract)

`[ ]` On every instance-scoped command, `mm` resolves `X-Hub-Instance-Id` as:

1. `--instance <uuid>` flag (per-invocation, wins).
2. `MM_<APP>_INSTANCE` env (e.g. `MM_CRM_INSTANCE`) — keep for agents/scripts/CI and project-scoped overrides.
3. **`prefs.get { app, key: "default_instance" }`** — the durable user default (new).
4. Hub-side `resolveActiveInstance` fallback (unchanged) when nothing above is set.

This subsumes the `d9a7d9c` env behaviour as layer 2 and adds the synced, UI-settable layer 3.

### 3. CLI verbs

`[ ]` `mm <app> use <instance>` — resolve `<instance>` (uuid | name | slug, via the app's instance list) and `prefs.set` it as that app's `default_instance`. Print confirmation.
`[ ]` `mm <app> use` (no arg) — show the current default + list selectable instances (from `workspace.list` for crm; per-app equivalent otherwise).
`[ ]` `mm <app> use --clear` — `prefs.delete`, falling back to layer 4.
`[ ]` Surface the resolved instance in `mm status` so it's debuggable ("crm → GroundedNinja CRM (pref)").

### 4. Clients (tray / web / CLI are peers)

`[ ]` The store is the `userPreference` table. The tray (`cmd/mm-tray/`), meta-me.uk settings, and `mm <app> use` are all just clients that read/write it via `prefs.get/set` (or the `instance.options` bundle) — same as the theme picker. Setting it in the tray immediately changes CLI behaviour because they share the one pref. No client owns the state.

### 5. Propagation / caching

`[ ]` Don't fetch a pref per command. Ride the theme channel: have `default_instance` travel on session refresh (`/api/session`, alongside `themeId`) so the CLI reads it from the cached session like the theme id — zero per-command round-trips, and the tray/web see changes on next refresh. `mm <app> use` busts/refreshes the session locally so the change is instant.

## Open questions / blocked-on

- `[ ]` **Per-app instance enumeration.** Only `crm` has `workspace.list` today. For the picker to generalise, each instance-bearing app needs an equivalent (or the hub exposes a generic "instances for app X for this user"). Until then, `use` works for CRM and degrades to "paste a uuid" elsewhere.
- `[ ]` **Pref key namespace.** Confirm `default_instance` doesn't collide with existing `userPreference` keys (audit `prefs.list`). Reserve a documented key registry if more cross-app prefs are coming.
- `[ ]` **Precedence surfacing.** Should `mm status` show the *whole* resolution chain (flag/env/pref/fallback) or just the winner? Lean: winner + source tag, `--verbose` for the chain.
- `[ ]` Does the tray write prefs directly via hub dispatch, or through the CLI? Lean: hub dispatch (the tray already talks to the hub), CLI is just another reader.

## Acceptance

- `mm crm use "GroundedNinja CRM"` then bare `mm crm projects` returns GroundedNinja's projects; switching to another CRM via the tray changes the CLI result with no env edit.
- `MM_CRM_INSTANCE=<other> mm crm projects` still overrides the pref (layer 2 > layer 3).
- `mm status` shows the resolved instance + source for each instance-bearing app.
- Setting `default_instance` for `crm` does not affect `kb` or other apps (per-app isolation via `appSlug`).
