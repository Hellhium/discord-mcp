package tools

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
)

var sentMessage = map[string]any{"id": "900", "channel_id": "5", "content": "hi", "author": map[string]any{"id": "42", "username": "helper"}, "timestamp": "2026-09-14T11:02:00Z"}

func TestReadMessages(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/channels/{id}/messages", discordtest.JSON(200, []map[string]any{
		{"id": "2", "content": "later", "author": map[string]any{"id": "8", "username": "bob"}, "timestamp": "2026-09-14T11:03:00Z"},
		{"id": "1", "content": "earlier", "author": map[string]any{"id": "7", "username": "al"}, "timestamp": "2026-09-14T11:02:00Z"},
	}))
	text, isErr := h.call(readMessagesTool(), map[string]any{"channel_id": "5", "around": "1", "limit": float64(10)})
	if isErr {
		t.Fatal(text)
	}
	if !strings.HasPrefix(text, "[2026-09-14 11:02] al (7) #1: earlier\n[2026-09-14 11:03] bob (8) #2: later") {
		t.Fatalf("text:\n%s", text)
	}
	q := h.last().Query
	if q.Get("around") != "1" || q.Get("limit") != "10" || q.Has("before") {
		t.Fatalf("query = %v", q)
	}
}

func TestReadMessagesRejectsTwoAnchors(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	_, isErr := h.call(readMessagesTool(), map[string]any{"channel_id": "5", "before": "1", "after": "2"})
	if !isErr || h.requests() != 0 {
		t.Fatalf("isErr=%v requests=%d", isErr, h.requests())
	}
}

func TestGetMessage(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/channels/{c}/messages/{m}", discordtest.JSON(200, sentMessage))
	text, isErr := h.call(getMessageTool(), map[string]any{"channel_id": "5", "message_id": "900"})
	if isErr || text != "[2026-09-14 11:02] helper (42) #900: hi" {
		t.Fatalf("text=%q", text)
	}
}

func TestSendMessage(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/channels/{id}/messages", discordtest.JSON(200, sentMessage))
	text, isErr := h.call(sendMessageTool(), map[string]any{
		"channel_id": "5", "content": "hi @everyone", "reply_to": "899", "allow_mentions": false,
	})
	if isErr || text != "Sent message #900 in channel 5." {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
	body := h.lastBody()
	if body["content"] != "hi @everyone" {
		t.Fatalf("body = %v", body)
	}
	if !reflect.DeepEqual(body["message_reference"], map[string]any{"message_id": "899", "fail_if_not_exists": false}) {
		t.Fatalf("message_reference = %v", body["message_reference"])
	}
	if !reflect.DeepEqual(body["allowed_mentions"], map[string]any{"parse": []any{}}) {
		t.Fatalf("allowed_mentions = %v", body["allowed_mentions"])
	}
}

func TestSendMessageDefaultsSendNoAllowedMentions(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/channels/{id}/messages", discordtest.JSON(200, sentMessage))
	h.call(sendMessageTool(), map[string]any{"channel_id": "5", "content": "@here"})
	if _, ok := h.lastBody()["allowed_mentions"]; ok {
		t.Fatal("allow_mentions default must not send allowed_mentions")
	}
}

func TestSendMessageWithAttachmentIsMultipart(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/channels/{id}/messages", discordtest.JSON(200, sentMessage))
	_, isErr := h.call(sendMessageTool(), map[string]any{
		"channel_id":  "5",
		"attachments": []any{map[string]any{"filename": "a.txt", "content_base64": "aGVsbG8="}},
	})
	if isErr {
		t.Fatal("send failed")
	}
	if ct := h.last().Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/form-data") {
		t.Fatalf("Content-Type = %s", ct)
	}
	args := h.lastAudit()["args"].(map[string]any)
	att := args["attachments"].([]any)[0].(map[string]any)
	if _, leaked := att["content_base64"]; leaked || att["size"] != float64(5) {
		t.Fatalf("audit attachment = %v", att)
	}
}

func TestSendMessageNeedsBody(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	_, isErr := h.call(sendMessageTool(), map[string]any{"channel_id": "5"})
	if !isErr || h.requests() != 0 {
		t.Fatalf("isErr=%v requests=%d", isErr, h.requests())
	}
}

func TestEditAndDeleteMessage(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("PATCH /api/v10/channels/{c}/messages/{m}", discordtest.JSON(200, sentMessage))
	h.handle("DELETE /api/v10/channels/{c}/messages/{m}", discordtest.JSON(204, nil))

	text, isErr := h.call(editMessageTool(), map[string]any{"channel_id": "5", "message_id": "900", "content": "fixed"})
	if isErr || text != "Edited message #900." || h.lastBody()["content"] != "fixed" {
		t.Fatalf("edit: %q %v %v", text, isErr, h.lastBody())
	}
	text, isErr = h.call(deleteMessageTool(), map[string]any{"channel_id": "5", "message_id": "900", "reason": "spam"})
	if isErr || text != "Deleted message #900." || h.last().Header.Get("X-Audit-Log-Reason") != "spam" {
		t.Fatalf("delete: %q %v", text, isErr)
	}
}

func TestPinMessage(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("PUT /api/v10/channels/{c}/messages/pins/{m}", discordtest.JSON(204, nil))
	h.handle("DELETE /api/v10/channels/{c}/messages/pins/{m}", discordtest.JSON(204, nil))
	if text, _ := h.call(pinMessageTool(), map[string]any{"channel_id": "5", "message_id": "900", "pinned": true}); text != "Pinned message #900." || h.last().Method != "PUT" {
		t.Fatalf("pin: %q %s", text, h.last().Method)
	}
	if text, _ := h.call(pinMessageTool(), map[string]any{"channel_id": "5", "message_id": "900", "pinned": false}); text != "Unpinned message #900." || h.last().Method != "DELETE" {
		t.Fatalf("unpin: %q %s", text, h.last().Method)
	}
	if _, isErr := h.call(pinMessageTool(), map[string]any{"channel_id": "5", "message_id": "900"}); !isErr {
		t.Fatal("pinned is required")
	}
}

func TestReactions(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("PUT /api/v10/channels/{c}/messages/{m}/reactions/{e}/@me", discordtest.JSON(204, nil))
	h.handle("DELETE /api/v10/channels/{c}/messages/{m}/reactions/{e}/{u}", discordtest.JSON(204, nil))

	h.call(addReactionTool(), map[string]any{"channel_id": "5", "message_id": "900", "emoji": "<:party:123>"})
	if raw := h.last().RawPath; !strings.HasSuffix(raw, "/reactions/party:123/@me") {
		t.Fatalf("custom emoji path = %s", raw)
	}
	h.call(removeReactionTool(), map[string]any{"channel_id": "5", "message_id": "900", "emoji": "👍", "user_id": "7"})
	if raw := h.last().RawPath; !strings.HasSuffix(raw, "/reactions/%F0%9F%91%8D/7") || h.last().Method != "DELETE" {
		t.Fatalf("remove path = %s", raw)
	}
	h.call(removeReactionTool(), map[string]any{"channel_id": "5", "message_id": "900", "emoji": "👍"})
	if raw := h.last().RawPath; !strings.HasSuffix(raw, "/@me") {
		t.Fatalf("remove own path = %s", raw)
	}
}

func TestNormalizeEmoji(t *testing.T) {
	for in, want := range map[string]string{
		"<:party:123>":  "party:123",
		"<a:dance:456>": "dance:456",
		"party:123":     "party:123",
		"👍":             "👍",
	} {
		if got := normalizeEmoji(in); got != want {
			t.Errorf("normalizeEmoji(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchMessages(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{g}/messages/search", discordtest.JSON(200, map[string]any{
		"total_results": 1,
		"messages":      []any{[]any{sentMessage}},
	}))
	text, isErr := h.call(searchMessagesTool(), map[string]any{
		"guild_id": "1", "content": "hi", "channel_id": "5", "author_id": "42", "limit": float64(5), "offset": float64(10),
	})
	if isErr || text != "1 result(s), showing 1:\n[2026-09-14 11:02] helper (42) #900: hi" {
		t.Fatalf("text=%q", text)
	}
	q := h.last().Query
	if q.Get("content") != "hi" || q.Get("channel_id") != "5" || q.Get("author_id") != "42" || q.Get("limit") != "5" || q.Get("offset") != "10" {
		t.Fatalf("query = %v", q)
	}
}

func TestSearchMessagesIndexing(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{g}/messages/search", discordtest.JSON(http.StatusAccepted, map[string]any{
		"message": "Index not yet available. Try again later", "code": 110000, "retry_after": 2,
	}))
	text, isErr := h.call(searchMessagesTool(), map[string]any{"guild_id": "1", "content": "hi"})
	if !isErr || !strings.Contains(text, "retry after 2s") {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
}

func TestSendDM(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/users/@me/channels", discordtest.JSON(200, map[string]any{"id": "77", "type": 1}))
	h.handle("POST /api/v10/channels/{id}/messages", discordtest.JSON(200, map[string]any{"id": "901", "channel_id": "77"}))
	text, isErr := h.call(sendDMTool(), map[string]any{"user_id": "7", "content": "hello"})
	if isErr || text != "Sent message #901 in channel 77." {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
	reqs := h.fake.Requests()
	if len(reqs) != 2 || !strings.Contains(string(reqs[0].Body), `"recipient_id":"7"`) || reqs[1].Path != "/api/v10/channels/77/messages" {
		t.Fatalf("requests = %+v", reqs)
	}
	if calls := h.lastAudit()["discord"].([]any); len(calls) != 2 {
		t.Fatalf("audit must list both calls: %v", calls)
	}
}
