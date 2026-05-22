package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"mm-cli/internal/wire"
)

func TestFeedbackCmd(t *testing.T) {
	// Set up mock submit function
	oldSubmitFeedback := submitFeedbackFunc
	defer func() {
		submitFeedbackFunc = oldSubmitFeedback
	}()

	var lastReq wire.HubFeedbackSubmitReq
	mockResp := wire.HubFeedbackSubmitResp{
		ID: "fb_12345",
	}

	submitFeedbackFunc = func(ctx context.Context, req wire.HubFeedbackSubmitReq) (wire.HubFeedbackSubmitResp, error) {
		lastReq = req
		return mockResp, nil
	}

	tests := []struct {
		name         string
		args         []string
		env          map[string]string
		expectErr    bool
		errContains  string
		expectedReq  *wire.HubFeedbackSubmitReq
		expectStdout string
		verifyAgent  bool
	}{
		{
			name:        "Empty message error",
			args:        []string{},
			expectErr:   true,
			errContains: "feedback message is required",
		},
		{
			name: "Simple feedback with defaults",
			args: []string{"unintuitive CLI behavior"},
			expectedReq: &wire.HubFeedbackSubmitReq{
				Message: "unintuitive CLI behavior",
				AppSlug: "mm",
			},
			expectStdout: "✓ Filed feedback fb_12345 (friction, app: mm)",
		},
		{
			name: "Feedback with custom app, kind and context",
			args: []string{"submit", "unintuitive KB behavior", "--app", "kb", "--kind", "bug", "--context", "some error details"},
			expectedReq: &wire.HubFeedbackSubmitReq{
				Message: "unintuitive KB behavior",
				AppSlug: "kb",
				URL:     "some error details",
			},
			expectStdout: "✓ Filed feedback fb_12345 (bug, app: kb)",
		},
		{
			name:        "Invalid kind rejection",
			args:        []string{"something", "--kind", "invalidkind"},
			expectErr:   true,
			errContains: `invalid classification kind: "invalidkind"`,
		},
		{
			name: "Agent source detection",
			args: []string{"agent feedback message"},
			env:  map[string]string{"MM_AGENT": "true"},
			expectedReq: &wire.HubFeedbackSubmitReq{
				Message: "agent feedback message",
				AppSlug: "mm",
			},
			verifyAgent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment variables
			for k, v := range tt.env {
				os.Setenv(k, v)
			}
			defer func() {
				for k := range tt.env {
					os.Unsetenv(k)
				}
			}()

			buf := new(bytes.Buffer)
			c := NewFeedbackCmd()
			c.SetOut(buf)
			c.SetErr(buf)
			c.SetArgs(tt.args)

			err := c.Execute()
			if (err != nil) != tt.expectErr {
				t.Fatalf("expected error: %v, got: %v", tt.expectErr, err)
			}

			if tt.expectErr {
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got: %v", tt.errContains, err)
				}
				return
			}

			// Verify fields passed to API request
			if tt.expectedReq != nil {
				if lastReq.Message != tt.expectedReq.Message {
					t.Errorf("expected message %q, got %q", tt.expectedReq.Message, lastReq.Message)
				}
				if lastReq.AppSlug != tt.expectedReq.AppSlug {
					t.Errorf("expected appSlug %q, got %q", tt.expectedReq.AppSlug, lastReq.AppSlug)
				}
				if lastReq.URL != tt.expectedReq.URL {
					t.Errorf("expected URL %q, got %q", tt.expectedReq.URL, lastReq.URL)
				}
				if tt.verifyAgent {
					if !strings.Contains(lastReq.UserAgent, "source: agent") {
						t.Errorf("expected UserAgent to contain source: agent, got %q", lastReq.UserAgent)
					}
				} else {
					if !strings.Contains(lastReq.UserAgent, "source: cli") {
						t.Errorf("expected UserAgent to contain source: cli, got %q", lastReq.UserAgent)
					}
				}
			}

			// Verify printed output
			if tt.expectStdout != "" {
				gotOut := strings.TrimSpace(buf.String())
				if !strings.Contains(gotOut, tt.expectStdout) {
					t.Errorf("expected output to contain %q, got %q", tt.expectStdout, gotOut)
				}
			}
		})
	}
}

func TestFeedbackCmdJson(t *testing.T) {
	oldSubmitFeedback := submitFeedbackFunc
	defer func() {
		submitFeedbackFunc = oldSubmitFeedback
	}()

	mockResp := wire.HubFeedbackSubmitResp{
		ID: "fb_json_123",
	}

	submitFeedbackFunc = func(ctx context.Context, req wire.HubFeedbackSubmitReq) (wire.HubFeedbackSubmitResp, error) {
		return mockResp, nil
	}

	buf := new(bytes.Buffer)
	c := NewFeedbackCmd()
	c.SetOut(buf)
	c.SetErr(buf)
	// Bind a persistent flag like --json
	c.PersistentFlags().Bool("json", true, "")
	c.PersistentFlags().Set("json", "true")
	c.SetArgs([]string{"my feedback"})

	err := c.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gotResp map[string]string
	if err := json.Unmarshal(buf.Bytes(), &gotResp); err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v, output: %s", err, buf.String())
	}

	if gotResp["id"] != mockResp.ID {
		t.Errorf("expected ID %q, got %q", mockResp.ID, gotResp["id"])
	}
	if gotResp["status"] != "filed" {
		t.Errorf("expected status 'filed', got %q", gotResp["status"])
	}
}
