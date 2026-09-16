package tools

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
)

func TestCreateThreadFromMessage(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/channels/{c}/messages/{m}/threads", discordtest.JSON(201, map[string]any{"id": "60", "type": 11, "name": "bug", "parent_id": "5"}))
	text, isErr := h.call(createThreadTool(), map[string]any{"channel_id": "5", "message_id": "900", "name": "bug", "reason": "triage"})
	if isErr || text != "Created #bug (60) public-thread parent=5" {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
	if !reflect.DeepEqual(h.lastBody(), map[string]any{"name": "bug", "auto_archive_duration": float64(1440)}) {
		t.Fatalf("body = %v", h.lastBody())
	}
	if h.last().Header.Get("X-Audit-Log-Reason") != "triage" {
		t.Fatal("reason not sent")
	}
}

func TestCreateStandalonePrivateThread(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/channels/{c}/threads", discordtest.JSON(201, map[string]any{"id": "61", "type": 12, "name": "secret", "parent_id": "5"}))
	_, isErr := h.call(createThreadTool(), map[string]any{"channel_id": "5", "name": "secret", "private": true, "auto_archive_minutes": float64(60)})
	if isErr {
		t.Fatal("create failed")
	}
	if !reflect.DeepEqual(h.lastBody(), map[string]any{"name": "secret", "auto_archive_duration": float64(60), "type": float64(12)}) {
		t.Fatalf("body = %v", h.lastBody())
	}
}

func TestCreateThreadValidation(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	for name, args := range map[string]map[string]any{
		"bad archive":        {"channel_id": "5", "name": "x", "auto_archive_minutes": float64(30)},
		"private on message": {"channel_id": "5", "message_id": "1", "name": "x", "private": true},
		"missing name":       {"channel_id": "5"},
	} {
		if _, isErr := h.call(createThreadTool(), args); !isErr {
			t.Errorf("%s: want error", name)
		}
	}
	if h.requests() != 0 {
		t.Fatalf("%d request(s) sent", h.requests())
	}
}

func TestListActiveThreads(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{g}/threads/active", discordtest.JSON(200, map[string]any{
		"threads": []map[string]any{{"id": "60", "type": 11, "name": "bug", "parent_id": "5"}},
		"members": []any{},
	}))
	text, isErr := h.call(listActiveThreadsTool(), map[string]any{"guild_id": "1"})
	if isErr || text != "#bug (60) public-thread parent=5" {
		t.Fatalf("text=%q", text)
	}
}

func TestCreateChannel(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/guilds/{g}/channels", discordtest.JSON(201, map[string]any{"id": "70", "type": 2, "name": "Lounge", "parent_id": "10"}))
	text, isErr := h.call(createChannelTool(), map[string]any{"guild_id": "1", "name": "Lounge", "type": "voice", "parent_id": "10"})
	if isErr || !strings.HasPrefix(text, "Created #Lounge (70) voice") {
		t.Fatalf("text=%q", text)
	}
	if !reflect.DeepEqual(h.lastBody(), map[string]any{"name": "Lounge", "type": float64(2), "parent_id": "10"}) {
		t.Fatalf("body = %v", h.lastBody())
	}
}

func TestEditChannel(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("PATCH /api/v10/channels/{c}", discordtest.JSON(200, map[string]any{"id": "60", "type": 11, "name": "bug", "thread_metadata": map[string]any{"archived": true, "locked": true}}))
	text, isErr := h.call(editChannelTool(), map[string]any{"channel_id": "60", "archived": true, "locked": true, "topic": ""})
	if isErr || text != "Updated #bug (60) public-thread archived locked" {
		t.Fatalf("text=%q", text)
	}
	if !reflect.DeepEqual(h.lastBody(), map[string]any{"archived": true, "locked": true, "topic": ""}) {
		t.Fatalf("body = %v", h.lastBody())
	}
	if _, isErr := h.call(editChannelTool(), map[string]any{"channel_id": "60"}); !isErr {
		t.Fatal("edit with no fields must fail")
	}
}

func TestDeleteChannel(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("DELETE /api/v10/channels/{c}", discordtest.JSON(200, map[string]any{"id": "70", "type": 0, "name": "old"}))
	text, isErr := h.call(deleteChannelTool(), map[string]any{"channel_id": "70", "reason": "cleanup"})
	if isErr || text != "Deleted #old (70) text" {
		t.Fatalf("text=%q", text)
	}
}
