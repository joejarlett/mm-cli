# GWS CLI Upgrades — Gmail & Calendar

**Goal:** Close the gaps that force an agent to call the GWS gateway directly via `curl`. After this, `mm email` and `mm calendar` cover the full agent workflow without raw HTTP fallbacks.

**Background:** In the current implementation:
- `mm email draft/send` routes to the **platform email log** (hub DB + platform SMTP), never Gmail. There is no `--from` flag and no way to create an actual Gmail draft.
- `mm calendar` has `list` and `new` but no `delete` or `get`. Removing a wrong calendar entry currently requires a direct `DELETE /calendar/events/{id}` call to the gateway.

The GWS gateway (`localhost:8222`) already exposes all the required endpoints — the work is wiring them through hub handlers and CLI commands.

---

## Scope

Three separate gaps, each with a hub change + CLI change.

### 1. Gmail draft & send via `mm email`

**Problem:** `mm email draft --to X --subject Y --body Z` saves a platform log row (`from: joe@meta-me.uk`). There is no path to create a real Gmail draft or send from `joe.jarlett@gmail.com`.

**Solution:** Add `--from` to `mm email draft` and `mm email send`. When `--from` is provided, bypass the platform log entirely and proxy to the gateway.

#### Hub — `email.ts`

Add two new actions alongside the existing ones:

```
gmail.draft   → POST /gmail/drafts  on the gateway
gmail.send    → POST /gmail/send    on the gateway
gmail.trash   → PUT  /gmail/messages/{id}/trash  on the gateway
```

Payload shape mirrors the gateway `DraftRequest` / `SendRequest`:
```ts
{ to, subject, body, body_type?, from?, cc?, bcc?, reply_to? }
```

`from` is passed as the `X-Gateway-Account` header (or inline in the body — check how `calendar.ts` passes account selection to the gateway).

#### CLI — `email.go`

- Add `--from` flag to `addSendFlags()`.
- In `runEmailSend()`: if `--from` is set, call `client.Hub(ctx, "email", "gmail.draft"|"gmail.send", req, &resp)` instead of the platform-log path.
- Add `mm email trash <gmail-message-id>` subcommand → `client.Hub(ctx, "email", "gmail.trash", {id}, &resp)`.

**Output for `mm email draft --from ...`:**

```
✓ Gmail draft saved (to: nora@example.com)
  Message ID: 19e69b1b62194072
```

**Output for `mm email trash <id>`:**

```
✓ Trashed: 19e69b1b62194072
```

---

### 2. `mm calendar delete <event-id>`

**Problem:** No way to delete a calendar event from the CLI.

**Solution:**

#### Hub — `calendar.ts`

Add a `delete` action:
```ts
case 'delete':
  // payload: { eventId, accountSlug? }
  await deleteEvent(payload.eventId, payload.accountSlug)
  return { deleted: true, eventId: payload.eventId }
```

Wire `deleteEvent` to `DELETE /calendar/events/{eventId}` on the gateway (same pattern as existing `createEvent` / `listEvents`).

#### CLI — `calendar.go`

```
mm calendar delete <event-id>  [--account <slug|email>]
```

- `Args: cobra.ExactArgs(1)`
- Calls `client.Hub(ctx, "calendar", "delete", {eventId, accountSlug?}, &resp)`
- Output: `✓ Deleted event: <id>`

---

### 3. `mm calendar get <event-id>`

**Problem:** No way to inspect a single event by ID (needed to verify before deleting, or to surface the event ID after creation).

**Solution:**

#### Hub — `calendar.ts`

Add a `get` action:
```ts
case 'get':
  // payload: { eventId, accountSlug? }
  const event = await getEvent(payload.eventId, payload.accountSlug)
  return { event }
```

Wire to `GET /calendar/events/{eventId}` on the gateway.

#### CLI — `calendar.go`

```
mm calendar get <event-id>  [--account <slug|email>]
```

- Output (default): one-line summary matching `mm calendar list` format
- Output (`--json`): full event object

---

## Wire types

Add to `wire/` as needed:

```go
// GmailDraftResp
type GmailDraftResp struct {
    ID        string `json:"id"`
    MessageID string `json:"messageId"`
}

// GmailTrashResp
type GmailTrashResp struct {
    ID      string `json:"id"`
    Trashed bool   `json:"trashed"`
}

// CalendarDeleteResp
type CalendarDeleteResp struct {
    Deleted bool   `json:"deleted"`
    EventID string `json:"eventId"`
}

// CalendarGetResp
type CalendarGetResp struct {
    Event HubCalendarEvent `json:"event"`
}
```

---

## What is NOT in scope

- `mm calendar update` — useful but not urgent; omit for now.
- `mm gmail` as a separate top-level command tree — unnecessary; `mm email` with `--from` is the right surface.
- Platform email log changes — leave untouched; the log is still useful for platform-sent mail.
- Gateway changes — the gateway already exposes all required endpoints. No gateway work needed.

---

## Files to touch

| File | Change |
|------|--------|
| `meta-me.uk/src/lib/server/mm/calendar.ts` | Add `delete` and `get` actions, wire to gateway |
| `meta-me.uk/src/lib/server/mm/email.ts` | Add `gmail.draft`, `gmail.send`, `gmail.trash` actions, proxy to gateway |
| `mm-cli/internal/cmd/calendar.go` | Add `delete` and `get` subcommands |
| `mm-cli/internal/cmd/email.go` | Add `--from` flag, `trash` subcommand, Gmail routing in `runEmailSend` |
| `mm-cli/internal/wire/` | Add `GmailDraftResp`, `GmailTrashResp`, `CalendarDeleteResp`, `CalendarGetResp` |

---

## Acceptance criteria

```bash
# Create a Gmail draft from Joe's Gmail address
mm email draft --from joe.jarlett@gmail.com --to nora@example.com \
  --subject "Test" --body "Hi Nora"
# → ✓ Gmail draft saved (to: nora@example.com) / Message ID: ...

# Trash a Gmail message
mm email trash 19e69b1b62194072
# → ✓ Trashed: 19e69b1b62194072

# Delete a calendar event
mm calendar delete 5dktn761surtrs98n2jomjlb84
# → ✓ Deleted event: 5dktn761surtrs98n2jomjlb84

# Inspect an event by ID
mm calendar get _60q30c1g...
# → Fri 29 May 10:00–11:00  Jane Doe - Project Status Sync
```
