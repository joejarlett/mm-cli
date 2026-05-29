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
	parsed, err := url.Parse(*row.URL)
	if err != nil {
		return ResolvedNode{}, err
	}
	bare := strings.SplitN(parsed.Hostname(), ".", 2)[0]
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	suffix, err := tailscale.Suffix()
	if err != nil {
		return ResolvedNode{}, err
	}
	return ResolvedNode{
		BaseURL:     fmt.Sprintf("https://%s.%s:%s", bare, suffix, port),
		DisplayName: row.Name,
	}, nil
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
