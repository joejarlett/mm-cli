package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"mm-cli/internal/wire"
)

// Fixtures mirror the normalised shapes in the hub's
// specs/surface-overview-contract.md. The CLI renders these; the hub owns
// them — so these tests pin the render, not the contract.

func intp(n int) *int { return &n }

// Scoped form (`mm overview kb`) drops the redundant header — the user named
// the app.
func TestRenderOverview_ScopedDropsHeader(t *testing.T) {
	resp := wire.OverviewResp{Apps: map[string]wire.OverviewApp{
		"kb": {Sections: []wire.OverviewSection{
			{Label: "Notebooks", Items: []wire.OverviewItem{
				{ID: "abc", Title: "Joe-Inc", Count: intp(12)},
				{ID: "def", Title: "Neurodiversity", Subtitle: "research", Count: intp(3)},
			}},
		}},
	}}
	got := renderOverview(resp, true) // scoped

	if strings.Contains(got, "# kb") {
		t.Errorf("scoped overview should omit the per-app header, got:\n%s", got)
	}
	if strings.Contains(got, "mm cards") {
		t.Errorf("scoped overview should omit the provenance footer, got:\n%s", got)
	}
	if strings.Contains(got, "`abc`") {
		t.Errorf("raw ids should not appear in the human view (they live in --json), got:\n%s", got)
	}
	for _, want := range []string{"## Notebooks (2)", "Joe-Inc (12)", "Neurodiversity — research (3)"} {
		if !strings.Contains(got, want) {
			t.Errorf("overview missing %q in:\n%s", want, got)
		}
	}
}

// Aggregate form (`mm overview`) ALWAYS shows the header + provenance, even
// for one app — so a lone section can't masquerade as the whole environment.
func TestRenderOverview_AggregateSingleAppKeepsHeaderAndProvenance(t *testing.T) {
	resp := wire.OverviewResp{Apps: map[string]wire.OverviewApp{
		"kb": {Sections: []wire.OverviewSection{{Label: "Notebooks", Items: []wire.OverviewItem{{ID: "a", Title: "X"}}}}},
	}}
	got := renderOverview(resp, false) // aggregate
	if !strings.Contains(got, "# kb") {
		t.Errorf("aggregate overview must show the per-app header even for one app, got:\n%s", got)
	}
	if !strings.Contains(got, "1 app ·") || !strings.Contains(got, "mm cards") {
		t.Errorf("aggregate overview must show a provenance footer, got:\n%s", got)
	}
}

func TestRenderOverview_MultiAppHeaders(t *testing.T) {
	resp := wire.OverviewResp{Apps: map[string]wire.OverviewApp{
		"kb":  {Sections: []wire.OverviewSection{{Label: "Notebooks", Items: []wire.OverviewItem{{ID: "a", Title: "X"}}}}},
		"crm": {Sections: []wire.OverviewSection{{Label: "Instances", Items: []wire.OverviewItem{{ID: "b", Title: "GN"}}}}},
	}}
	got := renderOverview(resp, false)
	if !strings.Contains(got, "# crm") || !strings.Contains(got, "# kb") {
		t.Errorf("multi-app overview should print per-app headers, got:\n%s", got)
	}
	if !strings.Contains(got, "2 apps ·") {
		t.Errorf("multi-app overview should report 2 apps, got:\n%s", got)
	}
	// Deterministic ordering: crm sorts before kb.
	if strings.Index(got, "# crm") > strings.Index(got, "# kb") {
		t.Errorf("apps should render in sorted order (crm before kb), got:\n%s", got)
	}
}

func TestRenderOverview_Empty(t *testing.T) {
	if got := renderOverview(wire.OverviewResp{}, false); !strings.Contains(got, "No apps expose an overview") {
		t.Errorf("empty overview should explain itself, got: %q", got)
	}
}

func TestOverviewLine_NoRawIDAndCountSemantics(t *testing.T) {
	// Bare item: plain name only — no markdown bold, no raw id (it's in
	// --json), no count.
	if got := overviewLine(wire.OverviewItem{ID: "x", Title: "Bare"}); got != "Bare" {
		t.Errorf("bare item: got %q, want %q", got, "Bare")
	}
	// A 0 count still renders (pointer distinguishes 0 from absent).
	if got := overviewLine(wire.OverviewItem{ID: "x", Title: "Z", Count: intp(0)}); !strings.Contains(got, "(0)") {
		t.Errorf("zero count must render: got %q", got)
	}
}

func TestRenderSurface_KindAndDate(t *testing.T) {
	resp := wire.SurfaceResp{Apps: map[string]wire.SurfaceApp{
		"crm": {Items: []wire.SurfaceItem{
			{ID: "1", Title: "Follow up with Acme", Kind: "followup", At: "2026-06-09T10:00:00Z"},
			{ID: "2", Title: "Deal warming", Subtitle: "Beta Ltd", Kind: "deal"},
		}},
	}}
	got := renderSurface(resp, true)
	for _, want := range []string{"[followup] Follow up with Acme", "2026-06-09", "[deal] Deal warming — Beta Ltd"} {
		if !strings.Contains(got, want) {
			t.Errorf("surface missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "`2`") {
		t.Errorf("raw ids should not appear in the surface human view (they live in --json), got:\n%s", got)
	}
}

func TestRenderSurface_EmptyItemsPerApp(t *testing.T) {
	resp := wire.SurfaceResp{Apps: map[string]wire.SurfaceApp{"kb": {Items: nil}}}
	if got := renderSurface(resp, true); !strings.Contains(got, "Nothing surfacing") {
		t.Errorf("app with no items should say so, got: %q", got)
	}
}

// Aggregate surface keeps the header + provenance even for a single app.
func TestRenderSurface_AggregateSingleAppProvenance(t *testing.T) {
	resp := wire.SurfaceResp{Apps: map[string]wire.SurfaceApp{
		"kb": {Items: []wire.SurfaceItem{{ID: "1", Title: "Recent doc", Kind: "research"}}},
	}}
	got := renderSurface(resp, false)
	// Header carries the app tagline now: "# kb — <gloss> (1)".
	if !strings.Contains(got, "# kb — ") || !strings.Contains(got, "(1)") {
		t.Errorf("aggregate surface must show the tagline'd per-app header, got:\n%s", got)
	}
	if !strings.Contains(got, "1 app ·") {
		t.Errorf("aggregate surface must show a provenance footer, got:\n%s", got)
	}
}

func TestRenderSurface_Empty(t *testing.T) {
	if got := renderSurface(wire.SurfaceResp{}, false); !strings.Contains(got, "Nothing surfacing right now") {
		t.Errorf("empty surface should explain itself, got: %q", got)
	}
}

// Guard the wire shapes decode from the exact JSON the contract documents —
// catches drift between the doc and the structs the renderer relies on.
func TestOverviewResp_DecodesContractJSON(t *testing.T) {
	const body = `{"apps":{"kb":{"sections":[{"label":"Notebooks","items":[{"id":"abc","title":"Joe-Inc","count":12,"href":"/kb/abc"}]}],"meta":{"app":"kb","took_ms":4}}}}`
	var resp wire.OverviewResp
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	kb, ok := resp.Apps["kb"]
	if !ok || len(kb.Sections) != 1 || len(kb.Sections[0].Items) != 1 {
		t.Fatalf("unexpected decode: %+v", resp)
	}
	if it := kb.Sections[0].Items[0]; it.Count == nil || *it.Count != 12 || it.Href != "/kb/abc" {
		t.Errorf("item fields not decoded: %+v", it)
	}
}

func TestSurfaceResp_DecodesContractJSON(t *testing.T) {
	const body = `{"apps":{"crm":{"items":[{"id":"1","title":"Follow up","kind":"followup","at":"2026-06-09T10:00:00Z","subtitle":"Acme"}],"meta":{"app":"crm","total":1,"took_ms":7}}}}`
	var resp wire.SurfaceResp
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	it := resp.Apps["crm"].Items[0]
	if it.Kind != "followup" || it.Subtitle != "Acme" || it.At == "" {
		t.Errorf("surface item not decoded: %+v", it)
	}
}
