# CLI Agent Output Excellence Guidelines

This specification defines the standards and implementation details for optimizing the `mm-go` CLI output when consumed by automated LLM agents (such as the local agent via its bash tool) rather than human users.

---

## 1. Core Principles of Agent-Facing CLIs

Unlike human users, LLM agents process CLI outputs programmatically. A robust, resilient agent interface requires:
1. **High JSON Parity**: Every query/inspect/admin command must support the `--json` option.
2. **Clean Separation of Channels**:
   - **Stdout**: Reserved exclusively for the requested payload or parseable data.
   - **Stderr**: Used for all diagnostic logs, connection warnings, status notifications, and updates.
3. **No Terminal Pollution**: ANSI escape sequences (colors, bold texts), terminal loaders, or interactive prompts must be suppressed if an agent context is detected.
4. **Non-Interactive Fallbacks**: Any potentially blockable operation must fail cleanly with a standard exit code rather than pausing to await keyboard input.

---

## 2. Standardized JSON Output Format

When `--json` is specified, the CLI must:
* Emit exactly one valid JSON object or array to `os.Stdout` (with zero trailing or leading diagnostic texts).
* Fail with a structured JSON envelope if an error occurs.
* Include status flags for authorization checks and missing inputs.

### Example: `mm whoami --json`
```json
{
  "authenticated": true,
  "userName": "Joe Jarlett",
  "userEmail": "joe.jarlett@gmail.com",
  "userId": "019d7321-7b00-7b5b-874b-2b61a37c5585",
  "prefix": "mm_9b2e8",
  "createdAt": "2026-05-14"
}
```

### Example: `mm status --json`
```json
{
  "authenticated": true,
  "userName": "Joe Jarlett",
  "userEmail": "joe.jarlett@gmail.com",
  "prefix": "mm_9b2e8",
  "apps": [
    {"slug": "kb", "name": "Knowledge Base", "description": "search, read, manage documents"},
    {"slug": "crm", "name": "CRM", "description": "contacts, projects, interactions"}
  ]
}
```

---

## 3. Detecting Automated Environments

The CLI utilizes the following environment variables to auto-detect if it is running within an LLM or script environment:
* `MM_AGENT=true`
* `MM_SOURCE=agent`

When detected:
1. **ANSI Escape Codes are disabled** (no colorized outputs or bold markers).
2. **Reconnection progress spinner** is replaced by minimal, structured log records written strictly to Stderr.
3. **Prompts fail fast**: Any terminal command requiring manual confirmation or OAuth redirection fails instantly with a clean error message and standard exit status.

---

## 4. Error Code Design

To allow agents to programmatically handle failures without resorting to complex text parsing, the CLI maps specific errors to standard shell exit codes:

| Scenario | Exit Code | JSON Error Payload (if `--json`) |
|---|---|---|
| General execution failure | `1` | `{ "error": "<message>" }` |
| Unauthorized / Session Expired | `1` | `{ "authenticated": false, "error": "Session expired..." }` |
| Invalid Command Arguments | `2` | Standard Cobra usage response |
| API / Connection Timeout | `124` | `{ "error": "connection timed out" }` |
| Missing Dependency / Helper Binary | `127` | `{ "error": "afplay not installed" }` |

---

## 5. Implementation Status Checklist

- [x] Standardize `--json` on all generic dispatch commands.
- [x] Implement `--json` format support on `mm whoami` command.
- [x] Implement `--json` format support on `mm status` command.
- [x] Divert all WebSocket reconnection alerts and cursor logs to Stderr in `chat_send.go`.
- [x] Auto-suppress colors and terminal spinners when `MM_AGENT` is configured.
