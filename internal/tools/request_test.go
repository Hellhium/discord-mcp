package tools

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
)

func TestRequestGetWithQuery(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{g}/audit-logs", discordtest.JSON(200, map[string]any{"audit_log_entries": []any{}}))
	text, isErr := h.call(requestTool(), map[string]any{
		"method": "GET", "route": "/guilds/1/audit-logs",
		"query": map[string]any{"limit": float64(5), "action_type": "22", "with": []any{"a", "b"}, "flag": true},
	})
	if isErr {
		t.Fatal(text)
	}
	var out struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil || out.Status != 200 || !strings.Contains(string(out.Body), "audit_log_entries") {
		t.Fatalf("text = %s", text)
	}
	q := h.last().Query
	if q.Get("limit") != "5" || q.Get("action_type") != "22" || strings.Join(q["with"], ",") != "a,b" || q.Get("flag") != "true" {
		t.Fatalf("query = %v", q)
	}
}

func TestRequestPostBodyAndReason(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/guilds/{g}/emojis", discordtest.JSON(201, map[string]any{"id": "3"}))
	text, isErr := h.call(requestTool(), map[string]any{
		"method": "POST", "route": "/guilds/1/emojis", "body": map[string]any{"name": "party"}, "reason": "new emoji",
	})
	if isErr || !strings.HasPrefix(text, `{"status":201`) {
		t.Fatalf("text=%s", text)
	}
	if h.lastBody()["name"] != "party" || h.last().Header.Get("X-Audit-Log-Reason") != "new%20emoji" {
		t.Fatalf("body=%v reason=%q", h.lastBody(), h.last().Header.Get("X-Audit-Log-Reason"))
	}
}

func TestRequestRejectedBeforeSending(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"host in route":   {"method": "GET", "route": "//evil.example/x"},
		"dot segments":    {"method": "GET", "route": "/channels/%2e%2e/oauth2"},
		"bad method":      {"method": "TRACE", "route": "/users/@me"},
		"missing route":   {"method": "GET"},
		"body on GET":     {"method": "GET", "route": "/users/@me", "body": map[string]any{"a": 1.0}},
		"object in query": {"method": "GET", "route": "/users/@me", "query": map[string]any{"x": map[string]any{}}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, auth.CapBot)
			if _, isErr := h.call(requestTool(), args); !isErr || h.requests() != 0 {
				t.Fatalf("isErr=%v requests=%d", isErr, h.requests())
			}
			if h.lastAudit()["outcome"] != "invalid_args" {
				t.Fatalf("audit = %v", h.lastAudit())
			}
		})
	}
}

func TestRequestTruncatesLargeResponse(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	big := strings.Repeat("x", maxRawResponse+10)
	h.handle("GET /api/v10/users/@me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"blob":"` + big + `"}`))
	})
	text, isErr := h.call(requestTool(), map[string]any{"method": "GET", "route": "/users/@me"})
	if isErr {
		t.Fatal("request failed")
	}
	var out struct {
		Status     int    `json:"status"`
		Truncated  bool   `json:"truncated"`
		Bytes      int    `json:"bytes"`
		BodyPrefix string `json:"body_prefix"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil || !out.Truncated || out.Bytes != len(big)+11 || len(out.BodyPrefix) != maxRawResponse {
		t.Fatalf("truncated=%v bytes=%d prefix=%d err=%v", out.Truncated, out.Bytes, len(out.BodyPrefix), err)
	}
}

func TestRequestErrorIncludesDiscordDetails(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/guilds/{g}/roles", discordtest.JSON(400, map[string]any{
		"message": "Invalid Form Body", "code": 50035,
		"errors": map[string]any{"name": map[string]any{"_errors": []any{map[string]any{"message": "Must be 100 or fewer in length."}}}},
	}))
	text, isErr := h.call(requestTool(), map[string]any{"method": "POST", "route": "/guilds/1/roles", "body": map[string]any{"name": "x"}})
	if !isErr || !strings.Contains(text, "Discord 400: Invalid Form Body (50035)") || !strings.Contains(text, "Must be 100 or fewer") {
		t.Fatalf("text = %s", text)
	}
	if h.lastAudit()["outcome"] != "discord_error" {
		t.Fatalf("audit = %v", h.lastAudit())
	}
}

func TestRequestAuditRedactsWebhookToken(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/webhooks/{id}/{token}", discordtest.JSON(200, map[string]any{"id": "1"}))
	h.call(requestTool(), map[string]any{"method": "GET", "route": "/webhooks/1/SECRETWEBHOOKTOKEN"})
	line := h.logs.String()
	if strings.Contains(line, "SECRETWEBHOOKTOKEN") {
		t.Fatalf("audit leaks webhook token: %s", line)
	}
}
