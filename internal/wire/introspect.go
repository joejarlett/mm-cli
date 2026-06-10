package wire

// Three-axis app introspection — the `overview` / `surface` halves of the
// cards · overview · surface contract (specs/surface-overview-contract.md in
// the hub repo). The hub owns these shapes; mm-cli only renders them.
//
//	cards / manifest → what an app can DO   (already shipped)
//	overview         → what IS here          (stable catalogue)
//	surface          → what's HAPPENING now  (decaying activity)

// ─── overview (agent.overview, normalised) ──────────────────────────────

// OverviewItem is one entry in an overview section — a stable catalogue row.
//
// Per the contract, the three quantitative/descriptive slots are distinct and
// shouldn't all describe the same number: value = preformatted headline amount
// with its unit baked in by the app ("£47,000", "175 kg"); count = a quantity;
// subtitle = a qualitative gloss.
type OverviewItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Value    string `json:"value,omitempty"` // preformatted headline amount + unit
	Count    *int   `json:"count,omitempty"` // pointer: distinguish 0 from absent
	Href     string `json:"href,omitempty"`
}

// OverviewSection groups overview items under a label (e.g. "Notebooks").
type OverviewSection struct {
	Label string         `json:"label"`
	Items []OverviewItem `json:"items"`
}

// OverviewApp is one app's overview payload (the agent.overview response).
type OverviewApp struct {
	Sections []OverviewSection `json:"sections"`
	Meta     map[string]any    `json:"meta,omitempty"`
}

// OverviewResp is the hub aggregator (overview.get) shape: app slug → payload.
// A single-app request returns the same shape with one key.
type OverviewResp struct {
	Apps map[string]OverviewApp `json:"apps"`
}

// ─── surface (agent.surface, normalised) ────────────────────────────────

// SurfaceItem is one activity row — most-relevant first.
type SurfaceItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Kind     string `json:"kind"`
	At       string `json:"at,omitempty"` // ISO timestamp
	Href     string `json:"href,omitempty"`
}

// SurfaceApp is one app's surface payload (the agent.surface response).
type SurfaceApp struct {
	Items []SurfaceItem  `json:"items"`
	Meta  map[string]any `json:"meta,omitempty"`
}

// SurfaceResp is the hub aggregator (surface.get) shape: app slug → payload.
type SurfaceResp struct {
	Apps map[string]SurfaceApp `json:"apps"`
}
