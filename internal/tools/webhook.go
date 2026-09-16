package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

// Webhook tools take no webhook ID or token argument: the webhook is the
// principal's, so a caller can only ever act as the webhook it authenticated
// with.

func webhookParams(p *auth.Principal) map[string]string {
	return map[string]string{"webhook_id": p.Webhook.ID, "webhook_token": p.Webhook.Token}
}

func threadQuery(a Args) (url.Values, error) {
	thread, err := a.OptionalID("thread_id")
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if thread != "" {
		q.Set("thread_id", thread)
	}
	return q, nil
}

func webhookGetTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_webhook_get",
			mcp.WithDescription("Show this webhook: its name and the channel and guild it posts to."),
			readOnly(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/webhooks/{webhook_id}/{webhook_token}", Params: webhookParams(p)})
			if err != nil {
				return "", err
			}
			if format == "json" {
				// Discord's webhook object includes the token and URL; never
				// echo them back.
				var obj map[string]any
				if err := json.Unmarshal(resp.Body, &obj); err != nil {
					return "", fmt.Errorf("decode Discord response: %w", err)
				}
				delete(obj, "token")
				delete(obj, "url")
				out, _ := json.Marshal(obj)
				return string(out), nil
			}
			return output(resp.Body, format, func(w struct {
				ID        string `json:"id"`
				Name      string `json:"name"`
				ChannelID string `json:"channel_id"`
				GuildID   string `json:"guild_id"`
			}) string {
				return fmt.Sprintf("Webhook %s (%s) posts to channel %s in guild %s.", w.Name, w.ID, w.ChannelID, w.GuildID)
			})
		},
	}
}

func webhookSendTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_webhook_send",
			mcp.WithDescription("Post a message as this webhook. Provide content, embeds or attachments."),
			mutating(),
			mcp.WithString("content", mcp.Description("Message text (up to 2000 characters)")),
			mcp.WithString("username", mcp.Description("Override the webhook's display name for this message")),
			mcp.WithString("avatar_url", mcp.Description("Override the webhook's avatar for this message (Discord fetches it, not this server)")),
			withEmbeds(), withAttachments(),
			optIDParam("thread_id", "Post in this thread of the webhook's channel"),
			withAllowMentions(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			body, files, err := messagePayload(a, true, true)
			if err != nil {
				return "", err
			}
			for _, k := range []string{"username", "avatar_url"} {
				v, err := a.String(k)
				if err != nil {
					return "", err
				}
				if v != "" {
					body[k] = v
				}
			}
			q, err := threadQuery(a)
			if err != nil {
				return "", err
			}
			q.Set("wait", "true")
			resp, err := p.Client.Do(ctx, discord.Call{Method: "POST", Route: "/webhooks/{webhook_id}/{webhook_token}", Params: webhookParams(p), Query: q, Body: body, Files: files})
			if err != nil {
				return "", err
			}
			return sendResult(resp, format)
		},
	}
}

func webhookMessageParams(p *auth.Principal, a Args) (map[string]string, url.Values, error) {
	m, err := a.ID("message_id")
	if err != nil {
		return nil, nil, err
	}
	q, err := threadQuery(a)
	if err != nil {
		return nil, nil, err
	}
	params := webhookParams(p)
	params["message_id"] = m
	return params, q, nil
}

const webhookMessageRoute = "/webhooks/{webhook_id}/{webhook_token}/messages/{message_id}"

func webhookGetMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_webhook_get_message",
			mcp.WithDescription("Read a message this webhook sent."),
			readOnly(), idParam("message_id", "Message"), optIDParam("thread_id", "Thread the message is in"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			params, q, err := webhookMessageParams(p, a)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: webhookMessageRoute, Params: params, Query: q})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderMessage)
		},
	}
}

func webhookEditMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_webhook_edit_message",
			mcp.WithDescription("Edit a message this webhook sent."),
			mutating(), idParam("message_id", "Message"),
			mcp.WithString("content", mcp.Description("New text")),
			withEmbeds(), withAllowMentions(), optIDParam("thread_id", "Thread the message is in"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			params, q, err := webhookMessageParams(p, a)
			if err != nil {
				return "", err
			}
			body, _, err := messagePayload(a, true, false)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "PATCH", Route: webhookMessageRoute, Params: params, Query: q, Body: body})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(m messageJSON) string { return fmt.Sprintf("Edited message #%s.", m.ID) })
		},
	}
}

func webhookDeleteMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_webhook_delete_message",
			mcp.WithDescription("Delete a message this webhook sent."),
			destructive(), idParam("message_id", "Message"), optIDParam("thread_id", "Thread the message is in")),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			params, q, err := webhookMessageParams(p, a)
			if err != nil {
				return "", err
			}
			if _, err := p.Client.Do(ctx, discord.Call{Method: "DELETE", Route: webhookMessageRoute, Params: params, Query: q}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Deleted message #%s.", params["message_id"]), nil
		},
	}
}
