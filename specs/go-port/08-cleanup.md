# Open loops — stuff to handle at the very end

> Parking lot for low-urgency follow-ups so they don't clutter the active todo list. Revisit when the Go port is feature-complete and we're tidying.

---

## Cross-machine deploys

- **fedora — local-agent rebuild.** See [meta-me-local-agent/TODO.md](../../../meta-me-local-agent/TODO.md). Carries the WS Origin gate. Offline 1d as of 2026-05-22. Build linux-x64, scp via `.new` rename dance, `systemctl --user restart meta-me-agent`.
- **MacBook Air (Pippa's, jj-macbookair) — local-agent source rsync.** See [meta-me-local-agent/TODO.md](../../../meta-me-local-agent/TODO.md). Carries the WS Origin gate. Different recipe because it's node + tsx, not a compiled binary.

Smoke test after each: curl with bogus + allowlisted + no Origin against `:31415/ws`. Expected: 403 / 101 / 101.

## Specs to update / retire after the Go cutover

- **specs/architecture.md** — partially stale. Concrete drift listed in [06-improvements.md](06-improvements.md) §"Stale claims". Either rewrite as a "what is, post-Go" doc or fold into go-port/ + delete.
- **README.md** + **CLAUDE.md** at the repo root — reference Bun + node-bundle paths. Update once Go is the only path.

## TS-side small cleanups (06-improvements items not yet actioned)

These don't block anything but the Go port spec assumes they're done:

- #7: delete `commands/v2.ts` + `case 'v2':` in `index.ts`. The Go port does not carry the alias.
- #8: rename admin commands (`apps`/`app`/`sql`/`health`/`errors`/`error`) under `mm admin <verb>`. Frees the `mm app` slot for the universal-verbs path. Touches `commands/hub.ts`, `index.ts`, help text, and the docs in README/CLAUDE.md.
- #6: standardise flag parser (Cobra is the answer in Go; TS could converge on one hand-rolled `parseArgs()` if we feel like cleaning up before cutover).

Land these whenever; not on the critical path.

## Platform-side asks (not mm-cli's fix)

Tracked in [06-improvements.md](06-improvements.md) §"Defence-in-depth" and the broader `architecture.md` §4.7. Summary:

- The bearer-auth gap: CLI bearer can't reach `auth: hub|session|either` on `/api/v2`. Two viable fixes (hub-side dispatch bridge or SDK-side bearer-as-session). Neither has landed. Go port inherits the same blocker.
- Provider API keys at `~/.pi/agent/auth.json` plaintext. Move to OS keychain only if/when distribution opens up beyond personal use.
- Server-side token revocation. `mm logout` is local-only today.

## What goes in the cleanup commit at the very end

When the Go port is the active binary on all machines and stable for 2+ weeks:

1. Delete `~/.mm/bin/mm-ts` (TS binary).
2. Update `package.json` scripts to drop Bun build targets, keep only `build:node` as the legacy export (for anyone diff-checking against TS).
3. Or: archive the TS source under `legacy/` and drop the active `package.json` entirely.
4. Update root `README.md` + `CLAUDE.md` to describe the Go-only world.
5. Retire the `00-audit.md` line about "current TS surface" — replace with a one-liner "see git log for the TS lineage".
