# mm as an LLM-facing interface — design audit

Living spec. Companion to [architecture.md](architecture.md), which it partially **corrects and extends** (see §1). Where architecture.md asks "how do we align `mm` with the cross-app contract", this asks the prior question: **what makes a CLI a good interface when the operator is an LLM, not a human and not a dumb script?** Get this right and the alignment work has a target to aim at.

**Legend:** `[x]` shipped · `[~]` partial · `[ ]` pending · `[!]` blocked.

Status probed live **2026-05-28** (architecture.md's table was 2026-05-20 and has drifted — see §2).

### Glossary (the word "dispatch" is overloaded — pin it down)

- **dispatch / `dispatch()`** — the mm-cli client call that POSTs `{feature, action, payload}` to an app's **`/api/v2`**. The generic transport (today: `client.Rpc` / `client.V2` / `doRpcAndRender`). "Migrate kb to dispatch" = move kb off its bespoke `/api/rpc` onto this.
- **hub dispatch bridge** — a *proposed, not-yet-built* endpoint on **`meta-me.uk/api/mm`** that validates the CLI bearer and HMAC-forwards to `<app>/api/v2`. The fix for the auth gap (§2/§4.7). Server-side proxy, not a CLI thing.
- **`mm desk` + local-agent** — *unrelated.* The `meta-me-local-agent` daemon (tailnet, SQLite) behind `desk.meta-me.uk` chat threads. Not a v2 app, not on the dispatch path. Tracked separately (architecture.md §4.5).

---

## Progress log

**2026-05-28 — auth gap closed, default-instance shipped.**
- **Auth gap fixed at the root (option b).** SDK (`@meta-me/app-agent`) now accepts the `mm_…` CLI bearer as a first-class `cli` auth mode (satisfies public/session/hub/either, not install), validated via `/api/cli/validate`. Deployed to **finances, crm, analytics, gn(v4), kb** + hub. `mm <app> ask` went from a hard 401 to working.
- **Default-instance shipped.** `instance.setDefault` (hub) + `mm <app> use <name|id>` (CLI) + dispatch-time resolution (sole instance → pinned default → helpful ambiguity error). `capture.resolveInstanceId` unified onto the same `defaultInstance` pref. `mm finances ask` now works with zero flags.
- **`ask` renders `markdown_snapshot`** (was dumping the raw envelope) — first piece of the §3.1 output contract, live.
- **Verified:** `ask` returns readable prose on finances / analytics / gn.
- **NOT yet done — the §4.4 wrapper-collapse.** kb and crm still use bespoke `/api/rpc` wrappers, so their universal `ask`/`find`/`use` are shadowed (`mm crm ask` → 404 'no handler for ask'). The auth fix has *unblocked* this, but the collapse is deferred: it's a deliberate refactor with design surface, and mm-cli currently carries unrelated in-flight work. Do it when that settles. crm matters most (5 instances → default-instance only helps it once it's on the universal path).
- **Moot now:** the hub dispatch-bridge (we took option b instead).

**2026-05-29 — universal verbs everywhere; contracts landing via shared helpers.**
- **crm wrapper-collapse (partial).** `mm crm ask`/`use` now route to `/api/v2` (was a 404); crm's bespoke verbs still ride `/api/rpc`. Extracted `runV2` + `pinDefaultInstance` so the universal verbs and the kb/crm wrappers share one dispatch/resolve/render path.
- **SDK error contract (§3.2).** Unknown feature/action now returns did-you-mean suggestions + the known list (edit-distance, in the dispatcher → every app inherits). CLI renders the structured message instead of dumping the envelope. Deployed to all apps.
- **kb `research.create` 500 → clean error.** Reported via feedback: a bad/foreign collectionId hit the FK constraint and surfaced as an HTML 500. Now UUID-format + existence check. (Plus `mm kb tree <id>` / `read <short_id>` confirmed fixed by the Go kb.go rewrite.) 3 kb-cli feedback items resolved.
- **GWS upgrades** (`specs/gws-cli-upgrades.md`) shipped: `mm email --from`/`trash` (real Gmail) + `mm calendar get`/`delete`, full stack, verified with round-trips.
- **Global doc-name resolution.** `mm kb rm/read/move/rename "<title>"` no longer demands `in=<notebook>` — bare names resolve across notebooks (one `documents.list`), candidate list on ambiguity. (This was the reported "`mm kb rm` doesn't work".)
- **`mm <app> use` (no arg)** lists instances + marks the default.

**Design call (2026-05-29) — incremental shared helpers over a big-bang §7 generator.**
The resource-declaration model (§7) stays the north star, but we are **not** building the declaration-generator speculatively. Every friction point so far (instance resolution, error rendering, doc-name resolution) has been cured by pulling the behaviour into a **shared helper** that multiple apps call (`runV2`, `pinDefaultInstance`, `resolveDocument`). That delivers §7's real payoff — uniform, name-tolerant, predictable behaviour — at a fraction of the risk, driven by actual friction. The generator only earns its keep at many more than five apps. So: **capture the contracts incrementally via shared code; reach for the generator only if duplication starts to bite.** The kb pilot is deferred, not cancelled.

**Still open:** `default-instance.md`'s UI/tray setters (CLI half done); bringing crm/finances verb-parity (rm/peek/move) up to kb's via the shared helpers; the §7 generator (north star, not yet warranted).

---

## 0. Thesis

The caller is an LLM. That is a *third* kind of operator, different from both the humans and the scripts a CLI usually serves:

| | Human at a terminal | Script / CI | **LLM operator** |
|---|---|---|---|
| Interactivity | prompts, spinners, color | none | **none — one shot, no TTY** |
| Input | typo-tolerant, remembers context | exact IDs | **fuzzy (names), stateless per call** |
| Output | scannable, paginated | machine-parseable | **terse-but-complete + structured on demand** |
| Errors | "try again" | exit code | **must teach: what's wrong + what to do next** |
| Discovery | man pages, --help | hardcoded | **must self-describe in one cheap call** |
| Cost | free | free | **tokens — every wasted line is paid for** |

Designing for the LLM operator is the unifying principle. Everything below derives from it.

A second principle: **`mm` is a thin dispatcher; the intelligence lives server-side.** This is already architecture.md's stance and the platform's (`createAppDispatcher`). The LLM-interface work must not smuggle logic back into the CLI — it must push the affordances (name resolution, composite reads, structured errors) *into the apps*, where every consumer (CLI, MCP, web, cron) gets them.

### Operating assumption: we own everything and it bends

All five apps, the SDK (`@meta-me/app-agent`), the hub, and auth are ours, in active development, single user, no external consumers. So:

- **Fix roots, not symptoms.** Prefer changing the SDK / auth model over teaching the CLI a workaround. Bridges and compat shims are debt we don't need.
- **Conform apps in lockstep.** A contract change is an SDK bump + redeploy of five apps, not a multi-quarter migration. "No per-app work" stops being a tiebreaker.
- **Delete freely.** No "keep as a hidden alias for one release." On cutover, the legacy path goes.
- **Co-design, don't retrofit.** The apps' `/api/v2` surface can be shaped *natively* around the LLM verb vocabulary and contracts below — the verb set and the app surface design each other.

This posture is assumed throughout; it's what turns the sections below from "migration path around constraints" into "target shape we bend the apps to."

---

## 1. Two planes, not a hierarchy: control vs delegation

architecture.md §3 makes `ask` (→ `agent.chat`, a **per-app mini-LLM**) the primary universal verb. That's too one-size. But the fix isn't "deterministic verbs instead of `ask`" — it's recognising there are **two planes, split by who holds the intelligence**, and keeping both first-class:

- **Control plane — deterministic verbs** (`find/peek/tree/rename/move/tag` → typed `feature.action`). For when **the caller is the intelligence** and wants precision, composability, and low token cost. This is the desktop IDEs and local-agents driving `mm` for exact, full control; and any LLM doing multi-step work where step N+1 consumes step N's *structured* output. Returns terse markdown / strict `--json`.
- **Delegation plane — `ask` → app.ask** (the app's specialist agent). For when **the app is the intelligence** and the caller doesn't want to learn the schema. For fuzzy, open-ended intent ("sort out my finances question"). Returns prose + a `writes[]` summary.

**Why both, and why this resolves the tension:**

- **The app is the specialist in *both*.** Knowledge locality — the app owns its schema/entities/rules — holds whether it exposes that specialism as typed actions or as a conversational agent. New app ships its own; zero central change either way.
- **The hub stays thin in *both*.** Forwarding a typed `{app, feature, action, payload}` (HMAC-signed) is exactly as dumb as forwarding an `ask`. So `ask` does **not** reduce *hub* load — it reduces *caller* load (the caller skips learning the schema). Naming that correctly tells you when `ask` wins: genuine fuzziness, not precision.
- **Pick the plane by intent-precision**, not by preference. Precise/composable/cheap → control. Fuzzy/open-ended/one-shot → delegation.

**The failure mode to avoid:** letting `ask` *cannibalise* the control plane ("why have typed verbs, just ask the app"). That pays tokens + non-determinism for `rename this to that`. Keep the control plane primary for precise work; keep `ask` for real fuzziness.

**Where the control-plane logic lives.** The rich verbs can't be architecture.md §4.6 Card *aliases* — an alias is a 1:1 `feature.action` map, but `peek` resolves a name *then* composes three calls. That logic must live **server-side as composite, name-tolerant actions** (kb already has `collections.surface`, `collections.digest`, `meta.actions` — proof the pattern works). Then `mm kb peek X` is a thin dispatch to a real `collections.peek` action. mm stays thin; the control plane gets rich verbs; MCP and the web app reuse them.

> This reframes the kb intent-verb work from this session (`internal/cmd/kb.go`) as a **prototype of the control-plane verb shape**. The shape is right; the home for the logic is the app's `/api/v2`, not the CLI.

---

## 2. Refreshed current state (corrects architecture.md §2 & §4.7)

| Claim in architecture.md (2026-05-20) | Live status 2026-05-28 |
|---|---|
| `pi` to be removed | ✅ done — 5 apps: analytics, crm, finances, gn, kb |
| §4.7 CLI bearer can't reach `auth: hub` actions | **❌ STILL BLOCKING** — `mm finances ask` → 401 `'agent.chat' requires 'hub' auth` |
| §4.7 hub dispatch bridge (option a) | not built — no `dispatch.run` on `meta-me.uk/api/mm` |
| §4.7 gn `either` auth rejects bearer | ✅ **fixed** — `mm gn list list` now returns data |
| §4.4 kb/crm migrate off `/api/rpc` | not started — **and must not start before §4.7** (see below) |
| `mm chat` (§4.5) | renamed → `mm desk`; new built-ins since spec: `mm run`, `mm capture`, `mm-tray` |

**The gating architecture.md misses:** kb/crm hand-wrappers work *because* `/api/rpc` is bearer-aware. The v2 contract apps' `agent.chat`/session actions are not. So migrating kb to v2 (§4.4) **before** the auth gap (§4.7) is fixed would **break kb at runtime**, not clean it up. Sequence is hard, not soft: **§4.7 → §4.4**, never the reverse.

**Therefore the highest-leverage task is fixing the auth gap at the root.** architecture.md framed this as a choice between (a) a hub dispatch bridge and (b) the SDK accepting the CLI bearer, and leaned (a) because it needed "no per-app work." Under the ownership assumption (§0) that tiebreaker is void — touching five apps is cheap. So **prefer (b): extend `verifyHubRequest` to accept `mm_…` bearers, bump the SDK, redeploy all apps.** It keeps the CLI talking **directly** to apps (no hub hop, no central proxy in the hot path).

And go deeper than (b) if cheap: the CLI-bearer gap is a *symptom* of a 4-mode auth zoo (`public/session/either/hub`) the bearer fits none of. Since the apps bend, **collapse the modes** so one bearer works across hub-forwarded, session, and CLI contexts. Fix the model, don't encode its maze into the CLI.

Until that lands, the `/api/rpc` wrappers (kb/crm) keep things working — but they're **throwaway**, deleted on cutover, not a bridge to maintain.

---

## 3. The six LLM contracts (the additive layer)

architecture.md defines the *verb set*. It does not define how verbs *behave* — and that behaviour is ~80% of what "LLM-friendly" means. These belong in the SDK (`@meta-me/app-agent`) so every app inherits them; the CLI renders them. Per §0, they're **enforced** by the SDK (it owns request parsing, response shaping, and error wrapping) and rolled to all apps in lockstep — not left as per-app convention to drift.

### 3.1 Output contract
- Terse **markdown by default**; `--json` a strict mirror of the same data. (Partly shipped — `fea4f93` enforces JSON parity. Extend to a *documented* rule.)
- **Token economy is a feature.** Large payloads return a **file-path receipt**, not inline content (kb's `read → /tmp/kb-<id>.md` pattern). Define a platform threshold (proposal: inline ≤ ~8 KB / ~2k tokens, else write + return `{path, size, title}`).
- Always print the entity **id** alongside names so the LLM can pin exact follow-ups.

### 3.2 Error contract
- Structured everywhere: `{code, message, remediation}`. Never a bare HTTP status or stack.
- **Ambiguity returns candidates** ("did you mean": list with ids). The kb resolver does this; nothing else does.
- Meaningful exit codes (0 ok / 1 user-error / 2 system-error) so a harness can branch without parsing prose.

### 3.3 Reference contract
- Every entity addressable by **name OR id**, interchangeably, anywhere.
- Resolution + disambiguation is **server-side** (decision §1), so CLI/MCP/web share one behaviour.

### 3.4 Discovery contract
- One cheap call yields the whole grammar: `mm cards` (apps + capabilities) → per-app `meta.actions` / manifest. (`mm kb actions` shipped this session — generalise to a reserved `meta.actions` every app exposes.)
- Surface MCP **safety annotations** (`readOnlyHint`, `destructiveHint`) from each Card so the LLM knows what's safe before it runs.

### 3.5 Grammar contract
- **One argument convention: `key=value`** pairs, plus a tiny fixed set of global `--flags` (`--json`). Kill per-command `--flag` sprawl (the kb.go prototype mixes `--apply`/`--label` with `k=v` — a wart to remove).
- **Shared verb vocabulary across apps** (`find/peek/read/tree/add/rm/rename/tag/move`) so an LLM's knowledge of one app transfers to the next. This is the deepest LLM-friendliness lever and it's currently per-app-bespoke.

### 3.6 Safety contract
- Destructive verbs honour `destructiveHint`: print a one-line "will do X to N items" summary; support a `dry-run` norm. Low stakes today (single user) but near-free to bake into the SDK now.

---

## 4. Sequenced workstream

1. **[!] Unblock §4.7 — hub dispatch bridge** (`meta-me.uk/api/mm` `dispatch.run` → HMAC-signs to `<app>/api/v2`). Load-bearing; everything else waits on it.
2. **Encode the §3 contracts in `@meta-me/app-agent`.** Single leverage point — every app inherits output/error/reference/discovery/grammar/safety. This is the real "LLM-friendly" deliverable.
3. **Pilot server-side composite verbs on kb** — promote `peek`/`tree`/name-resolution from the CLI prototype into kb `/api/v2` actions. Proves "thin mm + smart app."
4. **[after 1] Collapse mm wrappers to thin alias/dispatch maps** (architecture.md §4.4). Once the bearer reaches v2, delete the kb/crm `/api/rpc` paths outright — no compat alias period (§0).
5. **Refresh architecture.md** against §2 here (it has drifted).

Smallest viable cut: **(1) alone** turns the already-built universal verbs from "wire-correct but 401" into working — the highest value-per-effort move on the board.

---

## 5. Non-goals

- Rewriting the per-repo dev CLIs (`cli/*.ts`). Unchanged from architecture.md §6.
- Making `mm` smart. Intelligence stays server-side; `mm` renders and dispatches.
- Boiling the ocean on day one — the contracts (§3) are additive and can land app-by-app via SDK version bumps.

---

## 6. Open questions

- **Q1.** Plane selection (§1): both planes stay first-class — but *how is the plane chosen*? Explicit verb (`mm kb peek` vs `mm kb ask`) is the obvious default. Open: should `mm` ever auto-route a bare query to `ask` when no typed verb matches, or always make delegation explicit? (Leaning: always explicit — auto-routing reintroduces non-determinism.)
- **Q2.** Threshold + format for the §3.1 file-receipt pattern — one platform default, or per-action `hint`?
- **Q3.** Composite read actions (`peek`, `tree`) — per-app, or a shared SDK helper? **Leaning settled by §0: shared SDK helper**, adopted by all apps. Remaining detail: how much does the helper assume about an app's schema (does it need a per-app "resource map" to know what `tree`/`peek` mean)?
- **Q4.** Shared verb vocabulary — *enforced* or *conventional*? **Leaning settled by §0: enforced** (we control every caller, so the SDK can reject non-canonical verbs). Remaining detail: the canonical verb list itself — which verbs are universal vs legitimately app-specific?

---

## 7. The resource-declaration model — concrete design

> Status: **north star, not yet warranted** (see the 2026-05-29 design call in the Progress log). The spine that *could* answer Q3/Q4 and collapse the kb/crm wrappers to thin dispatchers — but we're capturing its payoff incrementally via shared helpers (`runV2`, `pinDefaultInstance`, `resolveDocument`) instead of building the generator speculatively. Revisit when app count or duplication grows. Design kept below so it's ready when that day comes.

**Idea:** an app stops *hand-writing* `tree`/`peek`/`find`/`rename`/`move`/`tag`/`rm`. It **declares its resource taxonomy**, and the SDK **generates** the canonical control-plane verbs (§1) from that declaration — with name resolution, the §3 contracts, and rendering wired once. "I have collections containing documents; documents have a title and a body" → the full verb surface, identical across every app.

### Declaration shape (sketch)

```ts
defineResources({
  collection: {
    label: 'notebook',
    nameField: 'name',          // human handle: resolution + display
    parentOf: 'document',        // hierarchy → drives `tree`
    actions: { list, get, create, rename: (id,name)=>update({id,name}), remove, surface, digest },
  },
  document: {
    label: 'doc',
    nameField: 'title',
    parent: 'collection',
    bodyField: 'content',        // drives `read` (→ file-receipt) + `peek` snippet
    actions: { list /* ({parentId}) */, get, create, rename, move, remove, search },
    labels: { attach, detach, replace },   // drives `tag`/`untag`
  },
})
```

Each `actions.*` is an *existing* app function — the declaration is wiring, not new logic.

### Generated verbs (the canonical vocabulary — Q4)

| Verb | Generated from | Notes |
|------|----------------|-------|
| `tree [parent]` | `parentOf` + `list` | parents with child counts; one level down when scoped |
| `peek <ref>` | `get` (+children/outline) | name-or-id; composite (see open points) |
| `read <ref>` | `bodyField` | large body → file-receipt (§3.1) |
| `find <q> [in]` | `search` | scoped by parent |
| `rename <ref> <name>` | `nameField` update | |
| `move <ref> <parent>` | `move` | |
| `tag`/`untag <ref> <label>` | `labels.*` | |
| `add <parent> …` / `rm <ref>` | `create`/`remove` | |

**Name resolution** (`ref = name｜id`, disambiguation with candidate lists — §3.3) lives in the generated layer, **once**, instead of re-implemented per app (kb.go, crm.go, capture.ts all do their own today).

### Why this is the spine

- Deletes per-app verb code: `kb.go`'s intent verbs and crm's wrapper become `dispatch(kb, 'document.peek', {ref})` etc.
- The §3 contracts (output/error/reference/grammar/safety) are enforced **in the generator**, so every app conforms by construction — resolves Q4 (enforced) and Q3 (shared SDK helper, schema-aware via the declaration).
- New app = declare resources → full LLM-friendly surface for free.

### Migration (kb pilot, gated on approval)

1. kb already has `collections`/`documents` + every needed action. Add `defineResources(...)` → generated verbs appear on kb `/api/v2`.
2. Ship behind the existing `mm kb` verbs for parity, diff the output, then point `kb.go` at the generated actions and delete its hand logic.
3. Repeat for crm. `capture.resolveInstanceId`-style duplication folds into the shared layer.

### Open design points (need a call before building)

- **Composite reads.** kb's `peek` pulls children *and* research runs; crm's pulls interactions. Generic "`peek` = get + list-children" won't cover that. Declared **composites** (`peek: [get, children, research.list]`) vs a generic peek + per-app extension hook?
- **Wire naming.** `document.peek` (resource-qualified action) vs `peek` with a resource arg. Resource-qualified is more explicit for the manifest/`mm <app> actions`.
- **Render split.** Generated actions return *structured* data; the CLI renders markdown / `--json` (keeps "intelligence server-side, rendering at the edge"). Confirm rendering never leaks server-side.
