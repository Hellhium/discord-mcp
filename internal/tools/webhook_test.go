package tools

import (
	"strings"
	"testing"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
)

const hookPath = "/api/v10/webhooks/" + hWebhookID + "/" + hWebhookToken

func TestWebhookGet(t *testing.T) {
	h := newHarness(t, auth.CapWebhook)
	h.handle("GET /api/v10/webhooks/{id}/{token}", discordtest.JSON(200, map[string]any{"id": hWebhookID, "name": "alerts", "channel_id": "5", "guild_id": "9"}))
	text, isErr := h.call(webhookGetTool(), nil)
	if isErr || text != "Webhook alerts (555) posts to channel 5 in guild 9." {
		t.Fatalf("text=%q", text)
	}
	if h.last().Path != hookPath || h.last().Header.Get("Authorization") != "" {
		t.Fatalf("path=%s auth=%q", h.last().Path, h.last().Header.Get("Authorization"))
	}
}

func TestWebhookGetJSONHidesToken(t *testing.T) {
	h := newHarness(t, auth.CapWebhook)
	h.handle("GET /api/v10/webhooks/{id}/{token}", discordtest.JSON(200, map[string]any{
		"id": hWebhookID, "name": "alerts", "token": hWebhookToken, "url": "https://discord.com/api/webhooks/555/" + hWebhookToken,
	}))
	text, isErr := h.call(webhookGetTool(), map[string]any{"format": "json"})
	if isErr || strings.Contains(text, hWebhookToken) || !strings.Contains(text, `"name":"alerts"`) {
		t.Fatalf("text = %s", text)
	}
}

func TestWebhookSend(t *testing.T) {
	h := newHarness(t, auth.CapWebhook)
	h.handle("POST /api/v10/webhooks/{id}/{token}", discordtest.JSON(200, map[string]any{"id": "900", "channel_id": "5"}))
	text, isErr := h.call(webhookSendTool(), map[string]any{
		"content": "deploy done", "username": "CI", "avatar_url": "https://example.com/a.png", "thread_id": "66", "allow_mentions": false,
	})
	if isErr || text != "Sent message #900 in channel 5." {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
	req := h.last()
	if req.Query.Get("wait") != "true" || req.Query.Get("thread_id") != "66" {
		t.Fatalf("query = %v", req.Query)
	}
	body := h.lastBody()
	if body["username"] != "CI" || body["avatar_url"] != "https://example.com/a.png" || body["content"] != "deploy done" || body["allowed_mentions"] == nil {
		t.Fatalf("body = %v", body)
	}
	logs := h.logs.String()
	if strings.Contains(logs, hWebhookToken) {
		t.Fatalf("audit leaks the webhook token: %s", logs)
	}
	if calls := h.lastAudit()["discord"].([]any); calls[0].(map[string]any)["route"] != "/webhooks/{webhook_id}/{webhook_token}" {
		t.Fatalf("audit route = %v", calls)
	}
	if p := h.lastAudit()["principal"].(map[string]any); p["kind"] != "instance" {
		t.Fatalf("principal = %v", p)
	}
}

func TestWebhookSendNeedsBody(t *testing.T) {
	h := newHarness(t, auth.CapWebhook)
	if _, isErr := h.call(webhookSendTool(), map[string]any{"username": "CI"}); !isErr || h.requests() != 0 {
		t.Fatalf("isErr=%v requests=%d", isErr, h.requests())
	}
}

func TestWebhookMessageLifecycle(t *testing.T) {
	h := newHarness(t, auth.CapWebhook)
	msg := map[string]any{"id": "900", "channel_id": "5", "content": "v1", "author": map[string]any{"id": hWebhookID, "username": "CI", "bot": true}, "timestamp": "2026-09-14T11:02:00Z"}
	h.handle("GET /api/v10/webhooks/{id}/{token}/messages/{m}", discordtest.JSON(200, msg))
	h.handle("PATCH /api/v10/webhooks/{id}/{token}/messages/{m}", discordtest.JSON(200, msg))
	h.handle("DELETE /api/v10/webhooks/{id}/{token}/messages/{m}", discordtest.JSON(204, nil))

	text, isErr := h.call(webhookGetMessageTool(), map[string]any{"message_id": "900", "thread_id": "66"})
	if isErr || text != "[2026-09-14 11:02] CI [bot] (555) #900: v1" || h.last().Query.Get("thread_id") != "66" {
		t.Fatalf("get: %q", text)
	}
	if text, isErr = h.call(webhookEditMessageTool(), map[string]any{"message_id": "900", "content": "v2"}); isErr || text != "Edited message #900." || h.lastBody()["content"] != "v2" {
		t.Fatalf("edit: %q", text)
	}
	if text, isErr = h.call(webhookDeleteMessageTool(), map[string]any{"message_id": "900"}); isErr || text != "Deleted message #900." || h.last().Path != hookPath+"/messages/900" {
		t.Fatalf("delete: %q %s", text, h.last().Path)
	}
}
