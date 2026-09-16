// Package app builds the running server from a validated config: one
// principal per instance (with its Discord client, and for bots with events a
// buffer and Gateway), the credential resolver, one MCP server per capability
// set, and the router. main stays a thin shell around Build and Close.
package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/config"
	"github.com/Hellhium/discord-mcp/internal/credential"
	"github.com/Hellhium/discord-mcp/internal/discord"
	"github.com/Hellhium/discord-mcp/internal/events"
	"github.com/Hellhium/discord-mcp/internal/intents"
	"github.com/Hellhium/discord-mcp/internal/router"
	"github.com/Hellhium/discord-mcp/internal/tools"
)

const defaultGatewayReadyTimeout = 30 * time.Second

// Options configures Build.
type Options struct {
	// Version is reported in the MCP handshake.
	Version string
	// VerifyCredentials checks every config credential against Discord and
	// opens Gateways at startup. Off, nothing contacts Discord until a tool
	// is called and event buffers stay empty (offline smoke tests).
	VerifyCredentials bool
	Log               *audit.Logger
	// HTTPClient supplies the Discord transport (tests inject the fake).
	HTTPClient          *http.Client
	GatewayReadyTimeout time.Duration
}

// App is the built server.
type App struct {
	Handler  http.Handler
	summary  []string
	gateways []*events.Gateway
	buffers  []*events.Buffer
}

// Build wires everything. On error, anything already started is closed.
func Build(ctx context.Context, cfg *config.Config, opts Options) (*App, error) {
	if opts.GatewayReadyTimeout <= 0 {
		opts.GatewayReadyTimeout = defaultGatewayReadyTimeout
	}
	dopts := discord.Options{Timeout: cfg.Discord.Timeout.Duration(), HTTPClient: opts.HTTPClient}
	a := &App{}

	entries := make([]auth.InstanceEntry, 0, len(cfg.Instances))
	for _, in := range cfg.Instances {
		p, err := a.buildInstance(ctx, in, dopts, opts)
		if err != nil {
			a.Close()
			return nil, err
		}
		tokens := make([]string, len(in.Auth.Tokens))
		for i, t := range in.Auth.Tokens {
			tokens[i] = t.Reveal()
		}
		entries = append(entries, auth.InstanceEntry{Tokens: tokens, Principal: p})
		a.summary = append(a.summary, fmt.Sprintf("instance %q: %s, %d token(s)", in.Name, p.Capability, len(tokens)))
	}

	// A zero CacheTTL here would defeat the direct-auth success cache and
	// re-verify every request against Discord.
	res, err := auth.NewResolver(entries, auth.DirectOptions{
		Enabled:  cfg.DirectAuth.Enabled,
		CacheTTL: cfg.DirectAuth.CacheTTL.Duration(),
		Discord:  dopts,
	})
	if err != nil {
		a.Close()
		return nil, err
	}
	a.summary = append(a.summary, fmt.Sprintf("direct auth: %t", cfg.DirectAuth.Enabled))

	servers := router.Servers{
		auth.CapBot:       newMCP(auth.CapBot, tools.BotTools(), opts),
		auth.CapBotEvents: newMCP(auth.CapBotEvents, append(tools.BotTools(), tools.EventTools()...), opts),
		auth.CapWebhook:   newMCP(auth.CapWebhook, tools.WebhookTools(), opts),
	}
	a.Handler = router.New(res, servers, opts.Log)
	return a, nil
}

func (a *App) buildInstance(ctx context.Context, in config.Instance, dopts discord.Options, opts Options) (*auth.Principal, error) {
	where := fmt.Sprintf("instance %q", in.Name)

	if in.Webhook != nil {
		wh, _ := credential.ParseWebhook(in.Webhook.URL.Reveal()) // shape checked by config
		c := discord.NewWebhook(dopts)
		if opts.VerifyCredentials {
			if _, err := c.VerifyWebhook(ctx, wh); err != nil {
				return nil, fmt.Errorf("%s: verify webhook: %w", where, err)
			}
		}
		return &auth.Principal{Kind: auth.KindInstance, Capability: auth.CapWebhook, Instance: in.Name, Client: c, Webhook: wh}, nil
	}

	c := discord.NewBot(in.Bot.Token.Reveal(), dopts)
	p := &auth.Principal{Kind: auth.KindInstance, Capability: auth.CapBot, Instance: in.Name, Client: c}
	if opts.VerifyCredentials {
		id, err := c.VerifyBot(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s: verify bot token: %w", where, err)
		}
		p.BotUserID = id.UserID
	}
	ev := in.Bot.Events
	if ev == nil {
		return p, nil
	}
	mask, _ := intents.Parse(ev.Intents) // names checked by config
	buf := events.NewBuffer(*ev.BufferSize)
	a.buffers = append(a.buffers, buf)
	p.Capability = auth.CapBotEvents
	p.Events = buf
	if !opts.VerifyCredentials {
		return p, nil
	}
	if err := c.CheckPrivilegedIntents(ctx, mask); err != nil {
		return nil, fmt.Errorf("%s: %w", where, err)
	}
	gw := events.NewGateway(in.Name, c.Session(), mask, buf, opts.Log)
	if err := gw.Start(opts.GatewayReadyTimeout); err != nil {
		return nil, err
	}
	a.gateways = append(a.gateways, gw)
	return p, nil
}

func newMCP(c auth.Capability, list []tools.Tool, opts Options) http.Handler {
	s := server.NewMCPServer("discord-mcp", opts.Version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
		server.WithInstructions(tools.Instructions(c)),
	)
	tools.Register(s, opts.Log, list)
	return server.NewStreamableHTTPServer(s,
		server.WithEndpointPath(router.MCPPath),
		// Stateless: each request is authenticated on its own and carries no
		// session state, so a session ID can never carry a credential.
		server.WithStateLess(true),
		server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			if p, ok := auth.FromContext(r.Context()); ok {
				return auth.WithPrincipal(ctx, p)
			}
			return ctx
		}),
	)
}

// Summary returns startup log lines: instance names, capabilities and token
// counts. Never a credential.
func (a *App) Summary() []string { return a.summary }

// Close stops Gateways and wakes event waiters.
func (a *App) Close() {
	for _, g := range a.gateways {
		_ = g.Close()
	}
	for _, b := range a.buffers {
		b.Close()
	}
}
