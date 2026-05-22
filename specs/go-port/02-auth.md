# mm-cli — authentication

> What `mm login` does, where the token lives, and what every authenticated request looks like.

---

## 1. Big picture

mm-cli is a single-user CLI authenticated with a long-lived bearer token issued by `auth.meta-me.uk` via OAuth 2.0 **device flow**. There is **no HMAC**, no per-request signing, no refresh token rotation. Token is opaque, ~`mm_<40 hex>`, valid until revoked.

This is the only auth path. `auth: hub|session|either` actions on app `/api/v2` are not reachable from the CLI (tracked in `architecture.md` §4.7).

---

## 2. Login — device flow

```
                    auth.meta-me.uk           browser (Joe)
mm-cli  ──POST /api/cli/device──>
        <─ device_code, user_code, verification_uri_complete, expires_in, interval ─

mm-cli  ──open url────────────>
                                                browser opens
                                                user signs in,
                                                approves the device

mm-cli  ──POST /api/cli/token { device_code }──>
        <─ 200 { access_token, key } on approve
        <─ 4xx { error: "authorization_pending" } while waiting
        <─ 4xx { error: "expired_token" } after expires_in

mm-cli  ──POST /api/cli/validate { token }─>
        <─ { user: { id, name, email, role }, key: { id, name, scopes } }

mm-cli  ─writes ~/.config/mm/auth.json (0o600)
```

Implementation: [src/commands/login.ts](../../src/commands/login.ts) drives the poll; [src/api.ts](../../src/api.ts) wraps the three endpoints. See `01-wire.md` §1 for endpoint shapes.

### Poll cadence

- `interval` from `/device` response is the minimum poll spacing in seconds (typically 5).
- mm-cli waits exactly `interval * 1000` ms between polls.
- Timeout is `(expires_in + 10) * 1000` ms total — server-told expiry plus a buffer.

### Browser open

Best-effort via `open` (macOS) / `xdg-open` (Linux) / `start` (Windows). If the open fails (no DE, headless host) the CLI keeps polling — user can copy the `verification_uri_complete` from the printed line.

### Errors

- `authorization_pending` → keep polling (not an error).
- `expired_token` → terminal: print "Run `mm login` again", exit 1.
- Network failure → throw with the raw message, exit 1.
- Any other `error` field → throw with `error_description || error`, exit 1.

---

## 3. AuthState — on-disk

File: `~/.config/mm/auth.json`. Mode `0o600`.

```ts
interface AuthState {
  token: string;       // "mm_<40 hex>" — opaque bearer
  prefix: string;      // first 8 chars of token (for whoami display)
  userId: string;      // UUID, resolved from validate response
  userName: string;
  userEmail: string;
  createdAt: string;   // ISO timestamp at write
}
```

Implementation: [src/auth.ts](../../src/auth.ts).

- `loadAuth(): AuthState | null` — reads + parses, silently nulls on missing/corrupt.
- `saveAuth(state)` — `mkdir -p ~/.config/mm/` then writeFileSync with `0o600`.
- `clearAuth()` — overwrites with `{"loggedOut": true}` (not a delete — preserves dir + perms).

The Go port should mirror this shape exactly. Existing TS-issued tokens must stay valid after the cutover; the only thing that changes is which binary reads/writes the file.

---

## 4. Authenticated requests

Every authenticated request mm-cli sends carries **two** headers:

```
Authorization: Bearer <auth.token>
X-Hub-User-Id: <auth.userId>
```

Plus `X-Hub-Instance-Id: <uuid>` on app `/api/v2` calls when the caller passed `--instance`.

### What validates the bearer?

The token is opaque to mm-cli; only `auth.meta-me.uk` knows what it means. Every protected endpoint we hit validates it server-side:

- **Hub `/api/mm`** — hub re-validates internally via the platform auth service.
- **App `/api/v2`** — the SDK's `verifyHubRequest` accepts:
  - Session cookies (browser flow), or
  - HMAC-signed forwards from the hub
  - Neither matches our bearer. **This is the load-bearing gap.** Only `auth: public` actions (currently just `agent.card`) work for the CLI.
- **App `/api/rpc`** — kb's and crm's `handleBearerAuth` calls `meta-me-auth:8080/api/cli/validate` and uses the validation response's `user.id`. This is why bearer works on these two apps but nowhere else on `/api/v2`.

### `X-Hub-User-Id` is dead weight today

mm-cli sends `X-Hub-User-Id: <auth.userId>` everywhere alongside the bearer. Verified 2026-05-22 by reading kb and crm's hooks.server.ts:

```ts
const handleBearerAuth = async ({ event, resolve }) => {
  // ...
  const { user, instances = [] } = await res.json();
  event.locals.user = { id: user.id, ... };  // FROM VALIDATE RESPONSE, not header
};
```

So the header is informational. The Go port should still send it (matches server-side expectations for any apps that *might* read it for cheaper identity lookups), but document that it's not load-bearing.

### Logout

`mm logout` writes `{"loggedOut": true}` over `auth.json`. The next `mm` invocation reads it back as `null` (JSON.parse succeeds; the shape doesn't match AuthState; defensive code in callers treats it as "not authenticated"). The token is **not revoked server-side** — there's no `/api/cli/revoke` endpoint today. Worth fixing in the platform later; out of scope for the port.

---

## 5. Whoami

`mm whoami` reads `auth.json` and prints:

```
User:  Joe Jarlett (joe.jarlett@gmail.com)
ID:    019d7321-7b00-7b5b-874b-2b61a37c5585
Token: mm_9b2e8... (created 2026-05-14)
```

`prefix` from the saved AuthState — first 8 chars of the token, never the full thing. The Go port should match this output format byte-for-byte.

---

## 6. Open questions the port shouldn't try to solve

These exist in the platform, not in mm-cli:

1. **Server-side token revocation.** Today `mm logout` is local-only. If a token leaks you can't kill it.
2. **Token rotation.** No refresh flow. Tokens live until manually revoked (which is also local-only — see #1).
3. **Bearer-as-session bridge.** Per `architecture.md` §4.7 — the SDK needs to either accept the CLI bearer as a fourth auth mode, or the hub needs to expose `/api/mm dispatch.run` that HMAC-signs downstream calls. Without this, `mm <app> ask "..."` will keep 401'ing on `agent.chat`.

The Go port inherits these. They're noted in `06-improvements.md` and `architecture.md`; not a port-blocker.
