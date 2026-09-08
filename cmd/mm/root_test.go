package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// wantRootCommands is the full top-level surface of `mm`: every verb, with its
// aliases beside it.
//
// It is a hand-maintained golden list, and that is the point. Adding, removing
// or renaming a top-level verb changes what every user and agent script can
// reach, so it should show up as a deliberate one-line diff in review rather
// than as a silent consequence of editing newRootCmd. When this test fails,
// read the diff it prints: if the change was intended, update this map.
//
// This asserts against the real newRootCmd(). Its predecessor (TestCliDrift,
// removed 2026-09-08) rebuilt a second command tree inside the test and
// compared that to the deleted TypeScript CLI — so it drifted from the actual
// root it meant to guard, missing overview, surface, host, convert and every
// app command, and then failed outright once src/index.ts was deleted.
var wantRootCommands = map[string][]string{
	// Apps — bespoke wrappers, then the registry-driven universal ones.
	"kb":        nil,
	"crm":       nil,
	"analytics": nil,
	"finances":  nil,
	"gn":        nil,
	"keel":      nil,
	"konte":     nil,

	// Google Workspace.
	"email":    nil,
	"calendar": nil,
	"drive":    nil,
	"tasks":    nil,

	// Agents & automation.
	"run":     nil,
	"desk":    nil,
	"hub":     nil,
	"capture": nil,
	"project": {"projects"},

	// Discovery.
	"cards":    {"card"},
	"manifest": nil,
	"overview": nil,
	"surface":  nil,

	// Account.
	"login":  nil,
	"logout": nil,
	"whoami": nil,
	"status": nil,

	// CLI & admin.
	"admin":    nil,
	"host":     nil,
	"feedback": nil,
	"stt":      nil,
	"tts":      nil,
	"convert":  nil,
	"update":   nil,
	"version":  nil,
}

// cobraBuiltins are added by Cobra itself during Execute, not by newRootCmd, so
// they are outside what this file governs.
var cobraBuiltins = map[string]bool{"help": true, "completion": true}

func rootCommands(t *testing.T) []*cobra.Command {
	t.Helper()
	var out []*cobra.Command
	for _, c := range newRootCmd().Commands() {
		if cobraBuiltins[c.Name()] {
			continue
		}
		out = append(out, c)
	}
	return out
}

func TestRootCommandSurface(t *testing.T) {
	got := map[string][]string{}
	for _, c := range rootCommands(t) {
		got[c.Name()] = c.Aliases
	}

	for name, wantAliases := range wantRootCommands {
		gotAliases, ok := got[name]
		if !ok {
			t.Errorf("command %q is in the golden list but missing from newRootCmd — was its AddCommand dropped?", name)
			continue
		}
		if a, b := sorted(wantAliases), sorted(gotAliases); !equal(a, b) {
			t.Errorf("command %q aliases: want %v, got %v", name, a, b)
		}
	}

	for name := range got {
		if _, ok := wantRootCommands[name]; !ok {
			t.Errorf("command %q is registered on the root but not in the golden list — add it to wantRootCommands", name)
		}
	}
}

// TestEveryRootCommandIsGrouped guards the help output: newRootCmd registers via
// an add() helper that sets GroupID, so a bare root.AddCommand compiles and runs
// but drops the command into Cobra's anonymous "Additional Commands" heading,
// where nobody scanning `mm --help` by intent will find it.
func TestEveryRootCommandIsGrouped(t *testing.T) {
	groups := map[string]bool{}
	for _, g := range newRootCmd().Groups() {
		groups[g.ID] = true
	}

	for _, c := range rootCommands(t) {
		switch {
		case c.GroupID == "":
			t.Errorf("command %q has no GroupID — register it with add(), not root.AddCommand", c.Name())
		case !groups[c.GroupID]:
			t.Errorf("command %q has GroupID %q, which is not registered via root.AddGroup", c.Name(), c.GroupID)
		}
	}
}

// TestNoShadowedRootNames catches a name or alias claimed twice. Cobra resolves
// such a collision silently by registration order, so one of the two commands
// simply becomes unreachable from the command line.
func TestNoShadowedRootNames(t *testing.T) {
	owner := map[string]string{}
	for _, c := range rootCommands(t) {
		for _, token := range append([]string{c.Name()}, c.Aliases...) {
			if prev, clash := owner[token]; clash {
				t.Errorf("%q is claimed by both %q and %q — one of them is unreachable", token, prev, c.Name())
				continue
			}
			owner[token] = c.Name()
		}
	}
}

// TestRootHelpListsEveryCommand ties the golden list to what a user actually
// sees, so a command cannot be registered yet hidden from `mm --help`.
func TestRootHelpListsEveryCommand(t *testing.T) {
	root := newRootCmd()
	var buf strings.Builder
	root.SetOut(&buf)
	if err := root.Help(); err != nil {
		t.Fatalf("root.Help(): %v", err)
	}
	help := buf.String()

	for name := range wantRootCommands {
		if !strings.Contains(help, "\n  "+name+" ") {
			t.Errorf("command %q does not appear in `mm --help` — is it marked Hidden?", name)
		}
	}
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
