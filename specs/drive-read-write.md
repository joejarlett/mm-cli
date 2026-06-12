# Drive CLI Surface — read / export (and write status)

**Goal:** Let an agent (or user) **read the contents of a Drive file** through `mm drive` — so reports saved to Drive (Gemini Deep Research, NotebookLM, shared Docs) can be ingested into a repo or the KB without a `curl` fallback or a manual download.

**Background — the backend already exists; only the CLI surface is missing.**

The whole pipeline for reading Drive content is already built and deployed:

- **Gateway** (`google-workspace-gateway/src/drive_router.py`):
  - `GET /drive/files/{id}/export` — export a Google-native file (Doc/Sheet/Slide) to a target mime (e.g. `text/markdown`, `text/plain`, `application/pdf`).
  - `GET /drive/files/{id}/download` — download raw file content (non-Google files).
  - `GET /drive/files/{id}` — file metadata.
- **Hub** (`meta-me.uk/src/lib/server/mm/drive.ts`) already exposes the matching actions:
  - `drive.export({ fileId, mime? }) → { content, mimeType }`
  - `drive.get({ fileId }) → { …metadata }`
  - `drive.download({ fileId }) → { content, … }`
  - (plus existing `drive.list`, `drive.createDoc`, `drive.update`)

**The only gap:** `mm drive` (`internal/cmd/drive.go`) wires up just `ls`, `doc`, and `mv`. It never surfaces `drive.export` / `drive.get` / `drive.download`. So `mm drive` can *list* a file and see its title, but cannot read its body — forcing an agent to fall back to WebFetch (which 401s on private Drive files) or ask the user to download manually.

This spec is therefore a **CLI-only change** (no hub or gateway work for read). Low risk, high leverage.

---

## Status — built & shipped (2026-06-04)

Sections 1–3 are **implemented** in `internal/cmd/drive.go` (commands `read`, `get`,
`download`) with table-driven tests in `internal/cmd/drive_test.go`. Verified live
against a real Doc. Two things differ from the original draft below — read these first:

1. **The hub payload field is `mimeType`, not `mime`.** The original snippet sent
   `{"fileId", "mime"}`; the hub (`drive.export`) ignores `mime` and would silently
   default to `text/html`. The shipped code sends `mimeType`.
2. **`--as md` does not work today** — the gateway export path returns **HTTP 503**
   for `text/markdown`. `txt` and `html` work; `pdf` returns raw bytes (use with
   `--out`, since stdout would be binary). So the shipped default is **`txt`**, not
   `md`. See "High-fidelity markdown" below for the real fix.

## Scope

### 1. `mm drive read <id|url>` — export a Google Doc/Sheet/Slide to text *(primary, shipped)*

```
mm drive read <id|url> [--as txt|html|pdf|md|<full/mime>] [--out <path>] [--account <slug|email>]
```

- `--as` selects the export mime (**default `txt`**):
  | `--as` | mime | works today? |
  |--------|------|--------------|
  | `txt`  | `text/plain` | ✅ (default) |
  | `html` | `text/html` | ✅ |
  | `pdf`  | `application/pdf` | ✅ raw bytes — use with `--out` |
  | `md`   | `text/markdown` | ❌ gateway 503 — see follow-up |
  | `<full/mime>` | passed through verbatim | escape hatch, e.g. `text/csv` for a Sheet |
- No `--out` → print `content` to stdout (pipes / inline-readable by an agent).
- `--out <path>` → write `content` to the file; confirmation goes to **stderr**
  (stdout stays clean for piping).
- Accepts a bare id or a pasted URL — `driveFileID()` strips `.../d/<id>/...`
  (Docs/Sheets/Slides/Drive file links) and `...?id=<id>` (open/uc links).

Calls `client.Hub(ctx, "drive", "export", {fileId, mimeType, accountSlug?}, &resp)`
where `resp` is `wire.HubDriveExportResp{Content, MimeType}`.

> The legacy TS impl (`src/commands/drive.ts`) was **not** updated — the Go binary
> is the live CLI; the TS path is dev-only and out of scope here.

### 2. `mm drive get <id|url>` — file metadata *(shipped)*

Wrapper over `drive.get`. Prints name, type, size, modifiedTime, link, parents.
Requests `fields=...,size` explicitly (the hub default omits size). Native Docs
report no size — expected. Use to confirm a file before reading, and to decide
`read` (Google-native) vs `download` (binary).

```
mm drive get <id|url> [--account <slug|email>]
```

### 3. `mm drive download <id|url> [--out <path>]` — raw non-Google files *(shipped)*

Wrapper over `drive.download` for files that aren't Google-native. **Caveat:** the
hub returns content via `res.text()`, so this is only safe for text-ish files
(`.md`, `.csv`, `.txt`). True binary (PDF/images/docx) is UTF-8-corrupted in transit
— see the binary-transport note below.

---

## High-fidelity markdown — the real follow-up (NOT built)

The original spec assumed `--as md` would give clean markdown. It doesn't, and the
better answer is to route through the **docling convert container** we already run,
rather than rely on Google's markdown export.

**What exists:** `mm convert <file>` (`internal/cmd/convert.go`) posts a file to a
local **docling-serve** container (`http://localhost:5001`, override `MM_CONVERT_URL`)
and returns excellent structured markdown for docx/xlsx/pptx/pdf — deterministic,
offline, content-hash cached under `~/.mm/convert-cache`. This is the markdown brain;
it just needs a clean file to chew on.

**Why we can't join them yet — the binary-transport gap.** docling needs a real
docx/pdf. Exporting a Doc as docx through the current hub produces a *corrupt* file:
the hub's `exportFile` (`meta-me.uk/src/lib/server/gateway.ts`) and `drive.download`
both return `res.text()`, which UTF-8-mangles binary bytes (verified: an exported
docx fails `unzip` with "missing ~4GB of bytes"; the 503 on `text/markdown` is a
separate gateway issue). So "Drive Doc → docx → docling → markdown" is blocked at the
first hop.

**The fix (gateway + hub, ~half a day):**
- **Gateway:** add a binary-safe export — either base64-encode the export/download
  bytes, or a sibling endpoint that returns `{ content_b64, mimeType }`. Kills the
  `res.text()` corruption.
- **Hub:** base64-decode in `drive.export` / `drive.download` (or add
  `drive.exportBinary`). Return bytes faithfully.
- **CLI:** `mm drive read --as md` then internally: export Doc → docx (binary-safe) →
  POST to docling (:5001) → structured markdown. Reuses the exact engine `mm convert`
  uses. Could also expose `mm drive read <id> | mm convert -` once convert reads stdin.

Until then: **`mm drive read --as txt` is the working path** for "agent reads a Doc",
and `mm convert <local-file>` is the path for files already on disk.

---

## Write — current status (mostly already covered)

"Write" largely exists: **`mm drive doc <name> --file x.md`** already creates a native Google Doc from markdown (`drive.createDoc`), and **`mm drive mv`** renames/reparents (`drive.update`).

The one genuine gap is **overwriting the body of an *existing* Doc**. The gateway currently has `POST /drive/files/upload` (new file) and `PATCH /drive/files/{id}` (metadata only) — there is **no update-content endpoint**. So in-place body updates would need a small backend addition first:

- **Gateway:** add `PATCH /drive/files/{id}/content` (or `?uploadType=media`) → Drive `files.update` with a media body, converting markdown→Doc as `createDoc` does.
- **Hub:** add `drive.updateContent({ fileId, content, sourceMime? })`.
- **CLI:** `mm drive write <id> --file x.md`.

*Treat the write-update section as direction, not contract — confirm the gateway has no media-update path before building, and only do it if a real need appears. The read side (sections 1–3) is the actual ask and is pure CLI wiring.*

---

## Why this matters

Unblocks the common loop "*research/notes saved to a Google Doc → ingest into the repo or KB*" — e.g. pulling a Gemini Deep Research report (`Building a "Beacon" for Belonging`) straight into `research/explorations/` with `mm drive read <id> --out beacon.txt`, instead of manual export. (Today that lands as flat text; the docling follow-up above upgrades it to structured markdown.) Filed motivation: platform feedback `LlF7XNYR4CLkgINo7Gz8o`.
