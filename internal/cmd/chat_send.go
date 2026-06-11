package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	mmhttp "mm-cli/internal/http"
	"mm-cli/internal/wire"
)

func newChatSendCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "send [message]",
		Short: "Drive a turn on the local agent (streams to stdout)",
		Long: "Drive a turn on the local agent. Use --node to target a remote agent.\n\n" +
			"Thread selection:\n" +
			"  --thread <id>   continue a specific thread (UUID or 6-char prefix)\n" +
			"  --new           start a fresh thread (--title / --project optional)\n" +
			"  (neither)       continue the MOST RECENTLY UPDATED thread\n\n" +
			"Because the default appends to whatever thread was touched last, every\n" +
			"send echoes its target to stderr (e.g. `→ continuing 2e8065 \"…\"`). For a\n" +
			"one-off task that shouldn't land in an unrelated thread, pass --new.",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runChatSend,
	}
	c.Flags().String("node", "", "Target a remote agent by name")
	c.Flags().Bool("new", false, "Create a new thread")
	c.Flags().String("title", "", "Thread title (with --new)")
	c.Flags().String("model", "", "Model id (provider/id form)")
	c.Flags().String("thread", "", "Thread id (UUID or 6-char prefix)")
	c.Flags().String("project", "", "Project UUID or label")
	return c
}

func runChatSend(cmd *cobra.Command, args []string) error {
	rawMessage := strings.Join(args, " ")
	if rawMessage == "" {
		return fmt.Errorf("Usage: mm desk send \"<message>\"")
	}

	isNew, _ := cmd.Flags().GetBool("new")
	title, _ := cmd.Flags().GetString("title")
	modelFlag, _ := cmd.Flags().GetString("model")
	threadFlag, _ := cmd.Flags().GetString("thread")
	projectFlag, _ := cmd.Flags().GetString("project")
	nodeFlag, _ := cmd.Flags().GetString("node")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	// Parse @<entity> mentions out of the message.
	body, resolvedNode, resolvedProject, warnings, err := ScanMessageMentions(cmd.Context(), rawMessage, nodeFlag, projectFlag)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, w)
	}

	message := strings.TrimSpace(body)
	if message == "" {
		return fmt.Errorf("message is empty after parsing mentions")
	}

	// Update node and project flags from resolved mentions
	nodeFlag = resolvedNode
	projectFlag = resolvedProject

	client := mmhttp.New()

	target, err := client.AgentBase(cmd.Context(), nodeFlag)
	if err != nil {
		return err
	}

	if isNew && threadFlag != "" {
		return fmt.Errorf("--new and --thread are mutually exclusive")
	}

	var provider, modelID string
	if modelFlag != "" {
		sl := strings.IndexByte(modelFlag, '/')
		if sl <= 0 {
			return fmt.Errorf("--model must be provider-prefixed, e.g. google/gemini-3.5-flash")
		}
		provider = modelFlag[:sl]
		modelID = modelFlag[sl+1:]
	}

	tgt, err := resolveSendThreadID(cmd.Context(), client, nodeFlag, isNew, threadFlag, projectFlag, title)
	if err != nil {
		return err
	}
	threadID := tgt.ID

	// Echo which thread this send landed in, so the target is never a silent
	// guess (the default continues the most-recently-updated thread). Goes to
	// stderr to keep stdout reserved for the streamed turn / --json payload.
	echoSendTarget(tgt)

	// Prepare initial send payload
	payload := map[string]any{"type": "send", "threadId": threadID, "content": message}
	if provider != "" && modelID != "" {
		payload["provider"] = provider
		payload["modelId"] = modelID
	}
	if projectFlag != "" && !isNew {
		payload["projectId"] = projectFlag
	}
	initialPayload, _ := json.Marshal(payload)

	wsURL := target.WS + "/ws"

	return streamWSWithReconnect(cmd.Context(), client, wsURL, threadID, wantJSON, initialPayload)
}

// streamWSWithReconnect dials, reads events, tracks the cursor, and handles unexpected drops.
func streamWSWithReconnect(ctx context.Context, client *mmhttp.Client, wsURL string, threadID string, wantJSON bool, initialPayload []byte) error {
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))
	streamedAnything := false
	statusActive := false

	clearStatus := func() {
		if statusActive && isTTY {
			fmt.Print("\r" + strings.Repeat(" ", 60) + "\r")
		}
		statusActive = false
	}

	var lastCursor *int64
	var conn *websocket.Conn
	var err error

	maxAttempts := 5
	attempt := 0

	// Initial Dial
	for {
		conn, _, err = websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			attempt++
			if attempt >= maxAttempts {
				return fmt.Errorf("WebSocket dial %s: %w", wsURL, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
				continue
			}
		}
		break
	}
	defer func() {
		if conn != nil {
			conn.CloseNow()
		}
	}()

	if err := conn.Write(ctx, websocket.MessageText, initialPayload); err != nil {
		return fmt.Errorf("WS write: %w", err)
	}

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			// Resilient Reconnect
			if lastCursor != nil {
				clearStatus()
				fmt.Fprintln(os.Stderr, "\n⚠️ Connection lost. Reconnecting...")
				conn.CloseNow()
				conn = nil

				reconnectSuccess := false
				for attempt = 1; attempt <= maxAttempts; attempt++ {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(1 * time.Second):
					}

					conn, _, err = websocket.Dial(ctx, wsURL, nil)
					if err != nil {
						fmt.Fprintf(os.Stderr, "  Retrying connection (attempt %d/%d)...\n", attempt, maxAttempts)
						continue
					}

					resumePayload := map[string]any{
						"type":     "resume",
						"threadId": threadID,
						"cursor":   *lastCursor,
					}
					resumeJSON, _ := json.Marshal(resumePayload)
					if err := conn.Write(ctx, websocket.MessageText, resumeJSON); err != nil {
						conn.CloseNow()
						conn = nil
						continue
					}

					reconnectSuccess = true
					fmt.Fprintln(os.Stderr, "✓ Reconnected. Resuming stream...")
					break
				}

				if !reconnectSuccess {
					return fmt.Errorf("connection lost and failed to reconnect after %d attempts", maxAttempts)
				}
				continue
			}

			if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "EOF") {
				if !streamedAnything {
					return fmt.Errorf("connection closed before turn completed")
				}
				return nil
			}
			return err
		}

		var evt map[string]any
		if err := json.Unmarshal(data, &evt); err != nil {
			continue
		}

		etype, _ := evt["type"].(string)

		if etype == "resume_empty" {
			clearStatus()
			return fmt.Errorf("stream session expired on the agent; unable to resume from cursor")
		}

		if cVal, exists := evt["cursor"]; exists {
			if num, ok := cVal.(float64); ok {
				cInt := int64(num)
				if lastCursor != nil && cInt <= *lastCursor {
					continue
				}
				lastCursor = &cInt
			}
		}

		if wantJSON {
			fmt.Println(string(data))
			if etype == "done" {
				return nil
			}
			if etype == "error" {
				if msg, ok := evt["message"].(string); ok {
					return fmt.Errorf("%s", msg)
				}
				return fmt.Errorf("WS error")
			}
			continue
		}

		switch etype {
		case "delta":
			clearStatus()
			if t, ok := evt["text"].(string); ok && t != "" {
				fmt.Print(t)
				streamedAnything = true
			}
		case "tool_start":
			if isTTY {
				clearStatus()
				name := "tool"
				if n, ok := evt["toolName"].(string); ok && n != "" {
					name = n
				}
				fmt.Printf("\r· running %s", name)
				statusActive = true
			}
		case "tool_end":
			clearStatus()
		case "thinking_delta", "status":
			// suppress
		case "done":
			clearStatus()
			if !streamedAnything {
				if ft, ok := evt["fullText"].(string); ok && ft != "" {
					fmt.Print(ft)
					streamedAnything = true
				}
			}
			if streamedAnything {
				fmt.Println()
			}
			return nil
		case "error":
			clearStatus()
			msg := "unknown"
			if m, ok := evt["message"].(string); ok {
				msg = m
			}
			return fmt.Errorf("%s", msg)
		}
	}
}

// sendTarget describes the thread a send resolved to, so the caller can echo
// it. Title may be empty when the agent didn't return one (e.g. an untitled
// --new thread); the echo falls back to the id alone in that case.
type sendTarget struct {
	ID    string
	Title string
	IsNew bool
}

// echoSendTarget prints a one-line confirmation of where a send landed, to
// stderr (stdout is reserved for the streamed turn). Mirrors the `→` prefix
// used elsewhere for refs in the desk overview.
func echoSendTarget(t sendTarget) {
	id6 := t.ID
	if len(id6) > 6 {
		id6 = id6[:6]
	}
	verb := "continuing"
	if t.IsNew {
		verb = "new thread"
	}
	if title := strings.Join(strings.Fields(t.Title), " "); title != "" {
		fmt.Fprintf(os.Stderr, "→ %s %s %q\n", verb, id6, truncString(title, 48))
	} else {
		fmt.Fprintf(os.Stderr, "→ %s %s\n", verb, id6)
	}
}

// resolveSendThreadID figures out which thread to send to: --thread <id>,
// --new (creates one), or continues the most recently updated. It returns the
// resolved thread (id + title where cheaply known) so the caller can echo it.
func resolveSendThreadID(
	ctx context.Context, client *mmhttp.Client,
	nodeFlag string, isNew bool, threadFlag, projectFlag, title string,
) (sendTarget, error) {
	if threadFlag != "" {
		id, t, err := resolveThreadWithTitle(ctx, client, nodeFlag, threadFlag)
		if err != nil {
			return sendTarget{}, err
		}
		return sendTarget{ID: id, Title: t}, nil
	}
	if isNew {
		body := map[string]any{}
		if title != "" {
			body["title"] = title
		}
		if projectFlag != "" {
			body["project_id"] = projectFlag
		}
		bodyJSON, _ := json.Marshal(body)
		resp, err := client.AgentFetch(ctx, nodeFlag, "/api/threads", &mmhttp.AgentReq{
			Method:      "POST",
			Body:        bodyJSON,
			ContentType: "application/json",
		})
		if err != nil {
			return sendTarget{}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			b, _ := io.ReadAll(resp.Body)
			return sendTarget{}, fmt.Errorf("POST /api/threads %d: %s", resp.StatusCode, truncString(string(b), 200))
		}
		var data struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return sendTarget{}, err
		}
		// Prefer the server's title; fall back to the --title flag we sent.
		respTitle := data.Title
		if respTitle == "" {
			respTitle = title
		}
		return sendTarget{ID: data.ID, Title: respTitle, IsNew: true}, nil
	}
	// Continue most recently updated.
	q := url.Values{}
	q.Set("limit", "1")
	if projectFlag != "" {
		q.Set("project_id", projectFlag)
	}
	resp, err := client.AgentFetch(ctx, nodeFlag, "/api/threads?"+q.Encode(), nil)
	if err != nil {
		return sendTarget{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 && projectFlag != "" {
		return sendTarget{}, fmt.Errorf("project '%s' not found", projectFlag)
	}
	if resp.StatusCode/100 != 2 {
		return sendTarget{}, fmt.Errorf("GET /api/threads %d", resp.StatusCode)
	}
	var data wire.AgentThreadsListResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return sendTarget{}, err
	}
	if len(data.Threads) == 0 {
		return sendTarget{}, fmt.Errorf("no existing thread to continue. Pass --new to create one.")
	}
	return sendTarget{ID: data.Threads[0].ID, Title: data.Threads[0].Title}, nil
}

// resolveThreadWithTitle resolves a --thread arg (UUID or prefix) to a full id
// and, when it can do so without an extra round-trip, the thread title. The
// threads list is fetched anyway for prefix matches and to look the title up.
func resolveThreadWithTitle(ctx context.Context, client *mmhttp.Client, node, arg string) (string, string, error) {
	resp, err := client.AgentFetch(ctx, node, "/api/threads?limit=1000", nil)
	if err != nil {
		// Fall back to the id-only resolver so a list failure never blocks a send.
		id, rerr := resolveThreadID(ctx, client, node, arg)
		return id, "", rerr
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		id, rerr := resolveThreadID(ctx, client, node, arg)
		return id, "", rerr
	}
	var data wire.AgentThreadsListResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		id, rerr := resolveThreadID(ctx, client, node, arg)
		return id, "", rerr
	}

	if uuidRe.MatchString(arg) {
		for _, t := range data.Threads {
			if t.ID == arg {
				return t.ID, t.Title, nil
			}
		}
		return arg, "", nil // valid UUID not in the recent list; send still works
	}
	if len(arg) < 4 {
		return "", "", fmt.Errorf("thread prefix '%s' is too short (need ≥4 chars)", arg)
	}
	var matches []wire.AgentThread
	for _, t := range data.Threads {
		if strings.HasPrefix(t.ID, strings.ToLower(arg)) {
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("thread not found: %s", arg)
	case 1:
		return matches[0].ID, matches[0].Title, nil
	}
	return "", "", fmt.Errorf("thread prefix '%s' is ambiguous (%d matches)", arg, len(matches))
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Suppress strconv unused warning from possible future use.
var _ = strconv.Itoa
