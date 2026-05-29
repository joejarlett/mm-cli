package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	mmhttp "mm-cli/internal/http"
	"mm-cli/internal/wire"
)

// NewHubCmd builds the `mm hub` tree — hub meta-agent conversations.
func NewHubCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "hub",
		Short: "Hub meta-agent conversations (list/show/send)",
	}
	c.AddCommand(newHubListCmd(), newHubShowCmd(), newHubSendCmd())
	return c
}

func newHubListCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "list",
		Short: "Recent hub conversations",
		Args:  cobra.NoArgs,
		RunE:  runHubList,
	}
	c.Flags().Int("limit", 20, "Max rows")
	return c
}

func newHubShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Print messages in a hub conversation",
		Args:  cobra.ExactArgs(1),
		RunE:  runHubShow,
	}
}

func newHubSendCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "send <message>",
		Short: "Send a message to the hub meta-agent (streams to stdout)",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runHubSend,
	}
	c.Flags().Bool("new", false, "Create a new conversation")
	c.Flags().String("thread", "", "Conversation ID or prefix")
	c.Flags().String("model", "", "Model ID (e.g. gemini-2.5-pro)")
	return c
}

// ─── list ──────────────────────────────────────────────────────────────

func runHubList(cmd *cobra.Command, _ []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	if limit <= 0 {
		limit = 20
	}
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	client := mmhttp.New()

	resp, err := client.HubFetch(cmd.Context(), http.MethodGet,
		fmt.Sprintf("/api/conversations?limit=%d", limit), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GET /api/conversations %d", resp.StatusCode)
	}
	var data wire.HubConversationsListResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(data.Conversations, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	if len(data.Conversations) == 0 {
		fmt.Println("(no conversations)")
		return nil
	}
	for _, c := range data.Conversations {
		id6 := c.ID
		if len(id6) > 6 {
			id6 = id6[:6]
		}
		pinned := ""
		if c.Pinned {
			pinned = " [pinned]"
		}
		fmt.Printf("%s  %s  %s%s\n",
			id6, padRight(relTimeISO(c.UpdatedAt), 8), truncString(c.Title, 60), pinned)
	}
	return nil
}

// ─── show ──────────────────────────────────────────────────────────────

func runHubShow(cmd *cobra.Command, args []string) error {
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")
	client := mmhttp.New()

	id, err := resolveHubConversationID(cmd.Context(), client, args[0])
	if err != nil {
		return err
	}
	resp, err := client.HubFetch(cmd.Context(), http.MethodGet,
		"/api/conversations/"+id+"/messages", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GET /api/conversations/%s/messages %d", id, resp.StatusCode)
	}
	var data wire.HubMessagesListResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}
	if wantJSON {
		out, _ := json.MarshalIndent(data.Messages, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	for _, m := range data.Messages {
		fmt.Printf("── %s ──\n", m.Role)
		fmt.Println(m.Content)
		fmt.Println()
	}
	return nil
}

// ─── send ──────────────────────────────────────────────────────────────

func runHubSend(cmd *cobra.Command, args []string) error {
	message := strings.Join(args, " ")
	isNew, _ := cmd.Flags().GetBool("new")
	threadFlag, _ := cmd.Flags().GetString("thread")
	modelFlag, _ := cmd.Flags().GetString("model")
	wantJSON, _ := cmd.Root().PersistentFlags().GetBool("json")

	if isNew && threadFlag != "" {
		return fmt.Errorf("--new and --thread are mutually exclusive")
	}

	client := mmhttp.New()

	conversationID, err := resolveHubSendConversationID(cmd.Context(), client, isNew, threadFlag)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"conversationId": conversationID,
		"content":        message,
	}
	if modelFlag != "" {
		payload["model"] = modelFlag
	}
	body, _ := json.Marshal(payload)

	resp, err := client.HubStream(cmd.Context(), "/api/chat", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST /api/chat %d: %s", resp.StatusCode, truncString(string(b), 200))
	}
	return streamSSE(resp.Body, wantJSON)
}

// streamSSE reads SSE events and prints to stdout. Same event shape as
// the desk WebSocket stream (delta / tool_start / tool_end / done / error).
func streamSSE(r io.Reader, wantJSON bool) error {
	scanner := bufio.NewScanner(r)
	streamedAnything := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var evt map[string]any
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}
		etype, _ := evt["type"].(string)
		if wantJSON {
			fmt.Println(data)
			if etype == "done" || etype == "error" {
				break
			}
			continue
		}
		switch etype {
		case "delta":
			if t, ok := evt["text"].(string); ok && t != "" {
				fmt.Print(t)
				streamedAnything = true
			}
		case "done":
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
			fmt.Println()
			msg := "unknown"
			if m, ok := evt["message"].(string); ok {
				msg = m
			}
			return fmt.Errorf("%s", msg)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if streamedAnything {
		fmt.Println()
	}
	return nil
}

// ─── helpers ───────────────────────────────────────────────────────────

func resolveHubConversationID(ctx context.Context, client *mmhttp.Client, prefix string) (string, error) {
	if len(prefix) >= 36 {
		return prefix, nil
	}
	if len(prefix) < 4 {
		return "", fmt.Errorf("conversation prefix '%s' is too short (need ≥4 chars)", prefix)
	}
	resp, err := client.HubFetch(ctx, http.MethodGet, "/api/conversations?limit=200", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("GET /api/conversations %d", resp.StatusCode)
	}
	var data wire.HubConversationsListResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	var matches []string
	for _, c := range data.Conversations {
		if strings.HasPrefix(c.ID, strings.ToLower(prefix)) {
			matches = append(matches, c.ID)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("conversation not found: %s", prefix)
	case 1:
		return matches[0], nil
	}
	return "", fmt.Errorf("conversation prefix '%s' is ambiguous (%d matches)", prefix, len(matches))
}

func resolveHubSendConversationID(ctx context.Context, client *mmhttp.Client, isNew bool, threadFlag string) (string, error) {
	if threadFlag != "" {
		return resolveHubConversationID(ctx, client, threadFlag)
	}
	if isNew {
		body, _ := json.Marshal(map[string]any{})
		resp, err := client.HubFetch(ctx, http.MethodPost, "/api/conversations", body)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			b, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("POST /api/conversations %d: %s", resp.StatusCode, truncString(string(b), 200))
		}
		var conv wire.HubConversation
		if err := json.NewDecoder(resp.Body).Decode(&conv); err != nil {
			return "", err
		}
		return conv.ID, nil
	}
	resp, err := client.HubFetch(ctx, http.MethodGet, "/api/conversations?limit=1", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("GET /api/conversations %d", resp.StatusCode)
	}
	var data wire.HubConversationsListResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if len(data.Conversations) == 0 {
		return "", fmt.Errorf("no existing conversation — pass --new to create one")
	}
	return data.Conversations[0].ID, nil
}

func relTimeISO(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05Z07:00", ts)
		if err != nil {
			return "?"
		}
	}
	diff := time.Since(t)
	s := int(diff.Seconds())
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	m := s / 60
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	h := m / 60
	if h < 24 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dd", h/24)
}
