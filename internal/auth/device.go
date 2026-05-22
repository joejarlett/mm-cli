package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DeviceInitResp matches the auth.meta-me.uk /api/cli/device response.
type DeviceInitResp struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// VerificationURL prefers verification_uri_complete (has the code prefilled),
// falls back to verification_uri.
func (d *DeviceInitResp) VerificationURL() string {
	if d.VerificationURIComplete != "" {
		return d.VerificationURIComplete
	}
	return d.VerificationURI
}

// TokenResp matches the /api/cli/token success response.
type TokenResp struct {
	AccessToken string `json:"access_token"`
	Key         struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Prefix string   `json:"prefix"`
		Scopes []string `json:"scopes"`
	} `json:"key"`
}

// ValidateResp matches the /api/cli/validate response.
type ValidateResp struct {
	User struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"user"`
	Key struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	} `json:"key"`
}

// ErrPending is returned by Poll while the user hasn't yet approved the
// device. The caller should sleep `interval` and retry.
var ErrPending = errors.New("authorization pending")

// ErrExpired is returned by Poll when the device code has expired.
var ErrExpired = errors.New("device code expired")

// Client wraps the device-flow endpoints with a configured base URL.
type Client struct {
	AuthURL    string
	HTTPClient *http.Client
}

// NewClient builds a Client with a sensible default timeout.
func NewClient(authURL string) *Client {
	return &Client{
		AuthURL:    authURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// DeviceInit POSTs to /api/cli/device to start a device flow.
func (c *Client) DeviceInit(ctx context.Context) (*DeviceInitResp, error) {
	url := c.AuthURL + "/api/cli/device"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device init: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("device init failed (%d): %s", resp.StatusCode, truncate(string(body), 200))
	}
	var out DeviceInitResp
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("device init: invalid JSON: %w", err)
	}
	return &out, nil
}

// Poll polls /api/cli/token. Returns ErrPending while the user hasn't
// approved; ErrExpired when the code has timed out; the token on success.
func (c *Client) Poll(ctx context.Context, deviceCode, clientName string) (*TokenResp, error) {
	url := c.AuthURL + "/api/cli/token"
	body, _ := json.Marshal(map[string]string{
		"device_code": deviceCode,
		"client_name": clientName,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token poll: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode/100 == 2 {
		var out TokenResp
		if err := json.Unmarshal(respBody, &out); err != nil {
			return nil, fmt.Errorf("token poll: invalid JSON: %w", err)
		}
		return &out, nil
	}

	var errBody struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(respBody, &errBody)
	switch errBody.Error {
	case "authorization_pending":
		return nil, ErrPending
	case "expired_token":
		return nil, ErrExpired
	case "":
		return nil, fmt.Errorf("token poll failed (%d): %s", resp.StatusCode, truncate(string(respBody), 200))
	default:
		desc := errBody.ErrorDescription
		if desc == "" {
			desc = errBody.Error
		}
		return nil, fmt.Errorf("token poll failed (%d): %s", resp.StatusCode, desc)
	}
}

// Validate hits /api/cli/validate to resolve user details + check the token
// is still valid. Returns nil + error on failure.
func (c *Client) Validate(ctx context.Context, token string) (*ValidateResp, error) {
	url := c.AuthURL + "/api/cli/validate"
	body, _ := json.Marshal(map[string]string{"token": token})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("validate failed (%d): %s", resp.StatusCode, truncate(string(body), 200))
	}
	var out ValidateResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("validate: invalid JSON: %w", err)
	}
	return &out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
