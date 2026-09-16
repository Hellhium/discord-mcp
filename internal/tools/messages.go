package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

func messageTools() []Tool {
	return []Tool{
		readMessagesTool(), getMessageTool(), sendMessageTool(), editMessageTool(), deleteMessageTool(),
		pinMessageTool(), addReactionTool(), removeReactionTool(), searchMessagesTool(), sendDMTool(),
	}
}

func chanMsg(channelID, messageID string) map[string]string {
	return map[string]string{"channel_id": channelID, "message_id": messageID}
}

// channelAndMessage reads the two IDs most message tools take.
func channelAndMessage(a Args) (string, string, error) {
	c, err := a.ID("channel_id")
	if err != nil {
		return "", "", err
	}
	m, err := a.ID("message_id")
	if err != nil {
		return "", "", err
	}
	return c, m, nil
}

func readMessagesTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_read_messages",
			mcp.WithDescription("Read message history from a channel or thread, printed oldest first. Use at most one of before, after, around."),
			readOnly(),
			idParam("channel_id", "Channel or thread"),
			mcp.WithNumber("limit", mcp.Description("Messages to return, 1-100 (default 50)")),
			optIDParam("before", "Messages before this message"),
			optIDParam("after", "Messages after this message"),
			optIDParam("around", "Messages around this message"),
			withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			channel, err := a.ID("channel_id")
			if err != nil {
				return "", err
			}
			limit, err := a.Int("limit", 50, 1, 100)
			if err != nil {
				return "", err
			}
			anchor, err := a.OneOf("before", "after", "around")
			if err != nil {
				return "", err
			}
			q := url.Values{"limit": {strconv.Itoa(limit)}}
			if anchor != "" {
				id, err := a.ID(anchor)
				if err != nil {
					return "", err
				}
				q.Set(anchor, id)
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/channels/{channel_id}/messages", Params: map[string]string{"channel_id": channel}, Query: q})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderMessages)
		},
	}
}

func getMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_get_message",
			mcp.WithDescription("Read one message."),
			readOnly(), idParam("channel_id", "Channel or thread"), idParam("message_id", "Message"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			c, m, err := channelAndMessage(a)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/channels/{channel_id}/messages/{message_id}", Params: chanMsg(c, m)})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderMessage)
		},
	}
}

// sendResult reports a created message.
func sendResult(resp discord.Response, format string) (string, error) {
	return output(resp.Body, format, func(m messageJSON) string {
		return fmt.Sprintf("Sent message #%s in channel %s.", m.ID, m.ChannelID)
	})
}

func sendMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_send_message",
			mcp.WithDescription("Send a message to a channel or thread. Provide content, embeds or attachments."),
			mutating(),
			idParam("channel_id", "Channel or thread"),
			mcp.WithString("content", mcp.Description("Message text (up to 2000 characters)")),
			withEmbeds(), withAttachments(),
			optIDParam("reply_to", "Reply to this message in the same channel"),
			withAllowMentions(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			channel, err := a.ID("channel_id")
			if err != nil {
				return "", err
			}
			body, files, err := messagePayload(a, true, true)
			if err != nil {
				return "", err
			}
			replyTo, err := a.OptionalID("reply_to")
			if err != nil {
				return "", err
			}
			if replyTo != "" {
				body["message_reference"] = map[string]any{"message_id": replyTo, "fail_if_not_exists": false}
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "POST", Route: "/channels/{channel_id}/messages", Params: map[string]string{"channel_id": channel}, Body: body, Files: files})
			if err != nil {
				return "", err
			}
			return sendResult(resp, format)
		},
	}
}

func editMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_edit_message",
			mcp.WithDescription("Edit a message the bot sent: replace its content and/or embeds."),
			mutating(),
			idParam("channel_id", "Channel or thread"), idParam("message_id", "Message"),
			mcp.WithString("content", mcp.Description("New text")),
			withEmbeds(), withAllowMentions(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			c, m, err := channelAndMessage(a)
			if err != nil {
				return "", err
			}
			body, _, err := messagePayload(a, true, false)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "PATCH", Route: "/channels/{channel_id}/messages/{message_id}", Params: chanMsg(c, m), Body: body})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(msg messageJSON) string { return fmt.Sprintf("Edited message #%s.", msg.ID) })
		},
	}
}

func deleteMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_delete_message",
			mcp.WithDescription("Delete a message."),
			destructive(), idParam("channel_id", "Channel or thread"), idParam("message_id", "Message"), withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			c, m, err := channelAndMessage(a)
			if err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			if _, err := p.Client.Do(ctx, discord.Call{Method: "DELETE", Route: "/channels/{channel_id}/messages/{message_id}", Params: chanMsg(c, m), Reason: reason}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Deleted message #%s.", m), nil
		},
	}
}

func pinMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_pin_message",
			mcp.WithDescription("Pin (pinned=true) or unpin (pinned=false) a message."),
			mutating(), idParam("channel_id", "Channel or thread"), idParam("message_id", "Message"),
			mcp.WithBoolean("pinned", mcp.Required(), mcp.Description("true to pin, false to unpin")),
			withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			c, m, err := channelAndMessage(a)
			if err != nil {
				return "", err
			}
			if _, ok := a.present("pinned"); !ok {
				return "", argErr("pinned is required")
			}
			pinned, err := a.Bool("pinned", false)
			if err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			method, verb := http.MethodPut, "Pinned"
			if !pinned {
				method, verb = http.MethodDelete, "Unpinned"
			}
			if _, err := p.Client.Do(ctx, discord.Call{Method: method, Route: "/channels/{channel_id}/messages/pins/{message_id}", Params: chanMsg(c, m), Reason: reason}); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s message #%s.", verb, m), nil
		},
	}
}

var customEmojiRe = regexp.MustCompile(`^<a?:([A-Za-z0-9_]+):([0-9]+)>$`)

// normalizeEmoji accepts the <:name:id> form as it appears in message text
// and turns it into the name:id form the reactions API expects.
func normalizeEmoji(s string) string {
	if m := customEmojiRe.FindStringSubmatch(s); m != nil {
		return m[1] + ":" + m[2]
	}
	return s
}

const emojiDesc = "Unicode emoji (👍) or custom emoji as name:id or <:name:id>"

func addReactionTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_add_reaction",
			mcp.WithDescription("React to a message as the bot."),
			mutating(), idParam("channel_id", "Channel or thread"), idParam("message_id", "Message"),
			mcp.WithString("emoji", mcp.Required(), mcp.Description(emojiDesc))),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			c, m, err := channelAndMessage(a)
			if err != nil {
				return "", err
			}
			emoji, err := a.RequiredString("emoji")
			if err != nil {
				return "", err
			}
			params := chanMsg(c, m)
			params["emoji"] = normalizeEmoji(emoji)
			if _, err := p.Client.Do(ctx, discord.Call{Method: "PUT", Route: "/channels/{channel_id}/messages/{message_id}/reactions/{emoji}/@me", Params: params}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Reacted %s to message #%s.", emoji, m), nil
		},
	}
}

func removeReactionTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_remove_reaction",
			mcp.WithDescription("Remove the bot's reaction, or another user's reaction when user_id is given."),
			destructive(), idParam("channel_id", "Channel or thread"), idParam("message_id", "Message"),
			mcp.WithString("emoji", mcp.Required(), mcp.Description(emojiDesc)),
			optIDParam("user_id", "Remove this user's reaction instead of the bot's")),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			c, m, err := channelAndMessage(a)
			if err != nil {
				return "", err
			}
			emoji, err := a.RequiredString("emoji")
			if err != nil {
				return "", err
			}
			user, err := a.OptionalID("user_id")
			if err != nil {
				return "", err
			}
			params := chanMsg(c, m)
			params["emoji"] = normalizeEmoji(emoji)
			route := "/channels/{channel_id}/messages/{message_id}/reactions/{emoji}/@me"
			if user != "" {
				route = "/channels/{channel_id}/messages/{message_id}/reactions/{emoji}/{user_id}"
				params["user_id"] = user
			}
			if _, err := p.Client.Do(ctx, discord.Call{Method: "DELETE", Route: route, Params: params}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Removed reaction %s from message #%s.", emoji, m), nil
		},
	}
}

func searchMessagesTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_search_messages",
			mcp.WithDescription("Search a guild's messages. Needs Read Message History; results depend on the Message Content intent."),
			readOnly(),
			idParam("guild_id", "Guild to search"),
			mcp.WithString("content", mcp.Description("Text to search for")),
			optIDParam("channel_id", "Only this channel"),
			optIDParam("author_id", "Only messages by this user"),
			mcp.WithNumber("limit", mcp.Description("Results, 1-25 (default 25)")),
			mcp.WithNumber("offset", mcp.Description("Skip this many results, 0-9975")),
			withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			guild, err := a.ID("guild_id")
			if err != nil {
				return "", err
			}
			q := url.Values{}
			content, err := a.String("content")
			if err != nil {
				return "", err
			}
			if content != "" {
				q.Set("content", content)
			}
			for _, key := range []string{"channel_id", "author_id"} {
				id, err := a.OptionalID(key)
				if err != nil {
					return "", err
				}
				if id != "" {
					q.Set(key, id)
				}
			}
			limit, err := a.Int("limit", 25, 1, 25)
			if err != nil {
				return "", err
			}
			offset, err := a.Int("offset", 0, 0, 9975)
			if err != nil {
				return "", err
			}
			q.Set("limit", strconv.Itoa(limit))
			if offset > 0 {
				q.Set("offset", strconv.Itoa(offset))
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/guilds/{guild_id}/messages/search", Params: map[string]string{"guild_id": guild}, Query: q})
			if err != nil {
				return "", err
			}
			if resp.Status == http.StatusAccepted {
				var pending struct {
					RetryAfter float64 `json:"retry_after"`
				}
				_ = json.Unmarshal(resp.Body, &pending)
				return "", fmt.Errorf("Discord is still indexing this guild for search: retry after %gs", pending.RetryAfter)
			}
			return output(resp.Body, format, func(r struct {
				TotalResults int             `json:"total_results"`
				Messages     [][]messageJSON `json:"messages"`
			}) string {
				var hits []string
				for _, group := range r.Messages {
					if len(group) > 0 {
						hits = append(hits, renderMessage(group[0]))
					}
				}
				if len(hits) == 0 {
					return "No results."
				}
				return fmt.Sprintf("%d result(s), showing %d:\n%s", r.TotalResults, len(hits), strings.Join(hits, "\n"))
			})
		},
	}
}

func sendDMTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_send_dm",
			mcp.WithDescription("Send a direct message to a user. The user must share a server with the bot and accept DMs."),
			mutating(),
			idParam("user_id", "Recipient"),
			mcp.WithString("content", mcp.Description("Message text")),
			withEmbeds(), withAttachments(), withAllowMentions(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			user, err := a.ID("user_id")
			if err != nil {
				return "", err
			}
			body, files, err := messagePayload(a, true, true)
			if err != nil {
				return "", err
			}
			dm, err := p.Client.Do(ctx, discord.Call{Method: "POST", Route: "/users/@me/channels", Body: map[string]any{"recipient_id": user}})
			if err != nil {
				return "", err
			}
			var ch channelJSON
			if err := json.Unmarshal(dm.Body, &ch); err != nil || ch.ID == "" {
				return "", fmt.Errorf("decode DM channel: unexpected response")
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "POST", Route: "/channels/{channel_id}/messages", Params: map[string]string{"channel_id": ch.ID}, Body: body, Files: files})
			if err != nil {
				return "", err
			}
			return sendResult(resp, format)
		},
	}
}
