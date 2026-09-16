package tools

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

// timeNow is replaced in tests.
var timeNow = time.Now

const maxTimeoutMinutes = 28 * 24 * 60

func memberTools() []Tool {
	return []Tool{
		listMembersTool(), getMemberTool(), searchMembersTool(), listRolesTool(), addRoleTool(), removeRoleTool(),
		timeoutMemberTool(), kickMemberTool(), banMemberTool(), unbanMemberTool(),
	}
}

type memberJSON struct {
	User                       userJSON   `json:"user"`
	Nick                       string     `json:"nick"`
	Roles                      []string   `json:"roles"`
	JoinedAt                   time.Time  `json:"joined_at"`
	CommunicationDisabledUntil *time.Time `json:"communication_disabled_until"`
}

func renderMember(m memberJSON) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s (%s)", displayName(m.User), m.User.ID)
	if m.Nick != "" {
		fmt.Fprintf(&sb, " nick=%s", m.Nick)
	}
	if len(m.Roles) > 0 {
		fmt.Fprintf(&sb, " roles=%s", strings.Join(m.Roles, ","))
	}
	fmt.Fprintf(&sb, " joined %s", m.JoinedAt.UTC().Format("2006-01-02"))
	if u := m.CommunicationDisabledUntil; u != nil && u.After(timeNow()) {
		fmt.Fprintf(&sb, " timed out until %s", u.UTC().Format(timeLayout))
	}
	return sb.String()
}

func renderMembers(ms []memberJSON) string {
	if len(ms) == 0 {
		return "No members."
	}
	lines := make([]string, len(ms))
	for i, m := range ms {
		lines[i] = renderMember(m)
	}
	return strings.Join(lines, "\n")
}

func guildParam(a Args) (map[string]string, error) {
	g, err := a.ID("guild_id")
	if err != nil {
		return nil, err
	}
	return map[string]string{"guild_id": g}, nil
}

func guildUserParams(a Args) (map[string]string, error) {
	params, err := guildParam(a)
	if err != nil {
		return nil, err
	}
	u, err := a.ID("user_id")
	if err != nil {
		return nil, err
	}
	params["user_id"] = u
	return params, nil
}

func listMembersTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_list_members",
			mcp.WithDescription("List guild members in user ID order. Requires the Server Members intent enabled in the Developer Portal."),
			readOnly(), idParam("guild_id", "Guild"),
			mcp.WithNumber("limit", mcp.Description("Members, 1-1000 (default 100)")),
			optIDParam("after", "Only members with a user ID after this one, for paging"),
			withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			params, err := guildParam(a)
			if err != nil {
				return "", err
			}
			limit, err := a.Int("limit", 100, 1, 1000)
			if err != nil {
				return "", err
			}
			after, err := a.OptionalID("after")
			if err != nil {
				return "", err
			}
			q := url.Values{"limit": {strconv.Itoa(limit)}}
			if after != "" {
				q.Set("after", after)
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/guilds/{guild_id}/members", Params: params, Query: q})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderMembers)
		},
	}
}

func getMemberTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_get_member",
			mcp.WithDescription("Show one guild member: nickname, roles, join date, timeout."),
			readOnly(), idParam("guild_id", "Guild"), idParam("user_id", "User"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			params, err := guildUserParams(a)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/guilds/{guild_id}/members/{user_id}", Params: params})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderMember)
		},
	}
}

func searchMembersTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_search_members",
			mcp.WithDescription("Find guild members whose username or nickname starts with query."),
			readOnly(), idParam("guild_id", "Guild"),
			mcp.WithString("query", mcp.Required(), mcp.Description("Name prefix")),
			mcp.WithNumber("limit", mcp.Description("Members, 1-1000 (default 25)")),
			withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			params, err := guildParam(a)
			if err != nil {
				return "", err
			}
			query, err := a.RequiredString("query")
			if err != nil {
				return "", err
			}
			limit, err := a.Int("limit", 25, 1, 1000)
			if err != nil {
				return "", err
			}
			q := url.Values{"query": {query}, "limit": {strconv.Itoa(limit)}}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/guilds/{guild_id}/members/search", Params: params, Query: q})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderMembers)
		},
	}
}

type roleJSON struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       int    `json:"color"`
	Position    int    `json:"position"`
	Permissions string `json:"permissions"`
	Managed     bool   `json:"managed"`
}

func listRolesTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_list_roles",
			mcp.WithDescription("List a guild's roles, highest first, with their permission bitsets."),
			readOnly(), idParam("guild_id", "Guild"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			params, err := guildParam(a)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/guilds/{guild_id}/roles", Params: params})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(rs []roleJSON) string {
				slices.SortFunc(rs, func(a, b roleJSON) int { return cmp.Compare(b.Position, a.Position) })
				lines := make([]string, len(rs))
				for i, r := range rs {
					lines[i] = fmt.Sprintf("%s (%s) position=%d permissions=%s", r.Name, r.ID, r.Position, r.Permissions)
					if r.Color != 0 {
						lines[i] += fmt.Sprintf(" color=#%06x", r.Color)
					}
					if r.Managed {
						lines[i] += " [managed]"
					}
				}
				return strings.Join(lines, "\n")
			})
		},
	}
}

func roleTool(name, desc, method, verb, prep string, ann mcp.ToolOption) Tool {
	return Tool{
		Def: mcp.NewTool(name, mcp.WithDescription(desc), ann,
			idParam("guild_id", "Guild"), idParam("user_id", "Member"), idParam("role_id", "Role"), withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			params, err := guildUserParams(a)
			if err != nil {
				return "", err
			}
			role, err := a.ID("role_id")
			if err != nil {
				return "", err
			}
			params["role_id"] = role
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			if _, err := p.Client.Do(ctx, discord.Call{Method: method, Route: "/guilds/{guild_id}/members/{user_id}/roles/{role_id}", Params: params, Reason: reason}); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s role %s %s user %s.", verb, role, prep, params["user_id"]), nil
		},
	}
}

func addRoleTool() Tool {
	return roleTool("discord_add_role", "Give a member a role.", "PUT", "Added", "to", mutating())
}

func removeRoleTool() Tool {
	return roleTool("discord_remove_role", "Take a role from a member.", "DELETE", "Removed", "from", destructive())
}

func timeoutMemberTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_timeout_member",
			mcp.WithDescription("Time a member out for a number of minutes (max 40320 = 28 days); 0 clears an existing timeout."),
			destructive(), idParam("guild_id", "Guild"), idParam("user_id", "Member"),
			mcp.WithNumber("minutes", mcp.Required(), mcp.Description("Duration in minutes, 0-40320")),
			withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			params, err := guildUserParams(a)
			if err != nil {
				return "", err
			}
			if _, ok := a.present("minutes"); !ok {
				return "", argErr("minutes is required")
			}
			minutes, err := a.Int("minutes", 0, 0, maxTimeoutMinutes)
			if err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			var until *string
			var untilTime time.Time
			if minutes > 0 {
				untilTime = timeNow().UTC().Add(time.Duration(minutes) * time.Minute)
				s := untilTime.Format(time.RFC3339)
				until = &s
			}
			body := map[string]any{"communication_disabled_until": until}
			if _, err := p.Client.Do(ctx, discord.Call{Method: "PATCH", Route: "/guilds/{guild_id}/members/{user_id}", Params: params, Body: body, Reason: reason}); err != nil {
				return "", err
			}
			if minutes == 0 {
				return fmt.Sprintf("Cleared the timeout of user %s.", params["user_id"]), nil
			}
			return fmt.Sprintf("Timed out user %s until %s UTC.", params["user_id"], untilTime.Format(timeLayout)), nil
		},
	}
}

func kickMemberTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_kick_member",
			mcp.WithDescription("Remove a member from the guild. They can rejoin with an invite."),
			destructive(), idParam("guild_id", "Guild"), idParam("user_id", "Member"), withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			params, err := guildUserParams(a)
			if err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			if _, err := p.Client.Do(ctx, discord.Call{Method: "DELETE", Route: "/guilds/{guild_id}/members/{user_id}", Params: params, Reason: reason}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Kicked user %s.", params["user_id"]), nil
		},
	}
}

func banMemberTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_ban_member",
			mcp.WithDescription("Ban a user from the guild, optionally deleting their recent messages."),
			destructive(), idParam("guild_id", "Guild"), idParam("user_id", "User"),
			mcp.WithNumber("delete_message_hours", mcp.Description("Delete the user's messages from the last 0-168 hours (default 0)")),
			withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			params, err := guildUserParams(a)
			if err != nil {
				return "", err
			}
			hours, err := a.Int("delete_message_hours", 0, 0, 168)
			if err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			body := map[string]any{"delete_message_seconds": hours * 3600}
			if _, err := p.Client.Do(ctx, discord.Call{Method: "PUT", Route: "/guilds/{guild_id}/bans/{user_id}", Params: params, Body: body, Reason: reason}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Banned user %s.", params["user_id"]), nil
		},
	}
}

func unbanMemberTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_unban_member",
			mcp.WithDescription("Lift a user's ban."),
			mutating(), idParam("guild_id", "Guild"), idParam("user_id", "User"), withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			params, err := guildUserParams(a)
			if err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			if _, err := p.Client.Do(ctx, discord.Call{Method: "DELETE", Route: "/guilds/{guild_id}/bans/{user_id}", Params: params, Reason: reason}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Unbanned user %s.", params["user_id"]), nil
		},
	}
}
