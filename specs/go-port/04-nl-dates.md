# mm-cli — natural-language date parsing (chrono replacement)

> The single non-mechanical piece of the Go port. Today `chrono-node` parses every `--when` and `--due` flag. Go has no peer library. This doc scopes what the port actually needs to cover.

---

## 1. Today: chrono-node coverage

mm-cli uses two entry points in [src/nl-date.ts](../../src/nl-date.ts):

```ts
parseNlDateTime(raw): { iso: string; date: Date }  // for --when
parseNlDate(raw): { iso: string; date: Date }      // for --due (date only)
```

Both fall back to `chrono.parse(trimmed, new Date(), { forwardDate: true })` if a direct ISO parse fails.

`forwardDate: true` is the critical setting — when the input is ambiguous (e.g. "Monday"), chrono picks the next future Monday, not the most recent past one.

Used by: `mm calendar new --when`, `mm calendar new --end`, `mm tasks add --due`.

---

## 2. Real-world phrases the user actually types

Reconstructed from CLAUDE.md examples + memory + intent of the feature. Anything not in this list is acceptable to fail.

### Times (for `--when`)

```
2026-05-20 14:00
2026-05-20T14:00
tomorrow 14:00
tomorrow 2pm
next monday 10am
next friday 14:00
fri 09:00
in 2 hours
in 30 minutes
at 3pm                  (today)
```

### Dates (for `--due`)

```
2026-05-20
tomorrow
next friday
next week              (Monday of next week)
in 3 days
end of week            (Friday this week)
```

### Bare times (for `--end`)

Handled specially — see [src/commands/calendar.ts](../../src/commands/calendar.ts) `--end` block. `"15:00"` is taken as same-day-as-start. Otherwise treated as a full `--when`.

---

## 3. Go landscape

| Library | Coverage | Notes |
|---|---|---|
| `github.com/olebedev/when` | Relative ("tomorrow", "in 2 hours"), some weekday names | Active, ~3k stars. Doesn't cover "next monday 10am" cleanly. |
| `github.com/araddon/dateparse` | Many ISO/RFC variants | Format autodetect; not NL. |
| `github.com/markusmobius/go-dateparser` | Port of Python's dateparser | Closest to chrono in scope. 1k stars. Pulled in dependencies are heavy. |
| Hand-rolled | Sane actual phrase list | ~150 LOC, zero deps. |

**Recommendation: hand-roll.** The phrase list above is short and bounded. Chrono is over-spec'd for these patterns. A purpose-built parser is faster, dependency-free, fully testable, and never surprises with "oh chrono interpreted that differently in v3."

---

## 4. Hand-rolled parser — scope

### Inputs

`parseNL(raw, opts) → time.Time` with:

- `opts.now time.Time` — anchor (test injection)
- `opts.location *time.Location` — defaults to `time.Local`
- `opts.forwardDate bool` — defaults to `true`

### Algorithm

In order, stop at first match:

1. **Strict ISO.** Match `\d{4}-\d{2}-\d{2}(T| )\d{2}:\d{2}(:\d{2})?` → `time.Parse` with that layout.
2. **ISO date only.** Match `\d{4}-\d{2}-\d{2}` → midnight in `opts.location`.
3. **`tomorrow [HH:MM|<N>am|<N>pm]`** → `opts.now.AddDate(0, 0, 1)`, time applied if present.
4. **`today [HH:MM|<N>am|<N>pm]`** → `opts.now`, time applied.
5. **`yesterday [HH:MM|...]`** → `opts.now.AddDate(0, 0, -1)`.
6. **`(next|this) <weekday> [HH:MM|...]`** → next/this Monday-Sunday. `next monday` after a Monday = Monday in 7 days. `this monday` = current week's Monday.
7. **`<weekday> [HH:MM|...]`** → with `forwardDate=true`, the next future occurrence. Without, the most recent past.
8. **`in <N> (minute|min|hour|hr|day|week|month)s?`** → `opts.now.Add(N * unit)`.
9. **`at <HH:MM|<N>am|<N>pm>`** → today at that time. If past, with `forwardDate=true`, tomorrow.
10. **`end of (week|month)`** → Friday this week / last day of this month.

Anything else → error: `"could not parse \"$raw\" as a date/time"`.

### Time formats accepted

- `HH:MM` (24-hour)
- `H:MM` (24-hour, single digit hour)
- `<H>am`, `<H>pm`, `<H>:MMam`, `<H>:MMpm` (case-insensitive)

### Weekday names

`mon|tue|tues|wed|thu|thur|thurs|fri|sat|sun` and full names. Case-insensitive.

### Coverage matrix (test vectors)

| Input | Expected (assuming now=2026-05-22 14:00 local, Fri) |
|---|---|
| `2026-05-20 14:00` | `2026-05-20 14:00:00 local` |
| `tomorrow 14:00` | `2026-05-23 14:00:00 local` |
| `tomorrow 2pm` | `2026-05-23 14:00:00 local` |
| `next monday 10am` | `2026-05-25 10:00:00 local` |
| `next friday 14:00` | `2026-05-29 14:00:00 local` |
| `fri 09:00` | `2026-05-29 09:00:00 local` (forward; today is Friday but we want next) |
| `in 2 hours` | `2026-05-22 16:00:00 local` |
| `in 30 minutes` | `2026-05-22 14:30:00 local` |
| `at 3pm` | `2026-05-22 15:00:00 local` |
| `at 10am` | `2026-05-23 10:00:00 local` (forward; 10am is past) |
| `2026-05-20` | `2026-05-20 00:00:00 local` |
| `tomorrow` | `2026-05-23 00:00:00 local` |
| `next friday` | `2026-05-29 00:00:00 local` |
| `in 3 days` | `2026-05-25 14:00:00 local` |
| `end of week` | `2026-05-22 (today Friday)` → today; if today Mon-Thu → upcoming Friday |
| `garbage` | Error |

Test file: `internal/nldate/nldate_test.go`. Should run with `go test ./...`.

---

## 5. Spec-driven testing

Single source of truth: the table above goes into Go as a parametrised test. The TS side gets the same table (regenerate `src/nl-date.ts` test fixtures from it) — if chrono ever drifts, we'll see the diff.

For phrases not in the table that worked in chrono: explicitly fail. The CLI prints a clear error pointing at the supported set. Users either learn the simpler set or fall back to ISO.

---

## 6. Why not just shell out to a JS helper

Tempting: keep `nl-date.ts` as a tiny node script the Go binary execs. Avoids the rewrite, keeps chrono's full coverage.

Downsides:
- Defeats the "single binary, no runtime" Go advantage.
- Adds startup cost (~50ms per parse).
- Adds a deploy surface (the helper script needs to ship alongside).
- Forks the codebase: TS does NL, Go does everything else.

Not worth it. Hand-roll.

---

## 7. Anti-scope

These chrono features are out:

- Relative phrases beyond a few units ("the day after tomorrow at lunch" — not supported)
- Holiday names ("Christmas", "Easter" — not supported)
- "Next-next" ("next next week" — not supported)
- Locale-specific phrases ("manhã" / "morgen" / "demain" — English-only)

If the user finds themselves typing these regularly, we extend the parser. Until then, the surface stays small.
