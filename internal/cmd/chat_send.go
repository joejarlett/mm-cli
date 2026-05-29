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
		Long:  "Use --node to target a remote agent. --new creates a fresh thread; otherwise continues the most recently updated.",
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

	threadID, err := resolveSendThreadID(cmd.Context(), client, nodeFlag, isNew, threadFlag, projectFlag, title)
	if err != nil {
		return err
	}

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

// resolveSendThreadID figures out which thread to send to: --thread <id>,
// --new (creates one), or continues the most recently updated.
func resolveSendThreadID(
	ctx context.Context, client *mmhttp.Client,
	nodeFlag string, isNew bool, threadFlag, projectFlag, title string,
) (string, error) {
	if threadFlag != "" {
		return resolveThreadID(ctx, client, nodeFlag, threadFlag)
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
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			b, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("POST /api/threads %d: %s", resp.StatusCode, truncString(string(b), 200))
		}
		var data struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return "", err
		}
		return data.ID, nil
	}
	// Continue most recently updated.
	q := url.Values{}
	q.Set("limit", "1")
	if projectFlag != "" {
		q.Set("project_id", projectFlag)
	}
	resp, err := client.AgentFetch(ctx, nodeFlag, "/api/threads?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 && projectFlag != "" {
		return "", fmt.Errorf("project '%s' not found", projectFlag)
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("GET /api/threads %d", resp.StatusCode)
	}
	var data wire.AgentThreadsListResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if len(data.Threads) == 0 {
		return "", fmt.Errorf("no existing thread to continue. Pass --new to create one.")
	}
	return data.Threads[0].ID, nil
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Suppress strconv unused warning from possible future use.
var _ = strconv.Itoa
