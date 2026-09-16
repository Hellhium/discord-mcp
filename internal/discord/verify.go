package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Hellhium/discord-mcp/internal/credential"
	"github.com/Hellhium/discord-mcp/internal/intents"
)

// BotIdentity is the bot user behind a token.
type BotIdentity struct {
	UserID   string
	Username string
}

// VerifyBot checks the client's bot token with GET /users/@me.
func (c *Client) VerifyBot(ctx context.Context) (BotIdentity, error) {
	resp, err := c.Do(ctx, Call{Method: "GET", Route: "/users/@me"})
	if err != nil {
		return BotIdentity{}, err
	}
	var u struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(resp.Body, &u); err != nil {
		return BotIdentity{}, fmt.Errorf("decode /users/@me: %w", err)
	}
	return BotIdentity{UserID: u.ID, Username: u.Username}, nil
}

// WebhookInfo describes a webhook as Discord reports it.
type WebhookInfo struct {
	ID        string
	Name      string
	ChannelID string
	GuildID   string
}

// VerifyWebhook checks a webhook with GET /webhooks/{id}/{token}.
func (c *Client) VerifyWebhook(ctx context.Context, wh credential.Webhook) (WebhookInfo, error) {
	resp, err := c.Do(ctx, Call{
		Method: "GET",
		Route:  "/webhooks/{webhook_id}/{webhook_token}",
		Params: map[string]string{"webhook_id": wh.ID, "webhook_token": wh.Token},
	})
	if err != nil {
		return WebhookInfo{}, err
	}
	var w struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		ChannelID string `json:"channel_id"`
		GuildID   string `json:"guild_id"`
	}
	if err := json.Unmarshal(resp.Body, &w); err != nil {
		return WebhookInfo{}, fmt.Errorf("decode webhook: %w", err)
	}
	return WebhookInfo{ID: w.ID, Name: w.Name, ChannelID: w.ChannelID, GuildID: w.GuildID}, nil
}

// CheckPrivilegedIntents fails, naming the intents, when m requests a
// privileged intent that is not enabled for the application. It calls
// Discord only when m contains a privileged intent.
func (c *Client) CheckPrivilegedIntents(ctx context.Context, m intents.Mask) error {
	if m&intents.Privileged == 0 {
		return nil
	}
	resp, err := c.Do(ctx, Call{Method: "GET", Route: "/applications/@me"})
	if err != nil {
		return fmt.Errorf("read application flags: %w", err)
	}
	var app struct {
		Flags uint64 `json:"flags"`
	}
	if err := json.Unmarshal(resp.Body, &app); err != nil {
		return fmt.Errorf("decode /applications/@me: %w", err)
	}
	if missing := intents.MissingPrivileged(m, app.Flags); len(missing) > 0 {
		return fmt.Errorf("privileged gateway intent(s) %s not enabled for this application: enable them in the Discord Developer Portal (Bot → Privileged Gateway Intents)",
			strings.Join(missing, ", "))
	}
	return nil
}
