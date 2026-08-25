package cmd

import (
	"strings"
	"testing"
)

func TestBuildAppView(t *testing.T) {
	entitled := []hubApp{
		{Slug: "kb", Name: "Knowledge Base"},
		{Slug: "desk", Name: "Desk"},
		{Slug: "transcribe", Name: "Transcribe"},
		{Slug: "crm", Name: "CRM"},
	}
	instances := []instanceItem{
		{ID: "i1", AppSlug: "crm", Name: "Second CRM"},
		{ID: "i2", AppSlug: "crm", Name: "Main CRM", IsPrimary: true},
		{ID: "n1", AppSlug: "desk", Name: "fedora", URL: "https://fedora.ts.net:31415"},
		{ID: "n2", AppSlug: "agent", Name: "M4", URL: "https://m4.ts.net:31415"},
	}

	got, nodes := buildAppView(entitled, instances)

	// Node instances are machines, not apps — they must not appear in either
	// the app list or as an entitled app row.
	for _, a := range got {
		if a.Slug == "desk" || a.Slug == "agent" {
			t.Fatalf("node slug %q leaked into the app list", a.Slug)
		}
	}
	if len(nodes) != 2 || nodes[0].Name != "fedora" || nodes[1].Name != "M4" {
		t.Fatalf("nodes = %+v, want fedora then M4 (case-insensitive sort)", nodes)
	}

	byslug := map[string]statusApp{}
	for _, a := range got {
		byslug[a.Slug] = a
	}

	// Registry apps carry typed verbs + the registry gloss; hub-only apps don't.
	if !byslug["kb"].Typed || byslug["kb"].Description == "" {
		t.Fatalf("kb should be typed with a description: %+v", byslug["kb"])
	}
	if byslug["transcribe"].Typed {
		t.Fatalf("transcribe has no typed verbs in this CLI")
	}
	if !byslug["transcribe"].Entitled {
		t.Fatalf("transcribe came from the hub, so it's entitled")
	}

	// Registry apps the hub didn't return still show, flagged as ungranted.
	fin, ok := byslug["finances"]
	if !ok || fin.Entitled || !fin.Typed {
		t.Fatalf("finances should appear as typed-but-ungranted: %+v", fin)
	}

	// The default instance sorts first so the "which one do I hit" answer
	// is the first name rendered.
	crm := byslug["crm"]
	if len(crm.Instances) != 2 || crm.Instances[0].Name != "Main CRM" || !crm.Instances[0].Default {
		t.Fatalf("crm instances = %+v, want the primary first", crm.Instances)
	}

	// Typed apps lead the list.
	firstGeneric := len(got)
	for i, a := range got {
		if !a.Typed {
			firstGeneric = i
			break
		}
	}
	for _, a := range got[firstGeneric:] {
		if a.Typed {
			t.Fatalf("typed app %q sorted after generic ones", a.Slug)
		}
	}
}

func TestRedactDSN(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"full url", "postgres://gn_app:hunter2@10.0.0.5:5432/mm?sslmode=disable", "gn_app@10.0.0.5:5432/mm"},
		{"no user", "postgres://localhost:5432/kb", "localhost:5432/kb"},
		{"no db", "postgres://gn_app:pw@db.internal:5432", "gn_app@db.internal:5432"},
		{"unparseable", "not a url", "(configured)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactDSN(tc.in)
			if got != tc.want {
				t.Fatalf("redactDSN(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, "hunter2") || strings.Contains(got, "pw@db") {
				t.Fatalf("password leaked into %q", got)
			}
		})
	}
}
