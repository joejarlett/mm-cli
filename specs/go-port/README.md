# mm-cli — Go port

A series of specs for rewriting mm-cli from TypeScript to Go.

## Read order

1. [00-audit.md](00-audit.md) — what mm-cli does today (surface, deps, files, env vars)
2. [01-wire.md](01-wire.md) — every HTTP/WS endpoint, request/response shape
3. [02-auth.md](02-auth.md) — OAuth device flow + AuthState on disk
4. [03-architecture.md](03-architecture.md) — Go package layout, library choices
5. [04-nl-dates.md](04-nl-dates.md) — chrono-node replacement (the only non-mechanical port)
6. [05-distribution.md](05-distribution.md) — cross-compile, hosting, self-update
7. [06-improvements.md](06-improvements.md) — cleanups + security findings surfaced by the audit
8. [07-roadmap.md](07-roadmap.md) — phased delivery (TS keeps working throughout)

## TL;DR

- **TS-side refactor done** (`wire/`, `http/client.ts`, `config.ts`). The TS already has the shape the Go port mirrors 1:1.
- **One genuine port challenge:** `chrono-node` → hand-rolled NL date parser (~150 LOC, fully tested).
- **Everything else is mechanical:** HTTP client maps to one `Client` struct with three methods (Hub / V2 / Rpc); wire types map to Go structs; config maps to one struct.
- **Why Go:** ~10–15 MB self-contained binary (vs ~104 MB Bun-compile or node-bundle + node runtime), no SSE4.2 ceiling (works on 2008-era CPUs), ~5 ms cold-start, self-updating, no runtime assumed on the target.
- **Time estimate:** ~1 week of focused work across Phases 1–5. Cutover passive.
- **Risk profile:** low. Platform auth gap (`auth: hub|session|either` unreachable from CLI bearer) is inherited from TS, not introduced. Everything else is mechanical translation against a verified-working TS reference.
