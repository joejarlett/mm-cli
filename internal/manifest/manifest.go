// Package manifest fetches + caches `<app>/api/v2/manifest`.
// Mirrors src/manifest.ts: ~/.mm-cli/manifests/<slug>.json, 24h TTL.
package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mm-cli/internal/apps"
)

const cacheTTL = 24 * time.Hour

type Action struct {
	Auth        string `json:"auth"`
	Description string `json:"description,omitempty"`
	// Input/Output are arbitrary JSON Schema-ish payloads; we just preserve them.
	Input  json.RawMessage `json:"input"`
	Output json.RawMessage `json:"output"`
}

type Manifest struct {
	AppSlug  string                       `json:"appSlug"`
	Version  string                       `json:"version"`
	Features map[string]map[string]Action `json:"features"`
}

func cacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, ".mm-cli", "manifests")
	return d, os.MkdirAll(d, 0o755)
}

func cachePath(slug string) (string, error) {
	d, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, slug+".json"), nil
}

// Load returns the manifest for `slug`, from cache if fresh (24h) or by
// fetching `<app>/api/v2/manifest`. Pass `refresh=true` to bypass cache.
func Load(ctx context.Context, slug string, refresh bool) (*Manifest, error) {
	path, err := cachePath(slug)
	if err != nil {
		return nil, err
	}
	if !refresh {
		if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) < cacheTTL {
			data, err := os.ReadFile(path)
			if err == nil {
				var m Manifest
				if json.Unmarshal(data, &m) == nil {
					return &m, nil
				}
			}
		}
	}
	m, err := Fetch(ctx, slug)
	if err != nil {
		return nil, err
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(path, data, 0o644)
	return m, nil
}

func Fetch(ctx context.Context, slug string) (*Manifest, error) {
	app, err := apps.Resolve(slug)
	if err != nil {
		return nil, err
	}
	url := app.URL + "/api/v2/manifest"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("manifest fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("manifest fetch %s failed: HTTP %d: %s", url, resp.StatusCode, string(body))
	}
	var m Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest %s: invalid JSON: %w", url, err)
	}
	if m.AppSlug == "" || m.Features == nil {
		return nil, fmt.Errorf("manifest from %s is malformed: missing appSlug/features", url)
	}
	return &m, nil
}

// ActionCount returns the total feature.action count.
func (m *Manifest) ActionCount() int {
	n := 0
	for _, actions := range m.Features {
		n += len(actions)
	}
	return n
}

// ResolveAction splits "feature.action" and looks it up, returning a clean
// error if either part is missing.
func (m *Manifest) ResolveAction(featureAction string) (feature, action string, def Action, err error) {
	dot := strings.IndexByte(featureAction, '.')
	if dot < 0 {
		return "", "", Action{}, fmt.Errorf("feature.action must be 'feature.action' format, got: '%s'", featureAction)
	}
	feature = featureAction[:dot]
	action = featureAction[dot+1:]
	fm, ok := m.Features[feature]
	if !ok {
		known := truncStringList(keys(m.Features), 10)
		return "", "", Action{}, fmt.Errorf("Unknown feature '%s' in %s. Known: %s", feature, m.AppSlug, known)
	}
	d, ok := fm[action]
	if !ok {
		known := truncStringList(keys(fm), 10)
		return "", "", Action{}, fmt.Errorf("Unknown action '%s' on %s.%s. Known: %s", action, m.AppSlug, feature, known)
	}
	return feature, action, d, nil
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func truncStringList(xs []string, n int) string {
	if len(xs) <= n {
		return strings.Join(xs, ", ")
	}
	return strings.Join(xs[:n], ", ") + "…"
}
