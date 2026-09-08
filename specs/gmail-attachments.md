# Gmail attachments — `mm email attachments` / `mm email download`

**Status:** **live** as of 2026-09-08 — gateway, hub and CLI all deployed, and verified
end to end against a real message. Companion to [gws-cli-upgrades.md](gws-cli-upgrades.md),
which closed the Gmail draft/send/trash gap the same way.

The first deploy shipped a bug that made `download` fail on every message; see
*[Gmail's attachment ids are per-response](#gmails-attachment-ids-are-per-response)* below,
which is the part of this doc worth reading if you touch attachments again.

**Problem:** `mm email search` and `mm email read` cover the inbox, but an attachment could
only be retrieved by hand in the browser. `mm email --help` had no verb for it.

Unlike the draft/send work, this one *did* need a gateway change: Gmail returns
`payload.parts[].body.attachmentId` and withholds the bytes for anything file-shaped, so
`users.messages.attachments.get` has to be reachable. It wasn't.

**Scope:** no new OAuth scope. The gateway's `email` bundle is `gmail.modify`, which already
covers attachment reads.

---

## The three layers

### 1. Gateway — `src/gmail_router.py`

```
GET /gmail/messages/{message_id}/attachments/{attachment_id}
  → { attachment_id, size, data }
```

`data` is base64**url** (Gmail's own alphabet), passed through unre-encoded so it is decoded
exactly once, at the CLI. Gated by the existing `EmailDep`.

### 2. Hub — `src/lib/server/mm/inbox.ts`

Two actions beside `email.search` / `email.read`:

```
email.attachments({id, accountSlug?})
  → { id, subject, attachments: [{partId, attachmentId, filename, mimeType, size, inline}] }
email.attachment({id, partId, accountSlug?})
  → { id, partId, attachmentId, filename, mimeType, size, data }
```

`attachments` walks the MIME tree of a `format: 'full'` read and reports every part carrying a
filename — `inline: true` marks embedded images (signatures, logos) rather than real files.
`attachment` re-reads the message so it can name the file and enforce a 25 MB cap *before*
pulling bytes, then falls back to the part's own `body.data` when Gmail inlined it.

The part is addressed by **`partId`** — its MIME position in the message (`1`, `0.2`, …).
`attachmentId` is accepted as a fallback selector but must not be relied on; see below.

Both are added to `forbiddenOnPlatformWide` in `src/routes/api/mm/+server.ts`: like
`email.read`, they read the caller-userId's actual Gmail, so they must not be reachable over
the platform-wide shared secret.

### 3. CLI — `internal/cmd/email.go`

```bash
mm email attachments <gmail-message-id>            # [part id] filename, mime type, size
mm email download <gmail-message-id> \
    [--part <part-id> | --name <substring> | --all] \
    [--include-inline] [--out <dir>] [--account <slug>]
```

`download` with no selector saves the message's single attachment; with several it refuses and
names them rather than dumping the lot unasked. `--part` takes the bracketed id from the
listing; `--name` matches a filename substring case-insensitively and refuses an ambiguous
match. Either one reaches an inline part without `--include-inline`, since naming a specific
part is unambiguous. `--out` defaults to the current directory and is created if missing.
Both support `--json`.

**Two safety rules, because filenames come from the sender:**

- `safeFilename` reduces the mail-supplied name to a plain basename and scrubs it, so
  `../../etc/passwd` writes `passwd` into `--out`, never outside it.
- `uniquePath` never overwrites — an existing `report.pdf` becomes `report-1.pdf`.

Neither command ever prints file contents.

---

## Not in scope

- **Streaming.** Bytes ride the JSON contract as base64, capped at Gmail's own 25 MB ceiling.
  A streaming path through the hub would be the fix if that cap ever bites; nothing needs it
  today, and `drive.download` has the same shape.
- **Attaching files on send.** The gateway's `SendEmailRequest` already accepts `attachments`;
  `mm email send` doesn't expose them. Separate gap, separate change.

---

## Gmail's attachment ids are per-response

The first deploy of this feature shipped a `download` that failed on **every** message, and the
reason is worth recording because nothing in Gmail's docs leads with it:

> `payload.parts[].body.attachmentId` is regenerated on every `users.messages.get`.

Two reads a second apart return different `attachmentId` strings for the same part. It is a
per-response token, not a durable handle:

```bash
mm email attachments 19f6f9b0fad82fdd --json | jq -r '.attachments[].attachmentId'
# ANGjdJ8_G-K5Eyug9olC…
mm email attachments 19f6f9b0fad82fdd --json | jq -r '.attachments[].attachmentId'
# ANGjdJ9yqwOSXO5If8zD…   ← same part, different id
```

The original design had the CLI list the parts (read 1), then pass the chosen `attachmentId`
back to `email.attachment`, which re-read the message (read 2) and searched read 2's MIME tree
for read 1's id. That never matched, so every download 404'd with
`no attachment <id> on message <id>` — the hub's own error, not Gmail's, which is what made it
look like a lookup bug rather than an id-lifetime one.

**The fix keeps the re-read and changes the handle.** The re-read is what names the file and
enforces the 25 MB cap *before* any bytes are pulled; the alternative — carrying filename and
size through from the caller's listing — would make a server-side size limit depend on a
client-supplied number, which is not a limit. So instead the part is addressed by `partId`,
its stable MIME position, and the handler calls the gateway with the `attachmentId` from **its
own** read — the only one guaranteed to still resolve.

Consequences, if you touch this again:

- `partId` is the handle in the payload, the CLI flag (`--part`) and the listing output. The
  listing no longer prints the 400-character `attachmentId` blob at all; it is still in
  `--json`, marked volatile, and is echoed in the response as the id actually used.
- Part ids are **not** always integers — nested multipart messages give `0.2`, `1.1`. Anything
  parsing them as numbers is wrong.
- `internal/cmd/email_attachments_test.go` has `TestDownloadAddressesPartsByPartID`, which
  fails if the download request ever carries `attachmentId` again. Its `att()` helper sets
  every fixture's `AttachmentID` to `volatile-<partId>` so no test can accidentally depend on it.

Verified end to end against a Brunel Incubator newsletter carrying a PDF and a calendar invite:

```bash
mm email attachments 19f6f9b0fad82fdd
mm email download 19f6f9b0fad82fdd --all --out ~/Downloads
mm email download 19f6f9b0fad82fdd --part 1
mm email download 19f6f9b0fad82fdd --name infographic
```

Both files came back with correct bytes (`file` reports a 1-page PDF v1.4 and a vCalendar),
the second fetch of the same name landed as `…-1.pdf` rather than overwriting, and each
error path (unknown part, unmatched name, both selectors, no selector with several parts)
reports what to do next.

## Deploy order

Gateway before hub — the hub calls the gateway endpoint, so a hub-first deploy leaves a window
where `email.attachment` 404s against the old gateway.

```bash
cd ~/Documents/dev/google-workspace-gateway && bash scripts/deploy.sh   # gateway FIRST
cd ~/Documents/dev/meta-me.uk && npm run deploy                          # then the hub
```

## Also fixed — the CLI parity test

`TestCliDrift` was **dead and failing on a clean tree**: it read `src/index.ts`, deleted in the
2026-06-11 TS→Go port, so it aborted before its first assertion and was the only red test in
`go test ./...` — masking real breakage. It is gone, along with the duplicate command tree it
built to compare against (`dummyRootCmd`), which had itself drifted: no `overview`, `surface`,
`host` or `convert`, and none of the registry-driven app commands.

Replaced by `cmd/mm/root_test.go`, which asserts against the **real** `newRootCmd()` rather than
a copy of it:

- `TestRootCommandSurface` — a hand-maintained golden list of every top-level verb and alias, so
  adding or removing one is a deliberate diff in review, not a silent side effect.
- `TestEveryRootCommandIsGrouped` — a bare `root.AddCommand` compiles and runs but drops the
  command into Cobra's anonymous "Additional Commands" heading; this catches the bypass.
- `TestNoShadowedRootNames` — Cobra resolves a duplicate name or alias silently by registration
  order, leaving one command unreachable.
- `TestRootHelpListsEveryCommand` — ties the golden list to what `mm --help` actually prints.

All four were mutation-checked (dropped registration, bypassed `add()`, alias collision) to
confirm they fail when they should.
