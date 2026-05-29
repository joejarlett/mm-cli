#!/usr/bin/env bash
#
# smoke-kb.sh — exercise the `mm kb` verb surface against live kb.
#
# Seeds two throwaway notebooks + a doc, then runs every verb by full id,
# short id, and name, asserting each returns the expected output and never
# an HTML/500 page. Cleans up after itself (incl. on failure). Exits non-zero
# if any check fails — safe to wire into a schedule.
#
#   ./scripts/smoke-kb.sh            # uses the `mm` on PATH
#   MM=/path/to/mm ./scripts/smoke-kb.sh
#
set -uo pipefail

MM=${MM:-mm}
NB="zz-mm-smoke"      # primary throwaway notebook
NB2="zz-mm-smoke-2"   # move target
PASS=0
FAIL=0

note() { printf '\n\033[1m%s\033[0m\n' "$1"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; PASS=$((PASS + 1)); }
bad()  { printf '  \033[31m✗ %s\033[0m\n' "$1"; FAIL=$((FAIL + 1)); }

# uuid / short-id extraction from command output
uuid()  { grep -oiE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1; }

# expect <desc> <expect-substr> -- <cmd...>
#   passes if output contains <expect-substr> and is NOT an HTML/500 page.
expect() {
	local desc=$1 want=$2; shift 2; [ "${1:-}" = "--" ] && shift
	local out; out=$("$@" 2>&1)
	if printf '%s' "$out" | grep -qiE '<!doctype|<html|Internal Error'; then
		bad "$desc — got an HTML/500 page"; return
	fi
	if [ -n "$want" ] && ! printf '%s' "$out" | grep -qiF "$want"; then
		bad "$desc — missing '$want' (got: $(printf '%s' "$out" | head -1))"; return
	fi
	ok "$desc"
}

# expect_err <desc> <expect-substr> -- <cmd...>
#   passes if the command fails with a CLEAN error containing <expect-substr>
#   (and crucially NOT an HTML/500 page).
expect_err() {
	local desc=$1 want=$2; shift 2; [ "${1:-}" = "--" ] && shift
	local out; out=$("$@" 2>&1)
	if printf '%s' "$out" | grep -qiE '<!doctype|<html|Internal Error'; then
		bad "$desc — error surfaced as an HTML/500 page"; return
	fi
	if ! printf '%s' "$out" | grep -qiF "$want"; then
		bad "$desc — expected clean error '$want' (got: $(printf '%s' "$out" | head -1))"; return
	fi
	ok "$desc"
}

# purge_nb <name> — remove EVERY notebook with this exact name (and its docs).
# Parses the markdown `tree` (lines: `- **<name>** — N docs \`<uuid>\``);
# loops because a crashed prior run can leave duplicates, which would make
# name resolution ambiguous. Exact match via the `**name**` delimiters.
purge_nb() {
	local nb=$1 cid
	for cid in $($MM kb tree 2>/dev/null | grep -F "**$nb**" | grep -oiE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}'); do
		for d in $($MM kb tree "$cid" 2>/dev/null | grep -oiE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}'); do
			$MM kb rm "$d" >/dev/null 2>&1
		done
		$MM kb collections remove id="$cid" >/dev/null 2>&1
	done
}

cleanup() {
	note "Cleanup"
	purge_nb "$NB"
	purge_nb "$NB2"
	printf '\n\033[1mResult: %d passed, %d failed\033[0m\n' "$PASS" "$FAIL"
	[ "$FAIL" -eq 0 ] || exit 1
}
trap cleanup EXIT

# ─── Setup ───────────────────────────────────────────────────────────────
note "Setup"
purge_nb "$NB"; purge_nb "$NB2"   # clear any orphans from a crashed prior run
NBID=$($MM kb collections create name="$NB" 2>&1 | uuid)
NB2ID=$($MM kb collections create name="$NB2" 2>&1 | uuid)
[ -n "$NBID" ] && ok "created notebook $NB ($NBID)" || bad "could not create $NB"
[ -n "$NB2ID" ] && ok "created notebook $NB2" || bad "could not create $NB2"

ADD=$($MM kb add "$NB" content="smoke body for the verb harness" title="smoke-doc" 2>&1)
DOCID=$(printf '%s' "$ADD" | uuid)
SHORT=${DOCID:0:8}
[ -n "$DOCID" ] && ok "added smoke-doc ($DOCID)" || bad "could not add smoke-doc"

# An ESTABLISHED doc (has chunked content) for the read checks — a freshly
# added doc's content is empty until the async chunking pipeline runs, so
# reading the new doc would race it. Pull the first doc from the largest
# notebook (skip the header line, which carries the notebook's own id).
EST_NB=${EST_NB:-Marketing Research}
ESTID=$($MM kb tree "$EST_NB" 2>/dev/null | grep '^- ' | grep -oiE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1)
[ -n "$ESTID" ] && ok "established doc for read checks ($ESTID)" || bad "no established doc found in '$EST_NB'"

# ─── Navigation ──────────────────────────────────────────────────────────
note "Navigation verbs"
expect "tree (all notebooks)"        "Notebooks"  -- $MM kb tree
expect "tree <notebook name>"        "smoke-doc"  -- $MM kb tree "$NB"
expect "peek <notebook name>"        "$NB"        -- $MM kb peek "$NB"
expect "peek <full doc id>"          "smoke-doc"  -- $MM kb peek "$DOCID"
expect "peek <short doc id>"         "smoke-doc"  -- $MM kb peek "$SHORT"
expect "peek <doc name>"             "smoke-doc"  -- $MM kb peek "smoke-doc"
expect "read <established full id>"  "Wrote"      -- $MM kb read "$ESTID"
expect "find (corpus)"               "Search"     -- $MM kb find "smoke body verb harness"
expect "find in=<notebook>"          "Search"     -- $MM kb find "smoke" in="$NB"
expect "related <full id>"           "Related"    -- $MM kb related "$DOCID"
expect "actions (introspection)"     "RPC surface" -- $MM kb actions

# ─── Mutation (by short id, then by name) ────────────────────────────────
note "Mutation verbs"
expect "rename <short id>"           "Renamed document"  -- $MM kb rename "$SHORT" "smoke-doc-r"
expect "rename <name>"               "Renamed document"  -- $MM kb rename "smoke-doc-r" "smoke-doc-2"
expect "tag <name>"                  "Tagged"            -- $MM kb tag "smoke-doc-2" smoke-label
expect "move <name> to <notebook>"   "Moved"             -- $MM kb move "smoke-doc-2" to "$NB2"
expect "move back"                   "Moved"             -- $MM kb move "smoke-doc-2" to "$NB"
expect "rm <short id>"               "Removed"           -- $MM kb rm "$SHORT"
expect_err "peek removed doc"        "no document"       -- $MM kb peek "$SHORT"

# ─── Error contract (must be CLEAN, never HTML) ──────────────────────────
note "Error contract"
expect_err "research create bad collectionId" "collectionId" -- $MM kb research create collectionId=not-a-uuid prompt="x"
expect_err "peek bogus full uuid"    "not found"  -- $MM kb peek 00000000-0000-7000-0000-000000000000
expect_err "rm nonexistent name"     "no document" -- $MM kb rm "definitely-not-a-real-doc-xyz"
expect_err "unknown action"          "nknown"     -- $MM kb documents bogusaction
