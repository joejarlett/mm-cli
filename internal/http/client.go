// Package http is the unified HTTP client — one place for every wire
// surface mm-cli speaks. Mirrors src/http/client.ts.
//
// Three transport methods + the local-agent helpers:
//
//   Hub(ctx, feature, action, payload, out) — POST {HubURL}/api/mm.
//     Unwraps `data` into `out`, throws on `errors`.
//
//   V2(ctx, app, "feature.action", payload) — POST {app.url}/api/v2.
//     Returns the raw envelope; per-app shapes vary so callers parse.
//
//   Rpc(ctx, app, feature, action, payload, out) — POST {app.url}/api/rpc.
//     Legacy kb+crm path. Returns parsed JSON into `out`.
//
//   AgentFetch / AgentBase — local-agent REST + WS base resolution.
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"mm-cli/internal/auth"
	"mm-cli/internal/config"
	"mm-cli/internal/wire"
)

// Client is the shared HTTP client. One instance per process; pass the
// pointer into command handlers.
type Client struct {
	HTTPClient *http.Client
	Cfg        *config.Config
	Auth       *auth.State // nil if not logged in
}

// New builds a Client with sensible timeouts + cached auth.
func New() *Client {
	state, _ := auth.Load()
	return &Client{
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		Cfg:        config.Load(),
		Auth:       state,
	}
}

// ─── Hub mm-RPC ────────────────────────────────────────────────────────

// Hub posts to {HubURL}/api/mm with the given {feature, action, payload}.
// On success, unmarshals `envelope.data` into `out`. On failure, returns
// the structured envelope error.
//
// `out` may be nil if the caller doesn't care about the response shape.
func (c *Client) Hub(ctx context.Context, feature, action string, payload any, out any) error {
	if c.Auth == nil {
		return fmt.Errorf("Not authenticated. Run `mm login` first.")
	}

	body, err := json.Marshal(map[string]any{
		"feature": feature,
		"action":  action,
		"payload": coalescePayload(payload),
	})
	if err != nil {
		return fmt.Errorf("marshal hub payload: %w", err)
	}

	url := c.Cfg.HubURL + "/api/mm"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Auth.Token)
	req.Header.Set("X-Hub-User-Id", c.Auth.UserID)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("hub request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("hub read body: %w", err)
	}

	// Envelope: { data: T } | { errors: [...] }
	var probe struct {
		Errors []wire.HubErrItem `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &probe); err != nil {
		return fmt.Errorf("hub %s.%s: non-JSON response (HTTP %d): %s",
			feature, action, resp.StatusCode, truncate(string(respBody), 200))
	}
	if resp.StatusCode/100 != 2 || len(probe.Errors) > 0 {
		var msg string
		if len(probe.Errors) > 0 {
			e := probe.Errors[0]
			msg = e.Detail
			if msg == "" {
				msg = e.Title
			}
		}
		if msg == "" {
			msg = fmt.Sprintf("%s.%s failed (HTTP %d)", feature, action, resp.StatusCode)
		}
		return fmt.Errorf("%s", msg)
	}

	if out == nil {
		return nil
	}

	// Unwrap data into out.
	var wrapper struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respBody, &wrapper); err != nil {
		return fmt.Errorf("hub unwrap data: %w", err)
	}
	if len(wrapper.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(wrapper.Data, out); err != nil {
		return fmt.Errorf("hub unmarshal %s.%s response: %w", feature, action, err)
	}
	return nil
}

// HubFetch makes a direct authenticated request to {HubURL}{path}.
// Does NOT go through /api/mm — use Client.Hub for mm-RPC calls.
func (c *Client) HubFetch(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	if c.Auth == nil {
		return nil, fmt.Errorf("not authenticated. Run `mm login` first.")
	}
	urlStr := c.Cfg.HubURL + path
	var r io.Reader
	if len(body) > 0 {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Auth.Token)
	req.Header.Set("X-Hub-User-Id", c.Auth.UserID)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.HTTPClient.Do(req)
}

// HubStream makes a streaming SSE POST to {HubURL}{path}.
// Uses a client without a timeout — caller must close resp.Body.
func (c *Client) HubStream(ctx context.Context, path string, body []byte) (*http.Response, error) {
	if c.Auth == nil {
		return nil, fmt.Errorf("not authenticated. Run `mm login` first.")
	}
	urlStr := c.Cfg.HubURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Auth.Token)
	req.Header.Set("X-Hub-User-Id", c.Auth.UserID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	return (&http.Client{}).Do(req)
}

// coalescePayload ensures the request body sends `{}` rather than `null`
// when the caller passes nil. Matches TS behaviour.
func coalescePayload(p any) any {
	if p == nil {
		return map[string]any{}
	}
	return p
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
