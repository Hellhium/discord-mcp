package discord

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Hellhium/discord-mcp/internal/credential"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
	"github.com/Hellhium/discord-mcp/internal/intents"
)

func TestVerifyBot(t *testing.T) {
	fake := discordtest.New(t)
	fake.Handle("GET /api/v10/users/@me", discordtest.JSON(200, map[string]any{"id": "42", "username": "helper", "bot": true}))
	got, err := newBot(t, fake, time.Second).VerifyBot(context.Background())
	if err != nil || got != (BotIdentity{UserID: "42", Username: "helper"}) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestVerifyBotRejected(t *testing.T) {
	fake := discordtest.New(t)
	fake.Handle("GET /api/v10/users/@me", discordtest.JSON(401, map[string]any{"message": "401: Unauthorized", "code": 0}))
	if _, err := newBot(t, fake, time.Second).VerifyBot(context.Background()); !IsUnauthorized(err) {
		t.Fatalf("err = %v, want unauthorized", err)
	}
}

func TestVerifyWebhook(t *testing.T) {
	fake := discordtest.New(t)
	fake.Handle("GET /api/v10/webhooks/{id}/{token}", discordtest.JSON(200, map[string]any{
		"id": "7", "name": "alerts", "channel_id": "8", "guild_id": "9",
	}))
	c := NewWebhook(Options{HTTPClient: fake.HTTPClient()})
	got, err := c.VerifyWebhook(context.Background(), credential.Webhook{ID: "7", Token: "tok"})
	if err != nil || got != (WebhookInfo{ID: "7", Name: "alerts", ChannelID: "8", GuildID: "9"}) {
		t.Fatalf("got %+v, %v", got, err)
	}
	if p := fake.Requests()[0].Path; p != "/api/v10/webhooks/7/tok" {
		t.Fatalf("path = %s", p)
	}
}

func TestCheckPrivilegedIntents(t *testing.T) {
	tests := []struct {
		name  string
		flags uint64
		mask  []string
		want  string // "" = no error
	}{
		{"not privileged", 0, []string{"guilds", "guild_messages"}, ""},
		{"content enabled (limited)", 1 << 19, []string{"message_content"}, ""},
		{"content missing", 0, []string{"message_content", "guild_members"}, `privileged gateway intent(s) guild_members, message_content`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := discordtest.New(t)
			fake.Handle("GET /api/v10/applications/@me", discordtest.JSON(200, map[string]any{"id": "1", "flags": tt.flags}))
			m, err := intents.Parse(tt.mask)
			if err != nil {
				t.Fatal(err)
			}
			err = newBot(t, fake, time.Second).CheckPrivilegedIntents(context.Background(), m)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if tt.name == "not privileged" && len(fake.Requests()) != 0 {
					t.Fatal("no privileged intents requested: must not call Discord")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}
