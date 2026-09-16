package tools

import (
	"strings"
	"testing"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
)

func TestGetMe(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/users/@me", discordtest.JSON(200, map[string]any{"id": "42", "username": "helper", "bot": true}))
	text, isErr := h.call(getMeTool(), nil)
	if isErr || text != "helper [bot] (42)" {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
	raw, _ := h.call(getMeTool(), map[string]any{"format": "json"})
	if !strings.Contains(raw, `"username":"helper"`) {
		t.Fatalf("json = %s", raw)
	}
}

func TestListGuilds(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/users/@me/guilds", discordtest.JSON(200, []map[string]any{
		{"id": "1", "name": "Home", "owner": true, "approximate_member_count": 12},
		{"id": "2", "name": "Work", "approximate_member_count": 300},
	}))
	text, isErr := h.call(listGuildsTool(), map[string]any{"after": "10", "limit": float64(50)})
	if isErr {
		t.Fatal(text)
	}
	if want := "Home (1) members~12 [owner]\nWork (2) members~300"; text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", text, want)
	}
	q := h.last().Query
	if q.Get("after") != "10" || q.Get("limit") != "50" || q.Get("with_counts") != "true" {
		t.Fatalf("query = %v", q)
	}
}

func TestGetGuild(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{id}", discordtest.JSON(200, map[string]any{
		"id": "1", "name": "Home", "owner_id": "7", "description": "cozy",
		"approximate_member_count": 12, "approximate_presence_count": 3, "premium_tier": 1,
	}))
	text, isErr := h.call(getGuildTool(), map[string]any{"guild_id": "1"})
	if isErr {
		t.Fatal(text)
	}
	for _, want := range []string{"Home (1)", "owner: 7", "members: ~12 (~3 online)", "boost tier: 1", "cozy"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if h.last().Query.Get("with_counts") != "true" {
		t.Fatal("with_counts not requested")
	}
}

func TestGetGuildRejectsBadIDWithoutCalling(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	text, isErr := h.call(getGuildTool(), map[string]any{"guild_id": 1.0})
	if !isErr || !strings.Contains(text, "as a string") || h.requests() != 0 {
		t.Fatalf("text=%q isErr=%v requests=%d", text, isErr, h.requests())
	}
	if h.lastAudit()["outcome"] != "invalid_args" {
		t.Fatalf("audit = %v", h.lastAudit())
	}
}

func TestListChannelsGroupsByCategory(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{id}/channels", discordtest.JSON(200, []map[string]any{
		{"id": "20", "type": 0, "name": "general", "parent_id": "10", "position": 1},
		{"id": "10", "type": 4, "name": "Text", "position": 0},
		{"id": "30", "type": 0, "name": "rules", "position": 0},
		{"id": "21", "type": 2, "name": "Lounge", "parent_id": "10", "position": 0, "topic": ""},
	}))
	text, isErr := h.call(listChannelsTool(), map[string]any{"guild_id": "1"})
	if isErr {
		t.Fatal(text)
	}
	want := "#rules (30) text\n" +
		"Text (10) category\n" +
		"  #Lounge (21) voice parent=10\n" +
		"  #general (20) text parent=10"
	if text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", text, want)
	}
}

func TestGetChannel(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/channels/{id}", discordtest.JSON(200, map[string]any{"id": "5", "type": 11, "name": "bug", "parent_id": "4",
		"thread_metadata": map[string]any{"archived": true}}))
	text, isErr := h.call(getChannelTool(), map[string]any{"channel_id": "5"})
	if isErr || text != "#bug (5) public-thread parent=4 archived" {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
}
