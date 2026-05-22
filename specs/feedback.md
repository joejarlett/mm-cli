# mm feedback — submit path

Living spec for a user/agent-facing **feedback submission** verb on the `mm` binary.

**Legend:** `[x]` shipped, `[~]` in progress, `[ ]` pending.

**Status:** Draft (2026-05-22). Companion to [architecture.md](architecture.md).

---

## 1. Why this exists

There is no way to file feedback from the `mm` binary today:

```
$ mm feedback
Unknown command: feedback
$ mm feedback --help
# … no feedback command in the help surface
```

The `mm feedback` referenced in the developer's global notes is the **admin triage** surface on the hub CLI (`npm run mm` in `meta-me.uk/`, reading the feedback table) — a *read/manage* path, not a *submit* path. So when an agent or user hits friction (an unintuitive default, a broken verb, a confusing error), there is no first-class way to drop it into the queue. It either gets worked around silently or written up as a full repo spec — too heavy for small papercuts.

This spec adds the lightweight submit half: `[ ] mm feedback`.

Concrete motivating case (2026-05-22): `mm crm log "..."` 500s with "Instance required" and doesn't auto-link the captured person. That warranted a full repo spec (`crm-v2/specs/cli-capture-ergonomics.md`). But most friction is smaller and should be a one-liner, not a spec.

## 2. What it is

A **hub-routed built-in**, not a v2-contract app. Rationale: there is no `feedback` app in `mm apps`, and feedback rows already live on the hub (the admin CLI triages them). So `mm feedback` posts to the hub at `meta-me.uk/api/mm`, exactly like `mm calendar` / `mm tasks` / `mm drive` do — see [architecture.md](architecture.md) §"What `mm` does NOT speak directly". It is *not* a `POST /api/v2 {feature, action}` app dispatch.

```
mm feedback "<text>"                     shorthand for `submit`
mm feedback submit "<text>" [flags]      file a feedback item
```

Mirroring `mm tasks` (bare command → `list`) and `mm crm log "<text>"` (one-shot capture), the bare `mm feedback "<text>"` defaults to `submit` so the common path is a single line.

### Flags

- `--app <slug>` — which app the feedback is about (`crm`, `kb`, `mm`, …). Defaults to `mm` (the CLI itself) when omitted.
- `--kind <bug|friction|idea>` — classification. Defaults to `friction`.
- `--context "<text>"` — optional extra detail (repro, the command that failed, error text).
- `--json` — parseable output (returns the created feedback id + status), per the platform-wide `--json` convention.

### Auto-captured metadata (no flag needed)

Each submission should stamp:
- `source: "cli"` (or `"agent"` when invoked by the local agent — detect via env, e.g. an agent-set marker).
- `mm` version (`mm v0.1.0`).
- `whoami` user id (already on the bearer).

This lets triage distinguish agent-surfaced friction from human reports, and ties a report to the CLI build that produced it.

## 3. Behaviour

- `[ ]` `mm feedback "text"` → POST to hub feedback-create; print `✓ Filed feedback <id> (friction, app: mm)`.
- `[ ]` Empty text → error, non-zero exit, point at `mm feedback help`.
- `[ ]` `--json` → `{ "id": "...", "status": "open", "kind": "friction", "app": "mm" }`.
- `[ ]` `mm feedback help` → subcommands + flags block, matching the house help style (see `printTasksHelp`).

## 4. Placement (for whoever implements)

Per [CLAUDE.md](../CLAUDE.md) §"Source map" and the hub-routed-built-in pattern:

- New `src/commands/feedback.ts` exporting `feedbackDispatch(command, args, flags)` + `printFeedbackHelp()`, shaped like `src/commands/tasks.ts`.
- Route the actual call through `hubApi` (`../hub`), same as tasks/calendar/drive.
- Add response types to `src/wire.ts` (e.g. `HubFeedbackSubmitResp`).
- Register the verb in `src/index.ts` command routing.
- Admin *triage* verbs (list/status/resolve) stay out of scope here — they belong with the hub admin surface (`src/commands/hub.ts` if ever ported), not the user-facing submit path.

## 5. Open questions / blocked-on

- `[ ]` **Hub endpoint contract.** Confirm the hub route + payload for feedback-create under `meta-me.uk/api/mm` (action name, required fields, whether `app`/`kind` are first-class columns or land in a `data` jsonb). This spec assumes a create exists or is cheap to add alongside the existing feedback table the admin CLI already reads.
- `[ ]` **`source: "agent"` detection.** Decide the signal the local agent sets so CLI-vs-agent submissions are distinguishable (env var vs explicit `--source`).
- `[ ]` Should `--app` validate against the live `mm apps` list, or accept free text? Lean: validate, fall back to free text with a warning.

## 6. Acceptance

- `mm feedback "the crm log command needs a default instance"` files a row visible to the hub admin triage surface, tagged `source: cli`, `app: mm`, `kind: friction`.
- `mm feedback "X" --json` returns the created id.
- Round-trips for an agent: a programmatic `mm feedback "<friction>" --app crm --json` invocation succeeds with the existing bearer and no extra auth.
