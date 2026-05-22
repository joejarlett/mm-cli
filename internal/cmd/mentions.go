package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"mm-cli/internal/http"
	"mm-cli/internal/wire"
)

type ClassifyResult struct {
	Type string // "node" or "project"
	Name string
}

type ResolvedMention struct {
	Start int
	End   int
	Type  string // "node" or "project"
	Name  string
}

var loadNodesFunc = func(ctx context.Context) ([]wire.HubInstance, error) {
	client := http.New()
	return client.LoadNodes(ctx)
}

var loadProjectsFunc = func(ctx context.Context, targetNode string) ([]wire.AgentProject, error) {
	client := http.New()
	resp, err := client.AgentFetch(ctx, targetNode, "/api/projects", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	var data wire.AgentProjectsListResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data.Projects, nil
}

// lookupNodeName checks if a node name matches any registered node on the Hub.
func lookupNodeName(ctx context.Context, name string) bool {
	nodes, err := loadNodesFunc(ctx)
	if err != nil {
		return false
	}
	lower := strings.ToLower(name)
	for _, n := range nodes {
		if strings.EqualFold(n.Name, lower) {
			return true
		}
	}
	return false
}

// lookupProjectName checks if a project label matches any project on the target node.
func lookupProjectName(ctx context.Context, name string, targetNode string) bool {
	if targetNode == "" {
		return false
	}
	projects, err := loadProjectsFunc(ctx, targetNode)
	if err != nil {
		return false
	}
	lower := strings.ToLower(name)
	for _, p := range projects {
		if strings.EqualFold(p.Label, lower) {
			return true
		}
	}
	return false
}

// ClassifyToken classifies a token (without the leading '@') into a node or project.
func ClassifyToken(ctx context.Context, token string, contextNode string) (*ClassifyResult, error) {
	if strings.HasPrefix(token, "node:") {
		name := token[len("node:"):]
		if len(name) > 0 {
			return &ClassifyResult{Type: "node", Name: name}, nil
		}
		return nil, nil
	}
	if strings.HasPrefix(token, "project:") {
		name := token[len("project:"):]
		if len(name) > 0 {
			return &ClassifyResult{Type: "project", Name: name}, nil
		}
		return nil, nil
	}

	isNode := lookupNodeName(ctx, token)
	isProject := lookupProjectName(ctx, token, contextNode)

	if isNode && isProject {
		return nil, fmt.Errorf("'@%s' is ambiguous (matches both node '%s' and project '%s'). Use @node:%s or @project:%s", token, token, token, token, token)
	}
	if isNode {
		return &ClassifyResult{Type: "node", Name: token}, nil
	}
	if isProject {
		return &ClassifyResult{Type: "project", Name: token}, nil
	}
	return nil, nil
}

// ScanMessageMentions parses mentions out of a message string.
func ScanMessageMentions(ctx context.Context, message string, existingNode, existingProject string) (body string, node string, project string, warnings []string, err error) {
	node = existingNode
	project = existingProject

	// Collect matches using the manually simulated negative lookbehind (?<![@\w])
	re := regexp.MustCompile(`@([a-zA-Z0-9][\w.:-]*)`)
	indices := re.FindAllStringSubmatchIndex(message, -1)

	type rawMatch struct {
		start int
		end   int
		token string
	}
	var matches []rawMatch
	for _, ind := range indices {
		start := ind[0]
		end := ind[1]
		tokenStart := ind[2]
		tokenEnd := ind[3]
		token := message[tokenStart:tokenEnd]

		if start > 0 {
			prev := message[start-1]
			isWord := (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9') || prev == '_' || prev == '@'
			if isWord {
				continue
			}
		}
		matches = append(matches, rawMatch{start: start, end: end, token: token})
	}

	// Resolve in order
	contextNode := existingNode
	var resolved []ResolvedMention
	for _, match := range matches {
		cls, err := ClassifyToken(ctx, match.token, contextNode)
		if err != nil {
			return "", "", "", nil, err
		}
		if cls == nil {
			continue
		}
		if cls.Type == "node" && contextNode == "" {
			contextNode = cls.Name
		}
		resolved = append(resolved, ResolvedMention{
			Start: match.start,
			End:   match.end,
			Type:  cls.Type,
			Name:  cls.Name,
		})
	}

	// First-wins-per-axis; explicit overrides
	nodeClaimed := existingNode != ""
	projectClaimed := existingProject != ""
	for _, r := range resolved {
		if r.Type == "node" {
			if existingNode != "" {
				if !strings.EqualFold(existingNode, r.Name) {
					warnings = append(warnings, fmt.Sprintf("warning: --node '%s' overrides @%s", existingNode, r.Name))
				}
			} else if !nodeClaimed {
				node = r.Name
				nodeClaimed = true
			}
		} else {
			if existingProject != "" {
				if !strings.EqualFold(existingProject, r.Name) {
					warnings = append(warnings, fmt.Sprintf("warning: --project '%s' overrides @%s", existingProject, r.Name))
				}
			} else if !projectClaimed {
				project = r.Name
				projectClaimed = true
			}
		}
	}

	// Strip resolved mentions in the leading block
	cursor := 0
	for cursor < len(message) {
		rn, size := utf8.DecodeRuneInString(message[cursor:])
		if unicode.IsSpace(rn) {
			cursor += size
		} else {
			break
		}
	}

	stripped := false
	for _, r := range resolved {
		if r.Start != cursor {
			break
		}
		cursor = r.End
		for cursor < len(message) {
			rn, size := utf8.DecodeRuneInString(message[cursor:])
			if unicode.IsSpace(rn) {
				cursor += size
			} else {
				break
			}
		}
		stripped = true
	}

	if stripped {
		body = message[cursor:]
	} else {
		body = message
	}

	// Unescape @@<token> to @<token>
	escapeRe := regexp.MustCompile(`@@([a-zA-Z0-9][\w.:-]*)`)
	body = escapeRe.ReplaceAllString(body, "@$1")

	return body, node, project, warnings, nil
}

// getFlag helper to look up a flag from raw string arguments.
func getFlagValue(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// PreprocessArgs intercepts os.Args[1:] before Cobra executes to convert mentions to flags.
func PreprocessArgs(ctx context.Context, args []string) ([]string, error) {
	// Find the first index where an argument is a mention (starts with @, len > 1, not starting with @@)
	startIdx := -1
	for i, arg := range args {
		if strings.HasPrefix(arg, "@") && len(arg) > 1 && !strings.HasPrefix(arg, "@@") {
			startIdx = i
			break
		}
	}

	// If no mentions found, return args unchanged
	if startIdx == -1 {
		return args, nil
	}

	// Consume contiguous mentions
	var tokens []string
	endIdx := startIdx
	for endIdx < len(args) {
		arg := args[endIdx]
		if strings.HasPrefix(arg, "@") && len(arg) > 1 && !strings.HasPrefix(arg, "@@") {
			tokens = append(tokens, arg[1:])
			endIdx++
		} else {
			break
		}
	}

	existingNode := getFlagValue(args, "--node")
	existingProject := getFlagValue(args, "--project")
	contextNode := existingNode

	node := existingNode
	project := existingProject
	nodeClaimed := existingNode != ""
	projectClaimed := existingProject != ""

	var dropped []int
	var warnings []string

	for j, token := range tokens {
		cls, err := ClassifyToken(ctx, token, contextNode)
		if err != nil {
			return nil, err
		}
		if cls == nil {
			continue
		}
		if cls.Type == "node" && contextNode == "" {
			contextNode = cls.Name
		}

		if cls.Type == "node" {
			if existingNode != "" {
				if !strings.EqualFold(existingNode, cls.Name) {
					warnings = append(warnings, fmt.Sprintf("warning: --node '%s' overrides @%s", existingNode, cls.Name))
				}
				dropped = append(dropped, j)
			} else if !nodeClaimed {
				node = cls.Name
				nodeClaimed = true
				dropped = append(dropped, j)
			}
		} else {
			if existingProject != "" {
				if !strings.EqualFold(existingProject, cls.Name) {
					warnings = append(warnings, fmt.Sprintf("warning: --project '%s' overrides @%s", existingProject, cls.Name))
				}
				dropped = append(dropped, j)
			} else if !projectClaimed {
				project = cls.Name
				projectClaimed = true
				dropped = append(dropped, j)
			}
		}
	}

	// Print warnings to stderr
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, w) // or log it
	}

	// Build the new args array
	var newArgs []string
	newArgs = append(newArgs, args[:startIdx]...)

	droppedMap := make(map[int]bool)
	for _, idx := range dropped {
		droppedMap[idx] = true
	}

	for j, token := range tokens {
		if !droppedMap[j] {
			newArgs = append(newArgs, "@"+token)
		}
	}

	newArgs = append(newArgs, args[endIdx:]...)

	if node != "" && existingNode == "" {
		newArgs = append(newArgs, "--node", node)
	}
	if project != "" && existingProject == "" {
		newArgs = append(newArgs, "--project", project)
	}

	return newArgs, nil
}
