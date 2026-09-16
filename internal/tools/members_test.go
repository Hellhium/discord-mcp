package tools

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
)

var member = map[string]any{
	"user": map[string]any{"id": "7", "username": "alice"}, "nick": "Al", "roles": []string{"1", "2"},
	"joined_at": "2026-01-02T03:04:05Z",
}

func TestListAndSearchMembers(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{g}/members", discordtest.JSON(200, []any{member}))
	h.handle("GET /api/v10/guilds/{g}/members/search", discordtest.JSON(200, []any{member}))

	text, isErr := h.call(listMembersTool(), map[string]any{"guild_id": "1", "after": "5", "limit": float64(10)})
	if isErr || text != "alice (7) nick=Al roles=1,2 joined 2026-01-02" {
		t.Fatalf("text=%q", text)
	}
	if q := h.last().Query; q.Get("after") != "5" || q.Get("limit") != "10" {
		t.Fatalf("query = %v", q)
	}
	if _, isErr := h.call(searchMembersTool(), map[string]any{"guild_id": "1", "query": "ali"}); isErr {
		t.Fatal("search failed")
	}
	if q := h.last().Query; q.Get("query") != "ali" || q.Get("limit") != "25" {
		t.Fatalf("query = %v", q)
	}
	if _, isErr := h.call(searchMembersTool(), map[string]any{"guild_id": "1"}); !isErr {
		t.Fatal("query is required")
	}
}

func TestGetMemberTimedOut(t *testing.T) {
	timeNow = func() time.Time { return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { timeNow = time.Now })
	h := newHarness(t, auth.CapBot)
	m := map[string]any{"user": map[string]any{"id": "7", "username": "alice"}, "roles": []string{},
		"joined_at": "2026-01-02T03:04:05Z", "communication_disabled_until": "2026-09-15T00:00:00Z"}
	h.handle("GET /api/v10/guilds/{g}/members/{u}", discordtest.JSON(200, m))
	text, isErr := h.call(getMemberTool(), map[string]any{"guild_id": "1", "user_id": "7"})
	if isErr || text != "alice (7) joined 2026-01-02 timed out until 2026-09-15 00:00" {
		t.Fatalf("text=%q", text)
	}
}

func TestListRoles(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{g}/roles", discordtest.JSON(200, []map[string]any{
		{"id": "1", "name": "@everyone", "position": 0, "permissions": "1024"},
		{"id": "2", "name": "Admin", "position": 3, "permissions": "8", "color": 16711680, "managed": false},
		{"id": "3", "name": "Bot", "position": 2, "permissions": "0", "managed": true},
	}))
	text, isErr := h.call(listRolesTool(), map[string]any{"guild_id": "1"})
	want := "Admin (2) position=3 permissions=8 color=#ff0000\n" +
		"Bot (3) position=2 permissions=0 [managed]\n" +
		"@everyone (1) position=0 permissions=1024"
	if isErr || text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", text, want)
	}
}

func TestRoleAssignment(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("PUT /api/v10/guilds/{g}/members/{u}/roles/{r}", discordtest.JSON(204, nil))
	h.handle("DELETE /api/v10/guilds/{g}/members/{u}/roles/{r}", discordtest.JSON(204, nil))
	args := map[string]any{"guild_id": "1", "user_id": "7", "role_id": "2", "reason": "promo"}
	if text, _ := h.call(addRoleTool(), args); text != "Added role 2 to user 7." || h.last().Path != "/api/v10/guilds/1/members/7/roles/2" {
		t.Fatalf("add: %q %s", text, h.last().Path)
	}
	if text, _ := h.call(removeRoleTool(), args); text != "Removed role 2 from user 7." || h.last().Method != "DELETE" {
		t.Fatalf("remove: %q", text)
	}
}

func TestTimeoutMember(t *testing.T) {
	timeNow = func() time.Time { return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { timeNow = time.Now })
	h := newHarness(t, auth.CapBot)
	h.handle("PATCH /api/v10/guilds/{g}/members/{u}", discordtest.JSON(200, member))

	text, isErr := h.call(timeoutMemberTool(), map[string]any{"guild_id": "1", "user_id": "7", "minutes": float64(90)})
	if isErr || text != "Timed out user 7 until 2026-09-14 13:30 UTC." {
		t.Fatalf("text=%q", text)
	}
	if !reflect.DeepEqual(h.lastBody(), map[string]any{"communication_disabled_until": "2026-09-14T13:30:00Z"}) {
		t.Fatalf("body = %v", h.lastBody())
	}
	text, _ = h.call(timeoutMemberTool(), map[string]any{"guild_id": "1", "user_id": "7", "minutes": float64(0)})
	if text != "Cleared the timeout of user 7." || !strings.Contains(string(h.last().Body), `"communication_disabled_until":null`) {
		t.Fatalf("clear: %q %s", text, h.last().Body)
	}
	if _, isErr := h.call(timeoutMemberTool(), map[string]any{"guild_id": "1", "user_id": "7", "minutes": float64(40321)}); !isErr {
		t.Fatal("more than 28 days must fail")
	}
}

func TestKickBanUnban(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("DELETE /api/v10/guilds/{g}/members/{u}", discordtest.JSON(204, nil))
	h.handle("PUT /api/v10/guilds/{g}/bans/{u}", discordtest.JSON(204, nil))
	h.handle("DELETE /api/v10/guilds/{g}/bans/{u}", discordtest.JSON(204, nil))

	if text, _ := h.call(kickMemberTool(), map[string]any{"guild_id": "1", "user_id": "7", "reason": "rules"}); text != "Kicked user 7." {
		t.Fatalf("kick: %q", text)
	}
	if h.last().Header.Get("X-Audit-Log-Reason") != "rules" {
		t.Fatal("kick reason not sent")
	}
	if text, _ := h.call(banMemberTool(), map[string]any{"guild_id": "1", "user_id": "7", "delete_message_hours": float64(24)}); text != "Banned user 7." {
		t.Fatalf("ban: %q", text)
	}
	if !reflect.DeepEqual(h.lastBody(), map[string]any{"delete_message_seconds": float64(86400)}) {
		t.Fatalf("ban body = %v", h.lastBody())
	}
	if text, _ := h.call(unbanMemberTool(), map[string]any{"guild_id": "1", "user_id": "7"}); text != "Unbanned user 7." {
		t.Fatalf("unban: %q", text)
	}
}

func TestModerationToolsAreDestructive(t *testing.T) {
	for _, tool := range []Tool{kickMemberTool(), banMemberTool(), timeoutMemberTool(), removeRoleTool()} {
		if a := tool.Def.Annotations; a.DestructiveHint == nil || !*a.DestructiveHint {
			t.Errorf("%s must be annotated destructive", tool.Def.Name)
		}
	}
	for _, tool := range []Tool{listMembersTool(), listRolesTool()} {
		if a := tool.Def.Annotations; a.ReadOnlyHint == nil || !*a.ReadOnlyHint {
			t.Errorf("%s must be annotated read-only", tool.Def.Name)
		}
	}
}
