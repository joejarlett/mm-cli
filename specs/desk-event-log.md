# Desk Event Log + Overview — a reflective "what's happened" surface

**Status: built (2026-06-06).** Implemented end-to-end and smoke-tested against a live
model. Backend in **`meta-me-local-agent`** (`src/events/log.ts`, schema + routes +
scheduled sweep in `src/db/schema.ts` / `src/server.ts`, tests in
`src/events/log.test.ts`); CLI half in this repo (bare `mm desk` + `mm desk refresh` in
`internal/cmd/desk.go`, wire types in `internal/wire/agent.go`, render tests in
`internal/cmd/desk_overview_test.go`). What shipped matches the design below, with two
notes: (1) cross-sweep open-loop netting is done by feeding the thread's still-open loops
back to the extractor, which cites their ids in `resolved_ids`; (2) the activity stream
excludes `open_loop` (shown only in its own section) and keeps `resolved` rows as the
"this got closed" signal — see the inline comments in `queryDeskOverview`; (3) the
open-loops section is gated on the loop's *thread* having moved within the `days` window
(not the loop's own age) — a loop on a thread untouched for weeks isn't actionably "open",
and this drops post-backfill staleness. Verified on real data: 21 unresolved loops total
collapse to 5 at the 7-day default, all 21 visible at `--days 30`.

Original design follows.

**Direction, not contract (drafted 2026-06-06).** The bulk of the work lives in
**`meta-me-local-agent`** (the Bun backend that owns `~/.mm/meta-me-local-agent.db`),
not in this repo. The CLI half is a thin passthrough.

**Goal:** give `mm desk` a summary surface — the conversational analogue of
`mm project overview`. Bare `mm desk` should answer *"what have I been working on with
the agent lately, and what's still open?"* without re-reading transcripts at read time.

---

## Why an event log, and why a scheduled sweep

We considered three ways to capture salient activity from threads. The reasoning that
led here, kept so the trade-offs aren't re-litigated:

| Capture model | Freshness | Salience precision | Flow impact | Robustness | Cost |
|---|---|---|---|---|---|
| Inline tool (agent calls `log_event` mid-turn) | instant | high | adds latency, model must remember | brittle | med |
| Post-turn hook | instant | med | in the hot path | med | med |
| **Scheduled sweep** (this spec) | lags by interval | med | **none** | **high** | **low** |

A desk overview is a **reflective** surface, not a live feed — so the sweep's only
weakness (lag of one interval) costs nothing, and in exchange:

1. **Zero flow impact.** Capture is a separate process; turns never pay for it and the
   model never has to remember anything.
2. **Better granularity.** A unit of work (a decision, an open loop) usually spans
   several turns. A sweep reads the whole span at once, summarises at the natural unit,
   and can **net things out** — an open loop opened and closed within the swept window
   can be emitted already-resolved, or dropped. Less noise, for free.
3. **Resumable + idempotent.** A per-thread watermark means a failed run just retries
   from where it left off; no event is ever tied to a turn that might not fire.

**Architectural consistency is the clincher.** This is exactly how `mm project overview`
already works: the project index is not built inline as files change — it is a batch
refresh (`POST /api/projects/{id}/index/refresh`) read cheaply afterward. A scheduled
event sweep makes the desk surface *identical in shape* to the project surface instead
of a special case.

**Accepted cost:** this is extractive summarisation (reconstructing salience from
transcript) rather than capturing intent live. Bounded by the watermark (only the new
tail), a cheap model, and batching — and the wider window the batch sees largely
compensates. For a reflective surface it is the right trade.

**Explicitly out of scope for v1:** the inline `log_event` tool and the post-turn hook.
The sweep alone is a complete solution. Add the explicit tool later *only if* the sweep
is observed to miss high-intent moments — do not build it speculatively.

---

## Data model (`meta-me-local-agent` DB)

One append-only table.

```sql
CREATE TABLE event (
  id          TEXT PRIMARY KEY,          -- uuid
  ts          INTEGER NOT NULL,          -- ms; the source message's created_at, not sweep time
  thread_id   TEXT NOT NULL,
  project_id  TEXT,                      -- nullable, mirrors thread.project_id
  kind        TEXT NOT NULL,             -- decision | open_loop | resolved | artifact | fact | question
  summary     TEXT NOT NULL,             -- one line, the way a memory file or index entry is one line
  refs_json   TEXT,                      -- JSON array: file paths, run ids, thread ids, urls touched
  resolves    TEXT                       -- nullable; event.id this `resolved` closes (open_loop ↔ resolved pairing)
);
CREATE INDEX event_ts        ON event (ts DESC);
CREATE INDEX event_project   ON event (project_id, ts DESC);
CREATE INDEX event_open_loop ON event (kind) WHERE kind = 'open_loop';
```

Watermark — one row per thread, advanced by the sweep:

```sql
CREATE TABLE event_cursor (
  thread_id            TEXT PRIMARY KEY,
  last_message_id      TEXT NOT NULL,
  last_swept_ts        INTEGER NOT NULL   -- sweep wall-clock, for observability
);
```

**Kinds, kept deliberately small** (discipline is what keeps the overview readable):
- `decision` — a choice made and why.
- `open_loop` — something raised and not yet resolved (the highest-value kind).
- `resolved` — closes an `open_loop` via `resolves`; the sweep nets these where it can.
- `artifact` — a file/PR/doc/run produced (`refs_json` carries the pointer).
- `fact` — something learned worth keeping at thread scope (distinct from durable memory).
- `question` — an open question to the user, still unanswered.

---

## The sweep job

Runs on a cron (~15 min) and on demand. Idempotent.

```
for each thread where updated_at > min(event_cursor.last_swept_ts) (or no cursor row):
    cursor   = event_cursor[thread_id]        # may be absent → start of thread
    tail     = GET /api/threads/{id}/messages since cursor.last_message_id
    if tail empty: continue
    events   = extract(tail, thread.project_id)   # cheap model, structured output
    within a single txn:
        for e in events:
            if e.kind == 'resolved' and matching open_loop exists in window: net out / link via resolves
            insert event
        upsert event_cursor[thread_id] = (last tail message id, now)
```

- **Model:** a cheap one (Haiku / gemini-flash). The agent already holds provider keys
  (cf. `mm desk models`). This is exactly the bulk-extract job a flash model is for.
- **Extraction prompt** returns a strict JSON array of `{kind, summary, refs, ts, resolves?}`
  via structured output — no free-form parsing. `ts` is the source message's
  `created_at`, so the log timeline reflects when things happened, not when swept.
- **Netting:** if an `open_loop` and its `resolved` both fall in one window, emit the
  `resolved` linked (or drop the pair) so transient loops never surface as open.
- **Bounded work:** only the tail past each watermark is ever read.

### Trigger
- Cron inside `meta-me-local-agent`, ~15 min. (Or, if simpler operationally, a `mm`
  invocation on a host schedule — but in-process cron keeps it self-contained, matching
  how the project index refreshes.)
- On-demand refresh endpoint (below), surfaced as `mm desk refresh`, mirroring
  `mm project rebuild`.

---

## Endpoints (`meta-me-local-agent`)

```
GET  /api/events/overview?project_id=&days=&limit=
        → { generated_at, window_days,
            open_loops:  [ {summary, thread_id, project_id, ts, refs} ],   # surfaced first
            by_project:  [ { project_id, label, buckets: {today, this_week, earlier},
                             events: [ {kind, summary, thread_id, ts, refs} ] } ],
            unassigned:  [ ...same shape, threads with no project ] }

POST /api/events/refresh   { thread_id? }    # run the sweep now; all threads or one
        → { swept_threads, new_events }
```

`overview` is read-cheap: pure DB grouping, no model call. Open loops first because
"what's still open" is the single thing a flat `mm desk list` can never tell you.

---

## CLI surface (this repo — the only part buildable here now)

Thin passthroughs, identical pattern to `internal/cmd/project.go`
(`passthroughGet` for reads, a `POST` for refresh). Wire into the existing `desk` tree
in `internal/cmd/desk.go`.

- **Bare `mm desk`** → `GET /api/events/overview`. Make the no-subcommand path the
  overview (the way bare `mm calendar` is the agenda), instead of printing help. This
  keeps the surface from sprawling into a new top-level command.
  - Flags: `--project <uuid|label>` (reuse `resolveProjectRef`), `--days N`,
    `--limit N`, `--json`, `--node`.
- **`mm desk refresh [--project ...]`** → `POST /api/events/refresh`, mirroring
  `mm project rebuild`.
- `list` / `show` / `search` / `projects` / `send` / `nodes` / `models` unchanged.

Output conventions (per repo CLAUDE.md): `--json` everywhere; payload to stdout,
status/retry to stderr; honour `MM_AGENT`/`MM_SOURCE` for colour suppression;
`cobra.NoArgs` on the bare leaf if it takes no positionals.

### Rendered example (non-JSON)

```
Desk — last 7 days

⏳ Open loops
  • drive --as md returns 503 from gateway export      [mm-cli · 2d ago]
  • decide: fold desk overview into bare `mm desk`?     [mm-cli · 4h ago]

mm-cli  (8 events)
  today     ✓ shipped drive read/export commands + tests
            → spec: desk event log (specs/desk-event-log.md)
  this week ✓ intent-grouped top-level help
            ✓ kb id/name resolution hardening

(unassigned)  (2 events)
  today     ? asked user which release channel to cut
```

---

## Relationship to adjacent stores (decided, not open)

Three stores, deliberately distinct lifecycles — do **not** merge:
- **`mm run` audit log** (`meta-me.uk/admin/audit`) — *run-level* execution record.
- **Memory files** (`~/.claude/.../memory/`) — *durable cross-session curated fact*.
- **Desk event log** (this) — *per-thread salient activity stream*, ephemeral-ish.

The desk log is the activity stream; memory is the distillate. A `fact` event that
proves durable may later be promoted to a memory file by hand — but the two have
different lifecycles and should stay separate.

---

## Build order

1. **`meta-me-local-agent`**: `event` + `event_cursor` tables; the sweep job +
   extraction prompt; `GET /api/events/overview`; `POST /api/events/refresh`; cron.
2. **mm-cli** (this repo): bare-`mm desk` overview passthrough + `mm desk refresh`,
   table-driven tests with a mocked agent client (no live tailnet calls).

Step 2 is ~a day and self-contained once the endpoints in step 1 exist; until then it
has nothing to call. Ship step 1 first.
