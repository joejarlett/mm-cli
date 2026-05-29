package cmd

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"mm-cli/internal/wire"
)

func TestRunCmdRouting(t *testing.T) {
	// Mock history functions
	oldAuditList := auditListFunc
	oldAuditShow := auditShowFunc
	defer func() {
		auditListFunc = oldAuditList
		auditShowFunc = oldAuditShow
	}()

	var listCalled bool
	auditListFunc = func(ctx context.Context, req wire.HubAuditListReq) (wire.HubAuditListResp, error) {
		listCalled = true
		return wire.HubAuditListResp{
			Runs: []wire.HubAuditRunSummary{},
		}, nil
	}

	var showCalled bool
	auditShowFunc = func(ctx context.Context, req wire.HubAuditShowReq) (wire.HubAuditShowResp, error) {
		showCalled = true
		return wire.HubAuditShowResp{
			RunID: req.RunID,
		}, nil
	}

	// 1. Check mm run (no args) -> routes to list
	{
		listCalled = false
		buf := new(bytes.Buffer)
		c := NewRunCmd()
		c.SetOut(buf)
		c.SetErr(buf)
		c.SetArgs([]string{})

		err := c.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !listCalled {
			t.Errorf("expected runAuditList to be called for empty args")
		}
	}

	// 2. Check mm run list -> routes to subcommand list
	{
		listCalled = false
		buf := new(bytes.Buffer)
		c := NewRunCmd()
		c.SetOut(buf)
		c.SetErr(buf)
		c.SetArgs([]string{"list"})

		err := c.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !listCalled {
			t.Errorf("expected runAuditList to be called for subcommand 'list'")
		}
	}

	// 3. Check mm run show <id> -> routes to subcommand show
	{
		showCalled = false
		buf := new(bytes.Buffer)
		c := NewRunCmd()
		c.SetOut(buf)
		c.SetErr(buf)
		c.SetArgs([]string{"show", "run_123"})

		err := c.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !showCalled {
			t.Errorf("expected runAuditShow to be called for subcommand 'show'")
		}
	}
}

func TestRunDispatch(t *testing.T) {
	// Mock helpers
	oldLookPath := hermesLookPath
	oldHermesRun := hermesRunFunc
	oldResolveProj := resolveProjectRoot
	defer func() {
		hermesLookPath = oldLookPath
		hermesRunFunc = oldHermesRun
		resolveProjectRoot = oldResolveProj
	}()

	hermesLookPath = func(file string) (string, error) {
		if file == "hermes" {
			return "/usr/local/bin/hermes", nil
		}
		return "", errors.New("not found")
	}

	var lastRunName string
	var lastRunArgs []string
	var lastRunDir string
	var lastRunEnv []string
	var lastRunWait bool

	hermesRunFunc = func(ctx context.Context, name string, args []string, dir string, env []string, wait bool) error {
		lastRunName = name
		lastRunArgs = args
		lastRunDir = dir
		lastRunEnv = env
		lastRunWait = wait
		return nil
	}

	resolveProjectRoot = func(ctx context.Context, ref string) (string, error) {
		if ref == "keel" {
			return "/projects/keel", nil
		}
		return "", errors.New("project not found")
	}

	tests := []struct {
		name         string
		args         []string
		expectedArgs []string
		expectedEnv  string
		expectedDir  string
		expectedWait bool
		expectOut    string
		expectErr    bool
		errContains  string
	}{
		{
			name: "Simple background dispatch",
			args: []string{"refactor error handling"},
			expectedArgs: []string{
				"--worktree", "--yolo", "--accept-hooks", "--pass-session-id",
				"-s", "meta-me", "chat", "-q", "refactor error handling",
			},
			expectedEnv:  "HERMES_INFERENCE_MODEL=google/gemini-3.5-flash",
			expectedWait: false,
			expectOut:    "▶ Hermes running in background",
		},
		{
			name: "Foreground waiting dispatch with project and thread",
			args: []string{"refactor keel", "--project", "keel", "--thread", "thread_123", "--wait"},
			expectedArgs: []string{
				"--worktree", "--yolo", "--accept-hooks", "--pass-session-id",
				"-s", "meta-me", "chat", "-q", "MM_THREAD_ID=thread_123 refactor keel",
			},
			expectedDir:  "/projects/keel",
			expectedWait: true,
		},
		{
			name: "Model and extra skills override",
			args: []string{"test routes", "--model", "custom-model", "--skills", "skill-a,skill-b"},
			expectedArgs: []string{
				"--worktree", "--yolo", "--accept-hooks", "--pass-session-id",
				"-s", "meta-me,skill-a,skill-b", "chat", "-q", "test routes",
			},
			expectedEnv:  "HERMES_INFERENCE_MODEL=custom-model",
			expectedWait: false,
		},
		{
			name: "Dry run output",
			args: []string{"write docs", "--dry-run"},
			expectOut: `HERMES_INFERENCE_MODEL=google/gemini-3.5-flash hermes --worktree --yolo --accept-hooks --pass-session-id -s meta-me chat -q "write docs"`,
		},
		{
			name:        "Invalid project error",
			args:        []string{"build something", "--project", "nonexistent"},
			expectErr:   true,
			errContains: `Project "nonexistent" not found`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset trackers
			lastRunName = ""
			lastRunArgs = nil
			lastRunDir = ""
			lastRunEnv = nil
			lastRunWait = false

			buf := new(bytes.Buffer)
			c := NewRunCmd()
			c.SetOut(buf)
			c.SetErr(buf)
			c.SetArgs(tt.args)

			err := c.Execute()
			if (err != nil) != tt.expectErr {
				t.Fatalf("expected error: %v, got: %v", tt.expectErr, err)
			}

			if tt.expectErr {
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error to contain %q, got %v", tt.errContains, err)
				}
				return
			}

			gotOut := buf.String()
			if tt.expectOut != "" && !strings.Contains(gotOut, tt.expectOut) {
				t.Errorf("expected output to contain %q, got:\n%s", tt.expectOut, gotOut)
			}

			// If it's not a dry-run and not an error, verify spawned parameters
			dryRun, _ := c.Flags().GetBool("dry-run")
			if !dryRun {
				if lastRunName != "hermes" {
					t.Errorf("expected command name to be 'hermes', got %q", lastRunName)
				}
				if !reflect.DeepEqual(lastRunArgs, tt.expectedArgs) {
					t.Errorf("expected args %v, got %v", tt.expectedArgs, lastRunArgs)
				}
				if tt.expectedDir != "" && lastRunDir != tt.expectedDir {
					t.Errorf("expected dir %q, got %q", tt.expectedDir, lastRunDir)
				}
				if tt.expectedEnv != "" {
					foundEnv := false
					for _, envVar := range lastRunEnv {
						if envVar == tt.expectedEnv {
							foundEnv = true
							break
						}
					}
					if !foundEnv {
						t.Errorf("expected env list to contain %q, got %v", tt.expectedEnv, lastRunEnv)
					}
				}
				if lastRunWait != tt.expectedWait {
					t.Errorf("expected wait %v, got %v", tt.expectedWait, lastRunWait)
				}
			}
		})
	}
}
