package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Hellhium/discord-mcp/internal/auth"
)

func appendMsg(h *harness, channel, author, content string) {
	h.p.Events.Append("MESSAGE_CREATE", json.RawMessage(fmt.Sprintf(
		`{"id":"1%s","channel_id":%q,"guild_id":"9","content":%q,"author":{"id":%q,"username":"u%s"},"timestamp":"2026-09-14T11:02:00Z"}`,
		author, channel, content, author, author)))
}

func TestPollEvents(t *testing.T) {
	h := newHarness(t, auth.CapBotEvents)
	appendMsg(h, "5", "7", "hello")
	h.p.Events.Append("MESSAGE_REACTION_ADD", json.RawMessage(`{"channel_id":"5","user_id":"8","guild_id":"9","emoji":{"name":"👍"}}`))

	text, isErr := h.call(pollEventsTool(), map[string]any{})
	if isErr {
		t.Fatal(text)
	}
	cursor := h.p.Events.Cursor()
	for _, want := range []string{"next_cursor: " + cursor, "#1 MESSAGE_CREATE [2026-09-14 11:02] u7 (7) #17: hello", "#2 MESSAGE_REACTION_ADD guild=9 channel=5 user=8"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}

	appendMsg(h, "6", "7", "elsewhere")
	text, _ = h.call(pollEventsTool(), map[string]any{"cursor": cursor, "channel_id": "5"})
	if strings.Contains(text, "elsewhere") || !strings.Contains(text, "No new events.") {
		t.Fatalf("filtered poll:\n%s", text)
	}

	raw, _ := h.call(pollEventsTool(), map[string]any{"cursor": cursor, "format": "json"})
	var page struct {
		Events     []map[string]any `json:"events"`
		NextCursor string           `json:"next_cursor"`
		Gap        bool             `json:"gap"`
	}
	if err := json.Unmarshal([]byte(raw), &page); err != nil || len(page.Events) != 1 || page.NextCursor == "" {
		t.Fatalf("json page = %s (%v)", raw, err)
	}
}

func TestPollEventsReportsGapAndBadCursor(t *testing.T) {
	h := newHarness(t, auth.CapBotEvents)
	appendMsg(h, "5", "7", "hi")
	text, isErr := h.call(pollEventsTool(), map[string]any{"cursor": "oldboot:3"})
	if isErr || !strings.Contains(text, "gap: some events were missed") {
		t.Fatalf("text:\n%s", text)
	}
	if _, isErr := h.call(pollEventsTool(), map[string]any{"cursor": "garbage"}); !isErr || h.lastAudit()["outcome"] != "invalid_args" {
		t.Fatalf("bad cursor: isErr=%v audit=%v", isErr, h.lastAudit())
	}
}

func TestWaitForMessageIgnoresSelfByDefault(t *testing.T) {
	h := newHarness(t, auth.CapBotEvents)
	go func() {
		time.Sleep(30 * time.Millisecond)
		appendMsg(h, "5", hBotUserID, "from the bot")
		appendMsg(h, "5", "7", "from a user")
	}()
	text, isErr := h.call(waitForMessageTool(), map[string]any{"channel_id": "5", "timeout_seconds": float64(5)})
	if isErr || !strings.Contains(text, "from a user") || strings.Contains(text, "from the bot") || !strings.Contains(text, "next_cursor: ") {
		t.Fatalf("text:\n%s", text)
	}
}

func TestWaitForMessageIncludeSelf(t *testing.T) {
	h := newHarness(t, auth.CapBotEvents)
	cursor := h.p.Events.Cursor()
	appendMsg(h, "5", hBotUserID, "from the bot")
	text, _ := h.call(waitForMessageTool(), map[string]any{"cursor": cursor, "include_self": true, "timeout_seconds": float64(1)})
	if !strings.Contains(text, "from the bot") {
		t.Fatalf("text:\n%s", text)
	}
}

func TestWaitForMessageTimeoutIsNotAnError(t *testing.T) {
	h := newHarness(t, auth.CapBotEvents)
	text, isErr := h.call(waitForMessageTool(), map[string]any{"channel_id": "5", "timeout_seconds": float64(1)})
	if isErr || !strings.HasPrefix(text, "No matching message within 1s.") {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
	if h.lastAudit()["outcome"] != "ok" {
		t.Fatalf("audit = %v", h.lastAudit())
	}
	raw, _ := h.call(waitForMessageTool(), map[string]any{"timeout_seconds": float64(1), "format": "json"})
	if !strings.Contains(raw, `"message":null`) || !strings.Contains(raw, `"next_cursor"`) {
		t.Fatalf("json = %s", raw)
	}
}

func TestWaitForMessageTimeoutBounds(t *testing.T) {
	h := newHarness(t, auth.CapBotEvents)
	if _, isErr := h.call(waitForMessageTool(), map[string]any{"timeout_seconds": float64(301)}); !isErr {
		t.Fatal("timeout above 300s must fail")
	}
}

func TestEventToolsWithoutBuffer(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	if text, isErr := h.call(pollEventsTool(), nil); !isErr || !strings.Contains(text, "not enabled") {
		t.Fatalf("text=%q", text)
	}
}
