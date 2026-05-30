#!/usr/bin/env bash
#
# smoke-apps.sh — cross-app surface check for `mm`.
#
# Verifies, cheaply and without mutating anything, that every registered app
# is reachable and self-documenting: card render, --help lists the verbs,
# `use` lists instances, and (one call) the auth+instance+render path works
# end-to-end. Catches the regressions that bite an LLM operator — 401s,
# HTML/500 pages, missing verbs in help — across the whole platform, not
# just kb. Pair with smoke-kb.sh (the deep kb verb sweep).
#
#   ./scripts/smoke-apps.sh        # uses the `mm` on PATH
#
set -uo pipefail
MM=${MM:-mm}
PASS=0
FAIL=0

note() { printf '\n\033[1m%s\033[0m\n' "$1"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; PASS=$((PASS + 1)); }
bad()  { printf '  \033[31m✗ %s\033[0m\n' "$1"; FAIL=$((FAIL + 1)); }

# expect <desc> <want> -- <cmd...>  (clean output containing <want>, never HTML)
expect() {
	local desc=$1 want=$2; shift 2; [ "${1:-}" = "--" ] && shift
	local out; out=$("$@" 2>&1)
	if printf '%s' "$out" | grep -qiE '<!doctype|<html|Internal Error'; then
		bad "$desc — HTML/500 page"; return
	fi
	if printf '%s' "$out" | grep -qiE '\b(401|unauthorized|requires .* auth)\b'; then
		bad "$desc — auth failure ($(printf '%s' "$out" | head -1))"; return
	fi
	if [ -n "$want" ] && ! printf '%s' "$out" | grep -qiF "$want"; then
		bad "$desc — missing '$want' (got: $(printf '%s' "$out" | head -1))"; return
	fi
	ok "$desc"
}

note "Discovery"
for slug in analytics crm finances gn kb; do
	expect "cards lists $slug" "$slug" -- $MM cards
done

note "Per-app card render + help (self-documenting?)"
# Universal-verb apps (app.go) — help must list the verbs.
for slug in analytics finances gn; do
	expect "$slug card render"     "$slug"  -- $MM "$slug"
	expect "$slug help lists ask"  "ask"    -- $MM "$slug" --help
	expect "$slug help lists use"  "use"    -- $MM "$slug" --help
	expect "$slug use (instances)" ""       -- $MM "$slug" use
done
# Wrapper apps document themselves their own way.
expect "crm help lists ask"      "ask"      -- $MM crm --help
expect "crm help lists surface"  "surface"  -- $MM crm --help
expect "crm use lists instances" "instance" -- $MM crm use
expect "kb help lists find"      "find"     -- $MM kb --help
expect "kb actions (surface)"    "RPC surface" -- $MM kb actions

note "Auth + instance + render (one end-to-end agent call)"
expect "finances ask returns prose" "£" -- $MM finances ask "what is my net worth, one number"

printf '\n\033[1mResult: %d passed, %d failed\033[0m\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
