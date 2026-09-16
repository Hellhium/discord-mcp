// Package e2e drives the full stack — router, auth, MCP servers, tools and the
// Discord client — over real HTTP against the fake Discord. Its central
// assertions: each caller sees only its capability set's tools, and every
// Discord request carries the credential of the caller that made it.
package e2e

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/app"
	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/config"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
	"github.com/Hellhium/discord-mcp/internal/router"
)

const (
	tokBot    = "bot-client-token-bot-client-tok"
	tokEvents = "events-client-token-events-cli"
	tokHook   = "hook-client-token-hook-client-t"
	botSecret = "MTIz.GAbC.configbotsecretvalue"
	hookID    = "123456789012345678"
	hookTok   = "ConfigHookSecret_123"
	directBot = "NDU2.XyZ.directbotsecretvalue"
)

func cfg(t *testing.T, direct bool) *config.Config {
	t.Helper()
	c, err := config.Parse([]byte(fmt.Sprintf(`
direct_auth: {enabled: %t}
instances:
  - name: assistant
    auth: {tokens: [%q]}
    bot: {token: %q}
  - name: watcher
    auth: {tokens: [%q]}
    bot:
      token: %q
      events: {intents: [guilds, guild_messages]}
  - name: alerts
    auth: {tokens: [%q]}
    webhook: {url: "https://discord.com/api/webhooks/%s/%s"}
`, direct, tokBot, botSecret, tokEvents, botSecret, tokHook, hookID, hookTok)))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type stack struct {
	srv  *httptest.Server
	fake *discordtest.Server
	logs *bytes.Buffer
}

func start(t *testing.T, direct bool) *stack {
	t.Helper()
	fake := discordtest.New(t)
	var logs bytes.Buffer
	a, err := app.Build(context.Background(), cfg(t, direct), app.Options{
		Version: "test", Log: audit.New(&logs), HTTPClient: fake.HTTPClient(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	return &stack{srv: srv, fake: fake, logs: &logs}
}

func connect(t *testing.T, s *stack, header, value string) (*client.Client, *mcp.InitializeResult, error) {
	t.Helper()
	mc, err := client.NewStreamableHttpClient(s.srv.URL+router.MCPPath, transport.WithHTTPHeaders(map[string]string{header: value}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mc.Close() })
	ctx := context.Background()
	if err := mc.Start(ctx); err != nil {
		return nil, nil, err
	}
	res, err := mc.Initialize(ctx, mcp.InitializeRequest{})
	return mc, res, err
}

func toolNames(t *testing.T, mc *client.Client) []string {
	t.Helper()
	res, err := mc.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}
	return names
}

func call(t *testing.T, mc *client.Client, name string, args map[string]any) (string, bool) {
	t.Helper()
	var req mcp.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := mc.CallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError
}

func TestToolListsPerCapability(t *testing.T) {
	s := start(t, false)
	tests := []struct {
		name   string
		token  string
		count  int
		has    []string
		hasNot []string
	}{
		{"bot", tokBot, 31, []string{"discord_send_message", "discord_request", "discord_search_messages"}, []string{"discord_poll_events", "discord_webhook_send"}},
		{"bot+events", tokEvents, 33, []string{"discord_send_message", "discord_poll_events", "discord_wait_for_message"}, []string{"discord_webhook_send"}},
		{"webhook", tokHook, 5, []string{"discord_webhook_send"}, []string{"discord_send_message", "discord_request"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc, _, err := connect(t, s, "Authorization", "Bearer "+tt.token)
			if err != nil {
				t.Fatal(err)
			}
			names := toolNames(t, mc)
			if len(names) != tt.count {
				t.Errorf("%d tools, want %d: %v", len(names), tt.count, names)
			}
			for _, n := range tt.has {
				if !slices.Contains(names, n) {
					t.Errorf("missing %s", n)
				}
			}
			for _, n := range tt.hasNot {
				if slices.Contains(names, n) {
					t.Errorf("must not expose %s", n)
				}
			}
		})
	}
}

func TestCallsCarryTheCallersCredential(t *testing.T) {
	s := start(t, false)
	s.fake.Handle("POST /api/v10/channels/{id}/messages", discordtest.JSON(200, map[string]any{"id": "900", "channel_id": "5"}))
	s.fake.Handle("POST /api/v10/webhooks/{id}/{token}", discordtest.JSON(200, map[string]any{"id": "901", "channel_id": "6"}))

	bot, _, err := connect(t, s, "X-API-Key", tokBot)
	if err != nil {
		t.Fatal(err)
	}
	if text, isErr := call(t, bot, "discord_send_message", map[string]any{"channel_id": "5", "content": "hi"}); isErr {
		t.Fatal(text)
	}
	reqs := s.fake.Requests()
	if got := reqs[len(reqs)-1].Header.Get("Authorization"); got != "Bot "+botSecret {
		t.Fatalf("bot call Authorization = %q", got)
	}

	hook, _, err := connect(t, s, "Authorization", "Bearer "+tokHook)
	if err != nil {
		t.Fatal(err)
	}
	if text, isErr := call(t, hook, "discord_webhook_send", map[string]any{"content": "deployed"}); isErr {
		t.Fatal(text)
	}
	reqs = s.fake.Requests()
	last := reqs[len(reqs)-1]
	if last.Path != "/api/v10/webhooks/"+hookID+"/"+hookTok || last.Header.Get("Authorization") != "" {
		t.Fatalf("webhook call = %s auth=%q", last.Path, last.Header.Get("Authorization"))
	}

	logs := s.logs.String()
	for _, secret := range []string{botSecret, hookTok, tokBot, tokHook} {
		if strings.Contains(logs, secret) {
			t.Fatalf("audit log leaks a credential:\n%s", logs)
		}
	}
	if !strings.Contains(logs, `"instance":"assistant"`) || !strings.Contains(logs, `"instance":"alerts"`) {
		t.Fatalf("audit log missing instance names:\n%s", logs)
	}
}

func TestUnknownTokenRejected(t *testing.T) {
	s := start(t, false)
	if _, _, err := connect(t, s, "Authorization", "Bearer "+directBot); err == nil {
		t.Fatal("direct credential accepted with direct auth disabled")
	}
	if !strings.Contains(s.logs.String(), `"msg":"auth_rejected"`) {
		t.Fatal("rejection not logged")
	}
	if len(s.fake.Requests()) != 0 {
		t.Fatal("rejection contacted Discord")
	}
}

func TestDirectBotToken(t *testing.T) {
	s := start(t, true)
	s.fake.Handle("GET /api/v10/users/@me", discordtest.JSON(200, map[string]any{"id": "77", "username": "direct"}))
	mc, _, err := connect(t, s, "Authorization", "Bearer "+directBot)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(toolNames(t, mc)); n != 31 {
		t.Fatalf("direct bot sees %d tools, want 31", n)
	}
	text, isErr := call(t, mc, "discord_get_me", nil)
	if isErr || text != "direct (77)" {
		t.Fatalf("get_me = %q", text)
	}
	reqs := s.fake.Requests()
	if got := reqs[len(reqs)-1].Header.Get("Authorization"); got != "Bot "+directBot {
		t.Fatalf("Authorization = %q", got)
	}
	if strings.Contains(s.logs.String(), directBot) || !strings.Contains(s.logs.String(), `"kind":"direct_bot"`) {
		t.Fatalf("audit log:\n%s", s.logs.String())
	}
}

func TestHandshakeInstructionsHideConfig(t *testing.T) {
	s := start(t, false)
	for _, tok := range []string{tokBot, tokEvents, tokHook} {
		_, res, err := connect(t, s, "Authorization", "Bearer "+tok)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"assistant", "watcher", "alerts", botSecret, hookTok, hookID, tok} {
			if strings.Contains(res.Instructions, secret) {
				t.Fatalf("instructions leak %q: %s", secret, res.Instructions)
			}
		}
	}
}

func TestStartupVerificationFailsWithoutLeaking(t *testing.T) {
	fake := discordtest.New(t)
	fake.Handle("GET /api/v10/users/@me", discordtest.JSON(401, map[string]any{"message": "401: Unauthorized", "code": 0}))
	c, err := config.Parse([]byte(fmt.Sprintf("instances:\n  - name: assistant\n    auth: {tokens: [%q]}\n    bot: {token: %q}\n", tokBot, botSecret)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Build(context.Background(), c, app.Options{VerifyCredentials: true, Log: audit.New(&bytes.Buffer{}), HTTPClient: fake.HTTPClient()})
	if err == nil || !strings.Contains(err.Error(), `instance "assistant"`) || strings.Contains(err.Error(), botSecret) {
		t.Fatalf("err = %v", err)
	}
}
