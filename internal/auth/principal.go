// Package auth turns the credential on an HTTP request into a Principal: a
// config instance, a direct bot token or a direct webhook. The principal
// carries its own Discord client, so a tool handler can only ever act with the
// credential of the request it is serving.
package auth

import (
	"context"

	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/credential"
	"github.com/Hellhium/discord-mcp/internal/discord"
	"github.com/Hellhium/discord-mcp/internal/events"
)

// Kind is where a principal's Discord credential came from.
type Kind string

const (
	KindInstance      Kind = "instance"
	KindDirectBot     Kind = "direct_bot"
	KindDirectWebhook Kind = "direct_webhook"
)

// Capability selects the MCP server, and so the tool list, a principal gets.
type Capability int

const (
	CapBot Capability = iota
	CapBotEvents
	CapWebhook
)

func (c Capability) String() string {
	switch c {
	case CapBot:
		return "bot"
	case CapBotEvents:
		return "bot+events"
	case CapWebhook:
		return "webhook"
	}
	return "unknown"
}

// Principal is an authenticated caller.
type Principal struct {
	Kind       Kind
	Capability Capability
	// Instance is the config instance name (KindInstance only).
	Instance string
	// CredentialHash is "sha256:<8 hex>" for direct principals.
	CredentialHash string
	Client         *discord.Client
	// BotUserID is the bot's own user ID (bot capabilities).
	BotUserID string
	// Webhook is set for CapWebhook.
	Webhook credential.Webhook
	// Events is set for CapBotEvents.
	Events *events.Buffer

	invalidate func()
}

// Actor identifies the principal in the audit log.
func (p *Principal) Actor() audit.Actor {
	return audit.Actor{Kind: string(p.Kind), Instance: p.Instance, Credential: p.CredentialHash}
}

// Invalidate evicts a direct credential Discord has stopped accepting. It is a
// no-op for config instances.
func (p *Principal) Invalidate() {
	if p.invalidate != nil {
		p.invalidate()
	}
}

type ctxKey struct{}

// WithPrincipal attaches p to ctx.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext returns the principal attached by WithPrincipal.
func FromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(*Principal)
	return p, ok && p != nil
}
