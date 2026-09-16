package tools

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

func channelTools() []Tool {
	return []Tool{createThreadTool(), listActiveThreadsTool(), createChannelTool(), editChannelTool(), deleteChannelTool()}
}

var autoArchiveMinutes = map[int]bool{60: true, 1440: true, 4320: true, 10080: true}

func createThreadTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_create_thread",
			mcp.WithDescription("Create a thread from a message (message_id) or a standalone thread in a channel."),
			mutating(),
			idParam("channel_id", "Parent channel"),
			mcp.WithString("name", mcp.Required(), mcp.Description("Thread name (1-100 characters)")),
			optIDParam("message_id", "Start the thread from this message"),
			mcp.WithBoolean("private", mcp.Description("Standalone threads only: create a private thread")),
			mcp.WithNumber("auto_archive_minutes", mcp.Description("60, 1440 (default), 4320 or 10080")),
			withReason(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			channel, err := a.ID("channel_id")
			if err != nil {
				return "", err
			}
			name, err := a.RequiredString("name")
			if err != nil {
				return "", err
			}
			message, err := a.OptionalID("message_id")
			if err != nil {
				return "", err
			}
			private, err := a.Bool("private", false)
			if err != nil {
				return "", err
			}
			archive, err := a.Int("auto_archive_minutes", 1440, 60, 10080)
			if err != nil || !autoArchiveMinutes[archive] {
				return "", argErr("auto_archive_minutes must be 60, 1440, 4320 or 10080")
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			body := map[string]any{"name": name, "auto_archive_duration": archive}
			call := discord.Call{Method: "POST", Body: body, Reason: reason}
			if message != "" {
				if private {
					return "", argErr("private applies to standalone threads only; omit message_id")
				}
				call.Route = "/channels/{channel_id}/messages/{message_id}/threads"
				call.Params = chanMsg(channel, message)
			} else {
				body["type"] = 11
				if private {
					body["type"] = 12
				}
				call.Route = "/channels/{channel_id}/threads"
				call.Params = map[string]string{"channel_id": channel}
			}
			resp, err := p.Client.Do(ctx, call)
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(c channelJSON) string { return "Created " + renderChannel(c) })
		},
	}
}

func listActiveThreadsTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_list_active_threads",
			mcp.WithDescription("List a guild's active (non-archived) threads."),
			readOnly(), idParam("guild_id", "Guild"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			guild, err := a.ID("guild_id")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/guilds/{guild_id}/threads/active", Params: map[string]string{"guild_id": guild}})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(r struct {
				Threads []channelJSON `json:"threads"`
			}) string {
				if len(r.Threads) == 0 {
					return "No active threads."
				}
				lines := make([]string, len(r.Threads))
				for i, c := range r.Threads {
					lines[i] = renderChannel(c)
				}
				return strings.Join(lines, "\n")
			})
		},
	}
}

var createChannelTypes = map[string]int{"text": 0, "voice": 2, "category": 4, "announcement": 5, "stage": 13, "forum": 15}

func createChannelTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_create_channel",
			mcp.WithDescription("Create a channel in a guild."),
			mutating(),
			idParam("guild_id", "Guild"),
			mcp.WithString("name", mcp.Required(), mcp.Description("Channel name")),
			mcp.WithString("type", mcp.Enum("text", "voice", "category", "announcement", "stage", "forum"), mcp.Description("Default text")),
			mcp.WithString("topic", mcp.Description("Channel topic")),
			optIDParam("parent_id", "Category to put the channel in"),
			mcp.WithBoolean("nsfw", mcp.Description("Age-restricted channel")),
			withReason(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			guild, err := a.ID("guild_id")
			if err != nil {
				return "", err
			}
			name, err := a.RequiredString("name")
			if err != nil {
				return "", err
			}
			typeName, err := a.String("type")
			if err != nil {
				return "", err
			}
			if typeName == "" {
				typeName = "text"
			}
			typ, ok := createChannelTypes[typeName]
			if !ok {
				return "", argErr("type must be one of text, voice, category, announcement, stage, forum")
			}
			body := map[string]any{"name": name, "type": typ}
			if err := copyOptional(a, body, "topic", "parent_id", "nsfw"); err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "POST", Route: "/guilds/{guild_id}/channels", Params: map[string]string{"guild_id": guild}, Body: body, Reason: reason})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(c channelJSON) string { return "Created " + renderChannel(c) })
		},
	}
}

// copyOptional copies the given arguments into body when present, validating
// *_id keys as IDs, known booleans as booleans and position as an integer.
func copyOptional(a Args, body map[string]any, keys ...string) error {
	for _, k := range keys {
		if _, ok := a.present(k); !ok {
			continue
		}
		switch {
		case strings.HasSuffix(k, "_id"):
			id, err := a.OptionalID(k)
			if err != nil {
				return err
			}
			body[k] = id
		case k == "nsfw" || k == "archived" || k == "locked":
			b, err := a.Bool(k, false)
			if err != nil {
				return err
			}
			body[k] = b
		case k == "position":
			n, err := a.Int(k, 0, 0, 10000)
			if err != nil {
				return err
			}
			body[k] = n
		default:
			s, err := a.String(k)
			if err != nil {
				return err
			}
			body[k] = s
		}
	}
	return nil
}

func editChannelTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_edit_channel",
			mcp.WithDescription("Change a channel or thread. Only the fields given are changed; archived and locked apply to threads."),
			mutating(),
			idParam("channel_id", "Channel or thread"),
			mcp.WithString("name", mcp.Description("New name")),
			mcp.WithString("topic", mcp.Description("New topic (empty string clears it)")),
			optIDParam("parent_id", "Move under this category"),
			mcp.WithBoolean("nsfw", mcp.Description("Age-restricted")),
			mcp.WithNumber("position", mcp.Description("Sort position")),
			mcp.WithBoolean("archived", mcp.Description("Threads: archive or unarchive")),
			mcp.WithBoolean("locked", mcp.Description("Threads: lock or unlock")),
			withReason(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			channel, err := a.ID("channel_id")
			if err != nil {
				return "", err
			}
			body := map[string]any{}
			if err := copyOptional(a, body, "name", "topic", "parent_id", "nsfw", "position", "archived", "locked"); err != nil {
				return "", err
			}
			if len(body) == 0 {
				return "", argErr("give at least one field to change")
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "PATCH", Route: "/channels/{channel_id}", Params: map[string]string{"channel_id": channel}, Body: body, Reason: reason})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(c channelJSON) string { return "Updated " + renderChannel(c) })
		},
	}
}

func deleteChannelTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_delete_channel",
			mcp.WithDescription("Delete a channel or thread. This cannot be undone."),
			destructive(), idParam("channel_id", "Channel or thread"), withReason(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			channel, err := a.ID("channel_id")
			if err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "DELETE", Route: "/channels/{channel_id}", Params: map[string]string{"channel_id": channel}, Reason: reason})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(c channelJSON) string { return "Deleted " + renderChannel(c) })
		},
	}
}
