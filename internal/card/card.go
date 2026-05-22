// Package card fetches + caches `<app>/.well-known/agent.json`.
// Mirrors src/agent-card.ts: ~/.mm-cli/cards/<slug>.json, 24h TTL.
package card

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"mm-cli/internal/apps"
)

const cacheTTL = 24 * time.Hour

type Tool struct {
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

type Alias struct {
	Feature     string `json:"feature"`
	Action      string `json:"action"`
	Description string `json:"description,omitempty"`
}

type Card struct {
	Name         string           `json:"name"`
	Description  string           `json:"description,omitempty"`
	Version      string           `json:"version,omitempty"`
	Capabilities []string         `json:"capabilities,omitempty"`
	ChatURL      string           `json:"chatUrl,omitempty"`
	MCPURL       *string          `json:"mcpUrl,omitempty"`
	Tools        []Tool           `json:"tools,omitempty"`
	Aliases      map[string]Alias `json:"aliases,omitempty"`
	Auth         []string         `json:"auth,omitempty"`
}

func cacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, ".mm-cli", "cards")
	return d, os.MkdirAll(d, 0o755)
}

func cachePath(slug string) (string, error) {
	d, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, slug+".json"), nil
}

func Load(ctx context.Context, slug string, refresh bool) (*Card, error) {
	path, err := cachePath(slug)
	if err != nil {
		return nil, err
	}
	if !refresh {
		if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) < cacheTTL {
			data, err := os.ReadFile(path)
			if err == nil {
				var c Card
				if json.Unmarshal(data, &c) == nil {
					return &c, nil
				}
			}
		}
	}
	c, err := Fetch(ctx, slug)
	if err != nil {
		return nil, err
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	_ = os.WriteFile(path, data, 0o644)
	return c, nil
}

func Fetch(ctx context.Context, slug string) (*Card, error) {
	app, err := apps.Resolve(slug)
	if err != nil {
		return nil, err
	}
	url := app.URL + "/.well-known/agent.json"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("card fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("card fetch %s failed: HTTP %d: %s", url, resp.StatusCode, string(body))
	}
	var c Card
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return nil, err
	}
	if c.Name == "" {
		return nil, fmt.Errorf("card from %s is malformed: missing 'name'", url)
	}
	return &c, nil
}

// HasCapability reports whether the card claims a given capability.
func (c *Card) HasCapability(cap string) bool {
	for _, x := range c.Capabilities {
		if x == cap {
			return true
		}
	}
	return false
}

// FindTool returns the named tool, if any.
func (c *Card) FindTool(name string) *Tool {
	for i := range c.Tools {
		if c.Tools[i].Name == name {
			return &c.Tools[i]
		}
	}
	return nil
}
