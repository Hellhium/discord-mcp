package tools

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

func discoveryTools() []Tool {
	return []Tool{getMeTool(), listGuildsTool(), getGuildTool(), listChannelsTool(), getChannelTool()}
}

func getMeTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_get_me",
			mcp.WithDescription("Show the bot user this server acts as."),
			readOnly(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/users/@me"})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(u userJSON) string {
				return fmt.Sprintf("%s (%s)", displayName(u), u.ID)
			})
		},
	}
}

type partialGuildJSON struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Owner                  bool   `json:"owner"`
	ApproximateMemberCount int    `json:"approximate_member_count"`
}

func listGuildsTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_list_guilds",
			mcp.WithDescription("List the servers (guilds) the bot is in."),
			readOnly(),
			optIDParam("after", "Only guilds with an ID after this one, for paging"),
			mcp.WithNumber("limit", mcp.Description("Max guilds, 1-200 (default 200)")),
			withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			after, err := a.OptionalID("after")
			if err != nil {
				return "", err
			}
			limit, err := a.Int("limit", 200, 1, 200)
			if err != nil {
				return "", err
			}
			q := url.Values{"limit": {strconv.Itoa(limit)}, "with_counts": {"true"}}
			if after != "" {
				q.Set("after", after)
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/users/@me/guilds", Query: q})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(gs []partialGuildJSON) string {
				if len(gs) == 0 {
					return "The bot is in no guilds."
				}
				lines := make([]string, len(gs))
				for i, g := range gs {
					lines[i] = fmt.Sprintf("%s (%s) members~%d", g.Name, g.ID, g.ApproximateMemberCount)
					if g.Owner {
						lines[i] += " [owner]"
					}
				}
				return strings.Join(lines, "\n")
			})
		},
	}
}

type guildJSON struct {
	ID                       string   `json:"id"`
	Name                     string   `json:"name"`
	OwnerID                  string   `json:"owner_id"`
	Description              string   `json:"description"`
	ApproximateMemberCount   int      `json:"approximate_member_count"`
	ApproximatePresenceCount int      `json:"approximate_presence_count"`
	PremiumTier              int      `json:"premium_tier"`
	Features                 []string `json:"features"`
}

func getGuildTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_get_guild",
			mcp.WithDescription("Show a guild: name, owner, approximate member counts, boost tier, features."),
			readOnly(), idParam("guild_id", "Guild"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			id, err := a.ID("guild_id")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{
				Method: "GET", Route: "/guilds/{guild_id}",
				Params: map[string]string{"guild_id": id},
				Query:  url.Values{"with_counts": {"true"}},
			})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(g guildJSON) string {
				var sb strings.Builder
				fmt.Fprintf(&sb, "%s (%s)\nowner: %s\nmembers: ~%d (~%d online)\nboost tier: %d",
					g.Name, g.ID, g.OwnerID, g.ApproximateMemberCount, g.ApproximatePresenceCount, g.PremiumTier)
				if g.Description != "" {
					fmt.Fprintf(&sb, "\ndescription: %s", g.Description)
				}
				if len(g.Features) > 0 {
					fmt.Fprintf(&sb, "\nfeatures: %s", strings.Join(g.Features, ", "))
				}
				return sb.String()
			})
		},
	}
}

func listChannelsTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_list_channels",
			mcp.WithDescription("List a guild's channels, grouped under their categories. Active threads are listed by discord_list_active_threads."),
			readOnly(), idParam("guild_id", "Guild"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			id, err := a.ID("guild_id")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/guilds/{guild_id}/channels", Params: map[string]string{"guild_id": id}})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderChannelTree)
		},
	}
}

// renderChannelTree prints uncategorised channels first, then each category
// with its children indented, everything in Discord's position order.
func renderChannelTree(chs []channelJSON) string {
	if len(chs) == 0 {
		return "No channels."
	}
	byPos := func(a, b channelJSON) int {
		if a.Position != b.Position {
			return a.Position - b.Position
		}
		return strings.Compare(a.ID, b.ID)
	}
	children := map[string][]channelJSON{}
	var top, categories []channelJSON
	for _, c := range chs {
		switch {
		case c.Type == 4:
			categories = append(categories, c)
		case c.ParentID != "":
			children[c.ParentID] = append(children[c.ParentID], c)
		default:
			top = append(top, c)
		}
	}
	slices.SortFunc(top, byPos)
	slices.SortFunc(categories, byPos)
	var lines []string
	for _, c := range top {
		lines = append(lines, renderChannel(c))
	}
	for _, cat := range categories {
		lines = append(lines, fmt.Sprintf("%s (%s) category", cat.Name, cat.ID))
		kids := children[cat.ID]
		slices.SortFunc(kids, byPos)
		for _, k := range kids {
			lines = append(lines, "  "+renderChannel(k))
		}
		delete(children, cat.ID)
	}
	// Children whose category was not returned.
	for _, kids := range children {
		for _, k := range kids {
			lines = append(lines, renderChannel(k))
		}
	}
	return strings.Join(lines, "\n")
}

func getChannelTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_get_channel",
			mcp.WithDescription("Show a channel or thread."),
			readOnly(), idParam("channel_id", "Channel or thread"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			id, err := a.ID("channel_id")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/channels/{channel_id}", Params: map[string]string{"channel_id": id}})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderChannel)
		},
	}
}
