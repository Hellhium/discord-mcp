package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/credential"
	"github.com/Hellhium/discord-mcp/internal/discord"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
	"github.com/Hellhium/discord-mcp/internal/events"
)

const (
	hBotToken     = "MTIz.GAbC.harnessharness"
	hBotUserID    = "42"
	hWebhookID    = "555"
	hWebhookToken = "hooktoken"
)

type harness struct {
	t    *testing.T
	fake *discordtest.Server
	logs *bytes.Buffer
	log  *audit.Logger
	p    *auth.Principal
}

func newHarness(t *testing.T, capability auth.Capability) *harness {
	t.Helper()
	fake := discordtest.New(t)
	opts := discord.Options{Timeout: 2 * time.Second, HTTPClient: fake.HTTPClient()}
	p := &auth.Principal{Kind: auth.KindInstance, Capability: capability, Instance: "test"}
	switch capability {
	case auth.CapWebhook:
		p.Client = discord.NewWebhook(opts)
		p.Webhook = credential.Webhook{ID: hWebhookID, Token: hWebhookToken}
	case auth.CapBotEvents:
		p.Client = discord.NewBot(hBotToken, opts)
		p.BotUserID = hBotUserID
		p.Events = events.NewBuffer(100)
	default:
		p.Client = discord.NewBot(hBotToken, opts)
		p.BotUserID = hBotUserID
	}
	var logs bytes.Buffer
	return &harness{t: t, fake: fake, logs: &logs, log: audit.New(&logs), p: p}
}

func (h *harness) handle(pattern string, fn http.HandlerFunc) { h.fake.Handle(pattern, fn) }

// call runs tool through the same wrapper Register uses.
func (h *harness) call(tool Tool, args map[string]any) (string, bool) {
	h.t.Helper()
	var req mcp.CallToolRequest
	req.Params.Name = tool.Def.Name
	req.Params.Arguments = args
	res, err := wrap(tool.Def.Name, tool.Handle, h.log)(auth.WithPrincipal(context.Background(), h.p), req)
	if err != nil {
		h.t.Fatalf("%s returned a protocol error: %v", tool.Def.Name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError
}

func (h *harness) requests() int { return len(h.fake.Requests()) }

func (h *harness) last() discordtest.Request {
	h.t.Helper()
	reqs := h.fake.Requests()
	if len(reqs) == 0 {
		h.t.Fatal("no request reached Discord")
	}
	return reqs[len(reqs)-1]
}

func (h *harness) lastBody() map[string]any {
	h.t.Helper()
	var m map[string]any
	if err := json.Unmarshal(h.last().Body, &m); err != nil {
		h.t.Fatalf("request body is not a JSON object: %s", h.last().Body)
	}
	return m
}

func (h *harness) lastAudit() map[string]any {
	h.t.Helper()
	lines := strings.Split(strings.TrimSpace(h.logs.String()), "\n")
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		h.t.Fatalf("audit line is not JSON: %q", lines[len(lines)-1])
	}
	return m
}
