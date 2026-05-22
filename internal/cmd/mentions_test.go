package cmd

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"mm-cli/internal/wire"
)

func TestScanMessageMentions(t *testing.T) {
	// Set up mock nodes and projects
	oldLoadNodes := loadNodesFunc
	oldLoadProjects := loadProjectsFunc
	defer func() {
		loadNodesFunc = oldLoadNodes
		loadProjectsFunc = oldLoadProjects
	}()

	loadNodesFunc = func(ctx context.Context) ([]wire.HubInstance, error) {
		return []wire.HubInstance{
			{Name: "fedora"},
			{Name: "ubuntu"},
			{Name: "ambiguous"},
		}, nil
	}

	loadProjectsFunc = func(ctx context.Context, targetNode string) ([]wire.AgentProject, error) {
		if targetNode == "fedora" {
			return []wire.AgentProject{
				{Label: "myproject"},
				{Label: "another"},
			}, nil
		}
		if targetNode == "ambiguous" {
			return []wire.AgentProject{
				{Label: "ambiguous"},
			}, nil
		}
		return nil, nil
	}

	tests := []struct {
		name            string
		message         string
		existingNode    string
		existingProject string
		expectedBody    string
		expectedNode    string
		expectedProj    string
		expectErr       bool
		expectedWarns   []string
	}{
		{
			name:         "Simple leading node mention",
			message:      "@fedora hello world",
			expectedBody: "hello world",
			expectedNode: "fedora",
		},
		{
			name:         "Simple leading node and project mentions",
			message:      "@fedora @myproject hello world",
			expectedBody: "hello world",
			expectedNode: "fedora",
			expectedProj: "myproject",
		},
		{
			name:         "Explicit node: prefix mention",
			message:      "@node:ubuntu hello",
			expectedBody: "hello",
			expectedNode: "ubuntu",
		},
		{
			name:         "Explicit project: prefix mention",
			message:      "@fedora @project:another hello",
			expectedBody: "hello",
			expectedNode: "fedora",
			expectedProj: "another",
		},
		{
			name:         "Lookbehind check: mid-sentence email address",
			message:      "my email is test@fedora.com",
			expectedBody: "my email is test@fedora.com",
			expectedNode: "",
			expectedProj: "",
		},
		{
			name:         "Lookbehind check: valid mention after punctuation",
			message:      "hello; @fedora ok",
			expectedBody: "hello; @fedora ok", // Not leading mention block, so not stripped
			expectedNode: "fedora",
			expectedProj: "",
		},
		{
			name:         "Unescaping @@",
			message:      "@@fedora is escaped",
			expectedBody: "@fedora is escaped",
			expectedNode: "",
			expectedProj: "",
		},
		{
			name:         "Overriding warnings",
			message:      "@ubuntu hello",
			existingNode: "fedora",
			expectedBody: "hello",
			expectedNode: "fedora", // flag overrides mention
			expectedWarns: []string{"warning: --node 'fedora' overrides @ubuntu"},
		},
		{
			name:            "Ambiguous mention error",
			message:         "@ambiguous test",
			existingNode:    "ambiguous",
			expectErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, node, project, warns, err := ScanMessageMentions(
				context.Background(),
				tt.message,
				tt.existingNode,
				tt.existingProject,
			)

			if (err != nil) != tt.expectErr {
				t.Fatalf("expected error: %v, got: %v", tt.expectErr, err)
			}
			if tt.expectErr {
				return
			}

			if body != tt.expectedBody {
				t.Errorf("expected body %q, got %q", tt.expectedBody, body)
			}
			if node != tt.expectedNode {
				t.Errorf("expected node %q, got %q", tt.expectedNode, node)
			}
			if project != tt.expectedProj {
				t.Errorf("expected project %q, got %q", tt.expectedProj, project)
			}

			// check warnings if any
			if len(warns) != len(tt.expectedWarns) {
				t.Errorf("expected %d warnings, got %d: %v", len(tt.expectedWarns), len(warns), warns)
			} else {
				for i, w := range warns {
					if !strings.Contains(w, tt.expectedWarns[i]) {
						t.Errorf("expected warning to contain %q, got %q", tt.expectedWarns[i], w)
					}
				}
			}
		})
	}
}

func TestPreprocessArgs(t *testing.T) {
	oldLoadNodes := loadNodesFunc
	oldLoadProjects := loadProjectsFunc
	defer func() {
		loadNodesFunc = oldLoadNodes
		loadProjectsFunc = oldLoadProjects
	}()

	loadNodesFunc = func(ctx context.Context) ([]wire.HubInstance, error) {
		return []wire.HubInstance{
			{Name: "fedora"},
			{Name: "ubuntu"},
		}, nil
	}

	loadProjectsFunc = func(ctx context.Context, targetNode string) ([]wire.AgentProject, error) {
		if targetNode == "fedora" {
			return []wire.AgentProject{
				{Label: "myproject"},
			}, nil
		}
		return nil, nil
	}

	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "No mentions",
			args:     []string{"chat", "list"},
			expected: []string{"chat", "list"},
		},
		{
			name:     "Simple node mention conversion",
			args:     []string{"chat", "list", "@fedora"},
			expected: []string{"chat", "list", "--node", "fedora"},
		},
		{
			name:     "Simple node and project mention conversion",
			args:     []string{"chat", "list", "@fedora", "@myproject"},
			expected: []string{"chat", "list", "--node", "fedora", "--project", "myproject"},
		},
		{
			name:     "Ignore @@ escaped mention",
			args:     []string{"chat", "list", "@@fedora"},
			expected: []string{"chat", "list", "@@fedora"},
		},
		{
			name:     "Pre-existing flag overrides mention",
			args:     []string{"chat", "list", "@ubuntu", "--node", "fedora"},
			expected: []string{"chat", "list", "--node", "fedora"}, // @ubuntu is consumed because it matches but gets overridden by --node fedora
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PreprocessArgs(context.Background(), tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("expected args %v, got %v", tt.expected, got)
			}
		})
	}
}
