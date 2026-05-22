// Package apps holds the slug→base-URL registry for the cross-app
// dispatcher. Mirrors src/apps.ts.
package apps

import "fmt"

type Entry struct {
	Slug        string
	URL         string
	Description string
}

var Registry = map[string]Entry{
	"kb":        {Slug: "kb", URL: "https://kb.meta-me.uk", Description: "Knowledge Base"},
	"crm":       {Slug: "crm", URL: "https://crm.meta-me.uk", Description: "CRM"},
	"finances":  {Slug: "finances", URL: "https://finances.meta-me.uk", Description: "Finances"},
	"gn":        {Slug: "gn", URL: "https://grounded.ninja", Description: "GroundedNinja"},
	"analytics": {Slug: "analytics", URL: "https://analytics.meta-me.uk", Description: "Analytics"},
}

// Resolve looks up an app slug; returns an error listing known slugs on miss.
func Resolve(slug string) (Entry, error) {
	if e, ok := Registry[slug]; ok {
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
