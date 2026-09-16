package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Hellhium/discord-mcp/internal/discord"
)

func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("not a JSON line: %q", line)
		}
		out = append(out, m)
	}
	return out
}

func TestActionLine(t *testing.T) {
	var buf bytes.Buffer
	New(&buf).Action(Action{
		Actor:    Actor{Kind: "instance", Instance: "assistant"},
		Tool:     "discord_send_message",
		Outcome:  OutcomeOK,
		Duration: 142 * time.Millisecond,
		Targets:  map[string]string{"channel_id": "1"},
		Args:     map[string]any{"content": "hello"},
		Calls:    []discord.CallRecord{{Method: "POST", Route: "/channels/{channel_id}/messages", Status: 200}},
	})
	m := decodeLines(t, &buf)[0]
	want := map[string]any{
		"level":       "INFO",
		"msg":         "action",
		"principal":   map[string]any{"kind": "instance", "instance": "assistant"},
		"tool":        "discord_send_message",
		"outcome":     "ok",
		"duration_ms": float64(142),
		"targets":     map[string]any{"channel_id": "1"},
		"args":        map[string]any{"content": "hello"},
		"discord":     []any{map[string]any{"method": "POST", "route": "/channels/{channel_id}/messages", "status": float64(200), "rate_limited_ms": float64(0)}},
	}
	for k, v := range want {
		if !reflect.DeepEqual(m[k], v) {
			t.Errorf("%s = %#v, want %#v", k, m[k], v)
		}
	}
	if _, ok := m["time"]; !ok {
		t.Error("missing time")
	}
	if _, ok := m["error"]; ok {
		t.Error("error must be omitted when empty")
	}
}

func TestActionFailureIsWarnWithError(t *testing.T) {
	var buf bytes.Buffer
	New(&buf).Action(Action{
		Actor:   Actor{Kind: "direct_bot", Credential: "sha256:1a2b3c4d"},
		Tool:    "discord_get_me",
		Outcome: OutcomeDiscordError,
		Error:   "Discord 403: Missing Permissions (50013)",
	})
	m := decodeLines(t, &buf)[0]
	if m["level"] != "WARN" || m["error"] != "Discord 403: Missing Permissions (50013)" {
		t.Fatalf("line = %v", m)
	}
	if p := m["principal"].(map[string]any); p["credential"] != "sha256:1a2b3c4d" || p["instance"] != nil {
		t.Fatalf("principal = %v", p)
	}
	if !reflect.DeepEqual(m["discord"], []any{}) || !reflect.DeepEqual(m["targets"], map[string]any{}) {
		t.Fatalf("empty calls/targets must render as [] and {}: %v %v", m["discord"], m["targets"])
	}
}

func TestAuthRejectedAndGateway(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.AuthRejected("unknown_token", "Authorization")
	l.Gateway("assistant", "gateway_connected", nil)
	l.Gateway("assistant", "gateway_disconnected", errors.New("eof"))
	lines := decodeLines(t, &buf)
	if lines[0]["msg"] != "auth_rejected" || lines[0]["reason"] != "unknown_token" || lines[0]["header"] != "Authorization" || lines[0]["level"] != "WARN" {
		t.Fatalf("auth line = %v", lines[0])
	}
	if lines[1]["msg"] != "gateway_connected" || lines[1]["instance"] != "assistant" || lines[1]["level"] != "INFO" {
		t.Fatalf("gateway line = %v", lines[1])
	}
	if lines[2]["level"] != "WARN" || lines[2]["error"] != "eof" {
		t.Fatalf("gateway error line = %v", lines[2])
	}
}

func TestSanitizeArgs(t *testing.T) {
	data := []byte("hello attachment")
	sum := sha256.Sum256(data)
	big := strings.Repeat("x", MaxBodyBytes+100)
	in := map[string]any{
		"content": "hi",
		"attachments": []any{
			map[string]any{"filename": "a.txt", "content_base64": base64.StdEncoding.EncodeToString(data), "description": "doc"},
			map[string]any{"filename": "b.bin", "content_base64": "!!!not base64"},
		},
		"route": "/webhooks/123/SECRET/messages/9",
		"body":  map[string]any{"text": big},
	}
	out := SanitizeArgs(in)

	if out["content"] != "hi" {
		t.Errorf("content changed: %v", out["content"])
	}
	atts := out["attachments"].([]any)
	a := atts[0].(map[string]any)
	if a["filename"] != "a.txt" || a["size"] != len(data) || a["sha256"] != hex.EncodeToString(sum[:]) || a["description"] != "doc" {
		t.Errorf("attachment = %v", a)
	}
	if _, ok := a["content_base64"]; ok {
		t.Error("attachment content must be removed")
	}
	if b := atts[1].(map[string]any); b["invalid_base64"] != true {
		t.Errorf("invalid attachment = %v", b)
	}
	if out["route"] != "/webhooks/{id}/{token}/messages/{id}" {
		t.Errorf("route = %v", out["route"])
	}
	body := out["body"].(map[string]any)
	if body["truncated"] != true || len(body["prefix"].(string)) > MaxBodyBytes {
		t.Errorf("body not truncated: truncated=%v len=%d", body["truncated"], len(body["prefix"].(string)))
	}
	// The input must not be modified.
	if _, ok := in["attachments"].([]any)[0].(map[string]any)["content_base64"]; !ok {
		t.Error("SanitizeArgs mutated its input")
	}
}

func TestSanitizeSmallBodyKept(t *testing.T) {
	out := SanitizeArgs(map[string]any{"body": map[string]any{"name": "x"}})
	if !reflect.DeepEqual(out["body"], map[string]any{"name": "x"}) {
		t.Fatalf("small body changed: %v", out["body"])
	}
}

func TestTargets(t *testing.T) {
	got := Targets(map[string]any{"channel_id": "1", "message_id": "2", "user_id": 3, "content": "x"})
	want := map[string]string{"channel_id": "1", "message_id": "2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Targets = %v, want %v", got, want)
	}
}
