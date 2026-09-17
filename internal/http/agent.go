package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"mm-cli/internal/tailscale"
	"mm-cli/internal/wire"
)

// AgentTarget is the resolved HTTP + WS base URL for an agent (local or remote).
type AgentTarget struct {
	HTTP        string
	WS          string
	DisplayName string
}

// AgentBase resolves the target agent's base URLs. node="" means local
// (`Cfg.LocalAgentURL`). Otherwise looks up the named instance from the
// hub + the local tailscaled's MagicDNS suffix.
func (c *Client) AgentBase(ctx context.Context, node string) (AgentTarget, error) {
	if node == "" {
		http := c.Cfg.LocalAgentURL
		return AgentTarget{
			HTTP:        http,
			WS:          httpToWS(http),
			DisplayName: "local",
		}, nil
	}
	row, err := c.ResolveNode(ctx, node)
	if err != nil {
		return AgentTarget{}, err
	}
	return AgentTarget{
		HTTP:        row.BaseURL,
		WS:          httpToWS(row.BaseURL),
		DisplayName: row.DisplayName,
	}, nil
}

func httpToWS(s string) string {
	if strings.HasPrefix(s, "https://") {
		return "wss://" + s[len("https://"):]
	}
	if strings.HasPrefix(s, "http://") {
		return "ws://" + s[len("http://"):]
	}
	return s
}

// AgentFetch performs a request against the agent at `node` (or localhost if empty).
// Init is nil for plain GET; pass a non-nil RequestInit-like struct for POST/etc.
type AgentReq struct {
	Method      string
	Body        []byte
	ContentType string
}

func (c *Client) AgentFetch(ctx context.Context, node string, path string, init *AgentReq) (*http.Response, error) {
	target, err := c.AgentBase(ctx, node)
	if err != nil {
		return nil, err
	}
	urlStr := target.HTTP + path
	method := http.MethodGet
	var body io.Reader
	contentType := ""
	if init != nil {
		if init.Method != "" {
			method = init.Method
		}
		if len(init.Body) > 0 {
			body = bytes.NewReader(init.Body)
		}
		contentType = init.ContentType
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s failed (%s): %w", urlStr, target.DisplayName, err)
	}
	return resp, nil
}

// ─── --node resolution via hub instance.list + tailscale suffix ────────

type ResolvedNode struct {
	BaseURL     string
	DisplayName string
}

var nodesCache struct {
	once sync.Once
	rows []wire.HubInstance
	err  error
}

// LoadNodes hits `instance.list` once per process; cached.
func (c *Client) LoadNodes(ctx context.Context) ([]wire.HubInstance, error) {
	nodesCache.once.Do(func() {
		var resp wire.HubInstanceListResp
		if err := c.Hub(ctx, "instance", "list",
			map[string]any{"slugs": []string{"desk", "agent"}}, &resp); err != nil {
			nodesCache.err = err
			return
		}
		nodesCache.rows = resp.Instances
	})
	return nodesCache.rows, nodesCache.err
}

// ResolveNode maps a --node name to a base URL via instance.list + suffix.
func (c *Client) ResolveNode(ctx context.Context, name string) (ResolvedNode, error) {
	nodes, err := c.LoadNodes(ctx)
	if err != nil {
		return ResolvedNode{}, err
	}
	lower := strings.ToLower(name)
	var matches []wire.HubInstance
	known := make([]string, 0, len(nodes))
	for _, n := range nodes {
		known = append(known, n.Name)
		if strings.EqualFold(n.Name, lower) {
			matches = append(matches, n)
		}
	}
	if len(matches) == 0 {
		k := strings.Join(known, ", ")
		if k == "" {
			k = "(none registered)"
		}
		return ResolvedNode{}, fmt.Errorf("No node named '%s'. Known: %s. Try: mm chat nodes", name, k)
	}
	if len(matches) > 1 {
		return ResolvedNode{}, fmt.Errorf("Multiple nodes named '%s'. Disambiguate via the hub.", name)
	}
	row := matches[0]
	if row.URL == nil || *row.URL == "" {
		return ResolvedNode{}, fmt.Errorf("Node '%s' has no URL registered.", row.Name)
	}
	// The local suffix is what a node on *our* tailnet is rebuilt against, but
	// a node shared in from another tailnet does not need it at all - so a
	// tailscaled that will not answer is only fatal for the former.
	suffix, suffixErr := tailscale.Suffix()
	base, err := nodeBaseURL(*row.URL, row.IsOwner, suffix, suffixErr)
	if err != nil {
		return ResolvedNode{}, err
	}
	return ResolvedNode{BaseURL: base, DisplayName: row.Name}, nil
}

// nodeBaseURL turns a node's hub-registered URL into the base URL to dial.
//
// For a node on our own tailnet the host is rebuilt from the bare name plus the
// *live* MagicDNS suffix, so a tailnet rename heals itself rather than leaving
// every registered row stale. specs/go-port/01-wire.md §"Local agent REST".
//
// That rebuild is wrong for a node **shared in from another account**, which
// lives on its owner's tailnet: reattaching ours invents a name that does not
// resolve. `Dee's iMac` is registered at `dees-imac.tail1d5901.ts.net` and
// every `mm desk --node "Dee's iMac"` died on
// `lookup dees-imac.taildd974e.ts.net: no such host` until this told them apart.
//
// Ownership is the discriminator, not the suffix. A suffix that differs from
// ours is ambiguous on its own - it is equally what a *stale* row for one of
// our own nodes looks like after a tailnet rename, and that is the case the
// rebuild exists to fix. `IsOwner` separates the two; `(shared)` in
// `mm desk nodes` is the same flag.
func nodeBaseURL(registered string, isOwner bool, localSuffix string, suffixErr error) (string, error) {
	parsed, err := url.Parse(registered)
	if err != nil {
		return "", err
	}
	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("node URL %q has no host", registered)
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	bare, _, hasSuffix := strings.Cut(host, ".")
	// Someone else's node on someone else's tailnet: theirs to name, and the
	// one path that never needs a local tailscaled at all.
	if !isOwner {
		return fmt.Sprintf("https://%s:%s", host, port), nil
	}
	if suffixErr != nil {
		// No local suffix to rebuild with. A fully-qualified registration is
		// still dialable as it stands; a bare one is not recoverable.
		if hasSuffix {
			return fmt.Sprintf("https://%s:%s", host, port), nil
		}
		return "", suffixErr
	}
	return fmt.Sprintf("https://%s.%s:%s", bare, localSuffix, port), nil
}

// ─── App /api/rpc (legacy) ─────────────────────────────────────────────

// Rpc posts {feature, action, payload} to `<app>/api/rpc`. Used by kb + crm.
// Returns parsed JSON into `out`. Throws on HTTP error.
func (c *Client) Rpc(ctx context.Context, appURL, feature, action string, payload, out any) error {
	if c.Auth == nil {
		return fmt.Errorf("Not authenticated. Run `mm login` first.")
	}
	body, _ := json.Marshal(map[string]any{
		"feature": feature,
		"action":  action,
		"payload": coalescePayload(payload),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, appURL+"/api/rpc", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Auth.Token)
	req.Header.Set("X-Hub-User-Id", c.Auth.UserID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("rpc request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s API error (%d): %s", feature, resp.StatusCode, truncate(string(respBody), 200))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

// V2 posts {feature, action, payload} to `<app>/api/v2`. Returns raw envelope.
// Per-app shapes vary so we don't unwrap.
type V2Result struct {
	OK     bool
	Status int
	Body   json.RawMessage
}

type V2Opts struct {
	Validate   *bool // nil = default true (skip manifest fetch if false)
	InstanceID string
}

func (c *Client) V2(ctx context.Context, appURL, featureAction string, payload any, opts V2Opts) (V2Result, error) {
	dot := strings.IndexByte(featureAction, '.')
	if dot < 0 {
		return V2Result{}, fmt.Errorf("feature.action must be 'feature.action' format, got: '%s'", featureAction)
	}
	feature := featureAction[:dot]
	action := featureAction[dot+1:]

	headers := map[string]string{"Content-Type": "application/json"}
	if c.Auth != nil {
		headers["Authorization"] = "Bearer " + c.Auth.Token
		headers["X-Hub-User-Id"] = c.Auth.UserID
	}
	if opts.InstanceID != "" {
		headers["X-Hub-Instance-Id"] = opts.InstanceID
	}
	body, _ := json.Marshal(map[string]any{
		"feature": feature, "action": action, "payload": coalescePayload(payload),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, appURL+"/api/v2", bytes.NewReader(body))
	if err != nil {
		return V2Result{}, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return V2Result{}, fmt.Errorf("v2 request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return V2Result{OK: resp.StatusCode/100 == 2, Status: resp.StatusCode, Body: raw}, nil
}
