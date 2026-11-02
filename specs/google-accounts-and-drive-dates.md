# Linked Google accounts, and future dates in `mm drive ls`

**Status:** Built 2 Nov 2026. Two small, separate fixes, found the same day while testing Google services after a reconnect. `mm drive ls` order and `--account` with a printed slug checked live. The email column in `mm accounts` needs the next meta-me.uk deploy (690a7b6).

## 1. Future-dated files show as "just now"

**The problem.** `mm drive ls` listed `ducks4.aif` and `plane_announcement.aif` at the top as "just now". They were not recent: `ducks4.aif` was created on 11 Jan 2021 in a folder shared by another account, but Drive holds its `modifiedTime` as 2033-07-05, and `plane_announcement.aif` as 2036-06-02. An agent read this as fresh uploads and raised a false alarm. They don't appear in Drive's own Recent view.

**Two causes:**
- The relative-time helper in `internal/cmd/drive.go` (`diff := time.Since(t)`, then `case diff < time.Minute: return "just now"`) treats any negative difference as "just now".
- The hub lists with `orderBy: 'modifiedTime desc'` (`meta-me.uk/src/lib/server/mm/drive.ts`), so future-dated files always take the top of the default list and can push real recent files off it.

**Fix:**
- **CLI (mm-cli):** when the time is in the future, show the actual date with a marker, for example `2033-07-05 (future date)`. Check `internal/cmd/email.go` and `desk_overview`, which also have "just now" code, and share one helper if they do the same thing.
- **Hub (meta-me.uk):** when no `--q` is given, stop future-dated files crowding out recent ones. Either add `modifiedTime <= now` to the default query, or over-fetch and move them to the end. Prefer moving them, so that no file silently disappears from a listing. `--q` searches stay as they are.

**Done when:**
- With a stubbed future timestamp, `mm drive ls` shows the date and the marker, not "just now", with a test.
- `mm drive ls` on Joe's account shows genuinely recent files first.
- Past times format exactly as before.

## 2. No way to list linked Google accounts

**The problem.** `mm email`, `calendar`, `drive` and `tasks` all take `--account` ("Pick a linked Google account"), but nothing lists the accounts to pick from. `mm accounts` returns `unknown command "accounts" for "mm"`, and `mm status` doesn't show them, although `joe-inc/CLAUDE.md` says `mm accounts` lists them. `mm drive ls --json` returns an opaque `accountSlug` (`acc_019d7321`). Filed as feedback `1z4J_sQMFzqShxgl7aE85`.

**Fix:** add `mm accounts`, in the Account help group, showing for each linked Google account:
- its email address
- the slug to pass to `--account`
- which is the default
- whether its login is still valid, if the hub can tell

This would also have helped on 2 Nov, when eight "hub unavailable (HTTP 502)" reports were cleared by reconnecting Google. Add `--json`, as with other commands.

**Check first:** the gateway has an `/accounts` route (see `meta-me.uk/specs/google-oauth-verification-guide.md`). Confirm whether the hub already exposes it to the CLI. If it doesn't, this needs a hub handler in meta-me.uk as well. If login validity isn't cheaply available, leave that field out rather than guessing.

**Done when:**
- `mm accounts` lists Joe's linked account or accounts, and a slug it prints works with `--account`.
- With no linked accounts, it says so and points to how to connect one.
- `joe-inc/CLAUDE.md` is then accurate, with no edit needed.

## Out of scope

- Correcting the dates on the Drive files themselves. They belong to another account.
- Changing how Google login or reconnecting works.
