// Package apps holds the slug→base-URL registry for the cross-app
// dispatcher. Mirrors src/apps.ts.
package apps

import (
	"fmt"
	"os"
	"strings"
)

type Entry struct {
	Slug        string
	URL         string
	Description string
}

// Description is a one-line domain gloss — "what lives in this app", not the
// slug capitalised. It's the single source of truth for the app's `Short` help
// line and card header, so an agent skimming `mm --help` can tell which app
// owns a question. Style: "<Name> — <nouns the app is about>", em-dash,
// lowercase after it, no trailing period.
var Registry = map[string]Entry{
	"kb":        {Slug: "kb", URL: "https://kb.meta-me.uk", Description: "Knowledge Base — research corpora, notebooks, documents, deep research"},
	"crm":       {Slug: "crm", URL: "https://crm.meta-me.uk", Description: "CRM — contacts, interactions, follow-ups (multi-instance)"},
	"finances":  {Slug: "finances", URL: "https://finances.meta-me.uk", Description: "Finances — accounts, transactions, net worth (multi-instance)"},
	"gn":        {Slug: "gn", URL: "https://grounded.ninja", Description: "GroundedNinja — wellbeing journal, practices, reflection"},
	"keel":      {Slug: "keel", URL: "https://keel.meta-me.uk", Description: "Keel — personal health: weight & trends, pantry, exercise, blood-test docs (multi-instance)"},
	"analytics": {Slug: "analytics", URL: "https://analytics.meta-me.uk", Description: "Analytics — pageviews and traffic across your apps"},
	"konte":     {Slug: "konte", URL: "https://konte.meta-me.uk", Description: "Konte — agent-driven YouTube production: ideas, research, scripts, storyboards, exports (multi-instance)"},
}

// Resolve looks up an app slug; returns an error listing known slugs on miss.
// An MM_<SLUG>_BASE_URL env var overrides the registered URL — used to point a
// command at a local dev server (e.g. MM_KONTE_BASE_URL=http://localhost:5173)
// for fast iteration before deploying. Mirrors the MM_CONVERT_URL pattern.
func Resolve(slug string) (Entry, error) {
	if e, ok := Registry[slug]; ok {
		if u := os.Getenv("MM_" + strings.ToUpper(slug) + "_BASE_URL"); u != "" {
			e.URL = strings.TrimRight(u, "/")
		}
		return e, nil
	}
	known := ""
	for k := range Registry {
		if known != "" {
			known += ", "
		}
		known += k
	}
	return Entry{}, fmt.Errorf("Unknown app: '%s'. Known apps: %s", slug, known)
}
