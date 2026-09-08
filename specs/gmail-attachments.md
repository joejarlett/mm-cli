# Gmail attachments — `mm email attachments` / `mm email download`

**Status:** implemented 2026-09-08. Companion to [gws-cli-upgrades.md](gws-cli-upgrades.md),
which closed the Gmail draft/send/trash gap the same way.

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
  → { id, subject, attachments: [{attachmentId, partId, filename, mimeType, size, inline}] }
email.attachment({id, attachmentId, accountSlug?})
  → { id, attachmentId, filename, mimeType, size, data }
```

`attachments` walks the MIME tree of a `format: 'full'` read and reports every part carrying a
filename — `inline: true` marks embedded images (signatures, logos) rather than real files.
`attachment` re-reads the message so it can name the file and enforce a 25 MB cap *before*
pulling bytes, then falls back to the part's own `body.data` when Gmail inlined it.

Both are added to `forbiddenOnPlatformWide` in `src/routes/api/mm/+server.ts`: like
`email.read`, they read the caller-userId's actual Gmail, so they must not be reachable over
the platform-wide shared secret.

### 3. CLI — `internal/cmd/email.go`

```bash
mm email attachments <gmail-message-id>            # filename, mime type, size, attachment id
mm email download <gmail-message-id> \
    [--attachment-id <id> | --all] \
    [--include-inline] [--out <dir>] [--account <slug>]
```

`download` with no selector saves the message's single attachment; with several it refuses and
names them rather than dumping the lot unasked. `--out` defaults to the current directory and
is created if missing. Both support `--json`.

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
