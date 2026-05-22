# mm-cli — Go port roadmap

> Phased delivery. Each phase ships independently, the TS keeps working throughout, rollback is one symlink change.

---

## Phase 0 — TS refactor (DONE 2026-05-22)

All in TS, all merged. Pre-port shape cleanup so the Go translation is mechanical.

- ✅ `src/wire/` — every request/response type extracted, single source of truth (`8402d56`).
- ✅ `src/http/client.ts` — four duplicate HTTP clients collapsed to one (`7dfb5a6`).
- ✅ `src/config.ts` — env vars + defaults in one struct (`bc53a47`).
- ✅ Audit + improvements docs landed (`d368ae9`).
- ✅ Security fixes (KB MCP-header, local-agent WS Origin) landed.

Pending small cleanups (not blocking):
- [ ] `mm v2` deprecated alias deletion (`06-improvements.md` #7).
- [ ] `mm admin <verb>` namespace rename (`06-improvements.md` #8).
- [ ] OrbStack rebuild + deploy of KB + CRM patches.
- [ ] Local-agent redeploy to fedora + Air when they come online.

---

## Phase 1 — Go skeleton + login + whoami

**Goal:** prove the Go toolchain, build matrix, signing, and Cobra wiring work. Smallest possible Go binary that does *something* useful.

Scope:
- `go.mod`, `cmd/mm/main.go`, `internal/config/`, `internal/auth/`, `internal/cmd/login.go`, `internal/cmd/whoami.go`, `internal/cmd/logout.go`.
- `scripts/release.sh` + `scripts/sign-darwin.sh`.
- The build matrix (4 platforms) compiles.
- Output of `mm login`, `mm whoami`, `mm logout` matches the TS version byte-for-byte.

Acceptance:
- `go test ./...` passes.
- `./scripts/release.sh v0.0.1-go` produces four binaries + checksums.
- Manual: `mm login` on m4 → `mm whoami` → `mm logout` cycle works against live `auth.meta-me.uk`. `~/.config/mm/auth.json` written/read in the existing shape.

Estimate: 0.5–1 day.

---

## Phase 2 — Hub commands (calendar / tasks / drive / email / stt / tts)

**Goal:** the bulk of daily-use commands, all behind one transport (`Hub`).

Scope:
- `internal/http/client.go` `Hub()` method.
- `internal/wire/hub.go` — translate `src/wire/hub.ts` to Go structs (mechanical).
- `internal/cmd/calendar.go`, `tasks.go`, `drive.go`, `email.go`, `stt.go`, `tts.go`.
- `internal/nldate/` — hand-rolled NL date parser per `04-nl-dates.md`. **Fully tested first** against the matrix.

Acceptance:
- Every command in scope produces output identical to TS for the test matrix.
- NL date parser passes its test table.
- `mm tts "hello" --play` actually plays audio.
- `mm stt audio.wav` returns the transcript.

Estimate: 2–3 days. NL date parser is the longest single item.

---

## Phase 3 — Local agent commands (chat + project)

**Goal:** the WS streamer. The trickiest single piece because of cursor-tracked replay.

Scope:
- `internal/http/agent.go` — `AgentFetch`, `AgentBase`, `LoadNodes`, `ResolveNode`.
- `internal/tailscale/tailscale.go` — `status --json` probe + suffix cache.
- `internal/wire/agent.go` — translate `src/wire/agent.ts`.
- `internal/cmd/chat/{chat,send,mentions}.go` — list, show, search, projects, nodes, models, **send**.
- `internal/cmd/project.go` — list, overview, detail, add, rebuild.

The mentions parser (`@<entity>` resolution) is straightforward to port; the WS streamer needs care:
- Cursor tracking per thread.
- On disconnect: re-open + `{type:"resume", cursor}`.
- Terminal handling: clear status line on `delta`, restore on `tool_start`.

Acceptance:
- `mm chat send "ping" --new` streams a reply identical to TS.
- `mm chat send "ping" --new --node MacBook\ Air` works over tailnet (when the Air is up).
- Disconnect mid-stream → reconnect resumes from cursor → no duplicated `delta` events.
- Mentions: `@fedora @joe-inc do X` routes correctly.

Estimate: 2–3 days. Mostly the WS streamer + manual reconnect testing.

---

## Phase 4 — App commands (universal verbs + manifest + cards + admin)

**Goal:** the v2 contract path + admin commands. Lower priority because the v2 surface is mostly blocked by the §4.7 auth gap.

Scope:
- `internal/http/client.go` `V2()` + `Rpc()` methods.
- `internal/manifest/`, `internal/card/` — fetch + cache (24h TTL, JSON shape compatible with TS cache).
- `internal/cmd/v2.go`, `cards.go`, `manifest.go`.
- `internal/cmd/admin/` — sql, apps, app, health, errors, error.
- `internal/db/` — pgx pool, mirror the `postgres-js` config.
- KB + CRM legacy wrappers go through `Rpc()`.

Acceptance:
- `mm kb collections`, `mm crm surface`, `mm finances` all work identically.
- `mm cards`, `mm cards <app>`, `mm manifest <app>` produce identical output.
- `mm admin sql`, `mm admin apps`, `mm admin health`, `mm admin errors` all work (note: the rename to `mm admin <verb>` lands here — TS may still be the active binary at this point, so cohabitation matters).

Estimate: 2 days. SQL + table rendering is the most code; the rest is glue.

---

## Phase 5 — Self-update + release pipeline

**Goal:** make distribution painless. Land before any non-Joe user.

Scope:
- `internal/cmd/update.go`, `internal/update/update.go` — protocol per `05-distribution.md`.
- `chat.meta-me.uk/install` route — the bash installer.
- `chat.meta-me.uk/static/dist/mm/` directory + `latest` pointer.
- First real release: `v0.1.0`.

Acceptance:
- `mm update --check` reports correctly against `latest`.
- `mm update` from `v0.1.0` to a staged `v0.1.1` succeeds. Atomic rename. Re-exec prints new version.
- Install script from a fresh machine puts the binary in the right place, with both symlinks.

Estimate: 1 day.

---

## Phase 6 — Cutover

**Goal:** retire the TS binary on Joe's machines.

Scope:
1. Install Go `mm` at `~/.mm/bin/mm` (sane primary path).
2. `~/.mm/bin/mm-ts` ← old TS binary (rollback path).
3. `~/.local/bin/mm` and `~/.mm/pi-agent/bin/mm` symlinks → `~/.mm/bin/mm` (i.e. Go).
4. Two weeks of daily use. Note any output diffs from TS, fix.
5. Delete `~/.mm/bin/mm-ts`.

Acceptance:
- Two weeks of clean Go usage.
- TS source remains in the repo as historical reference + diff target.
- No re-login needed (`auth.json` shape unchanged).

Estimate: passive observation period.

---

## Sequencing rules

- **Phase 0 must be done.** ✅
- **Phases 1–4** can be interleaved if needed, but the natural order is dependency-driven: 1 unblocks 2/3/4. 2 and 3 are independent. 4 is last because it's the lowest-traffic surface.
- **Phase 5** can land any time after Phase 1 (`mm update` is itself a Go command).
- **Phase 6** waits for 1–5.

---

## What this is not

- **Not a feature freeze on the TS.** TS keeps shipping fixes during 1–6. The Go port races to feature parity; if TS adds anything substantial mid-port, the wire spec (`01-wire.md`) gets updated and Go follows.
- **Not a rewrite of the platform.** The auth gap, the MCP-header pattern, the manifest size question — all platform concerns. The port inherits the gaps; fixes happen separately.
- **Not "Go for the sake of Go."** The case is: smaller binary, no SSE4.2 ceiling, single static distribution, self-update, ~5ms cold-start, code opacity for non-developer users. All four reasons stand. If any one becomes irrelevant later, that's reason to revisit the choice — but they all stand today.

---

## Risk register

| Risk | Likelihood | Mitigation |
|---|---|---|
| NL date parser fails to cover a phrase Joe types | Medium | Test matrix in `04-nl-dates.md`; extend on observed misses. |
| WS resume-by-cursor breaks (cursor drift, missed events) | Medium | Phase 3 manual disconnect testing; agent's resume logic is already proven against TS. |
| `tsgo` no-tsconfig limitation hiding TS errors mid-refactor | Low | Build via `bun build` (the actual transpile path) catches anything bun rejects. |
| Cobra autoformat diverges from current help text | Low | Override via `Cmd.SetHelpTemplate()` if needed. |
| TS commits between cutover-prep and cutover invalidate spec | Medium | Re-check the audit before Phase 6. The wire spec (`01-wire.md`) is structured to make this cheap. |
| Platform `auth: hub` gap blocks universal verbs in Go too | Certain | Documented expectation. Not a port-blocker; same blocker as TS. |
