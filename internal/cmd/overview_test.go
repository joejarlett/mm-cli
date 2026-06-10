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

func TestRenderOverview_SingleAppNoHeader(t *testing.T) {
	resp := wire.OverviewResp{Apps: map[string]wire.OverviewApp{
		"kb": {Sections: []wire.OverviewSection{
			{Label: "Notebooks", Items: []wire.OverviewItem{
				{ID: "abc", Title: "Joe-Inc", Count: intp(12)},
				{ID: "def", Title: "Neurodiversity", Subtitle: "research", Count: intp(3)},
			}},
		}},
	}}
	got := renderOverview(resp)

	// Single app → no `# kb` header.
	if strings.Contains(got, "# kb") {
		t.Errorf("single-app overview should omit the per-app header, got:\n%s", got)
	}
	for _, want := range []string{"## Notebooks (2)", "Joe-Inc (12)", "`abc`", "Neurodiversity — research (3)"} {
		if !strings.Contains(got, want) {
			t.Errorf("overview missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderOverview_MultiAppHeaders(t *testing.T) {
	resp := wire.OverviewResp{Apps: map[string]wire.OverviewApp{
		"kb":  {Sections: []wire.OverviewSection{{Label: "Notebooks", Items: []wire.OverviewItem{{ID: "a", Title: "X"}}}}},
		"crm": {Sections: []wire.OverviewSection{{Label: "Instances", Items: []wire.OverviewItem{{ID: "b", Title: "GN"}}}}},
	}}
	got := renderOverview(resp)
	if !strings.Contains(got, "# crm") || !strings.Contains(got, "# kb") {
		t.Errorf("multi-app overview should print per-app headers, got:\n%s", got)
	}
	// Deterministic ordering: crm sorts before kb.
	if strings.Index(got, "# crm") > strings.Index(got, "# kb") {
		t.Errorf("apps should render in sorted order (crm before kb), got:\n%s", got)
	}
}

func TestRenderOverview_Empty(t *testing.T) {
	if got := renderOverview(wire.OverviewResp{}); !strings.Contains(got, "No apps expose an overview") {
		t.Errorf("empty overview should explain itself, got: %q", got)
	}
}

func TestOverviewLine_OmitsAbsentCountAndSubtitle(t *testing.T) {
	// count nil and no subtitle → neither rendered, but a 0 count still shows.
	if got := overviewLine(wire.OverviewItem{ID: "x", Title: "Bare"}); got != "Bare `x`" {
		t.Errorf("bare item: got %q", got)
	}
	if got := overviewLine(wire.OverviewItem{ID: "x", Title: "Z", Count: intp(0)}); !strings.Contains(got, "(0)") {
		t.Errorf("zero count must render (pointer distinguishes 0 from absent): got %q", got)
	}
}

func TestRenderSurface_KindAndDate(t *testing.T) {
	resp := wire.SurfaceResp{Apps: map[string]wire.SurfaceApp{
		"crm": {Items: []wire.SurfaceItem{
			{ID: "1", Title: "Follow up with Acme", Kind: "followup", At: "2026-06-09T10:00:00Z"},
			{ID: "2", Title: "Deal warming", Subtitle: "Beta Ltd", Kind: "deal"},
		}},
	}}
	got := renderSurface(resp)
	for _, want := range []string{"[followup] Follow up with Acme", "2026-06-09", "[deal] Deal warming — Beta Ltd", "`2`"} {
		if !strings.Contains(got, want) {
			t.Errorf("surface missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderSurface_EmptyItemsPerApp(t *testing.T) {
	resp := wire.SurfaceResp{Apps: map[string]wire.SurfaceApp{"kb": {Items: nil}}}
	if got := renderSurface(resp); !strings.Contains(got, "Nothing surfacing") {
		t.Errorf("app with no items should say so, got: %q", got)
	}
}

func TestRenderSurface_Empty(t *testing.T) {
	if got := renderSurface(wire.SurfaceResp{}); !strings.Contains(got, "Nothing surfacing right now") {
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
