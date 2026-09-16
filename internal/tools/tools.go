// Package tools defines the MCP tools and the single wrapper every tool call
// goes through.
//
// A tool is a Handler: it receives the request's Principal — whose Discord
// client is the only credential it can use — and typed arguments, and returns
// text for the LLM. The wrapper supplies the principal from the request
// context, records every Discord call, maps errors to audit outcomes and tool
// errors, evicts a direct credential Discord has revoked, and writes exactly
// one audit line per call.
package tools

import (
	"context"
	"errors"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

// Handler implements one tool.
type Handler func(ctx context.Context, p *auth.Principal, a Args) (string, error)

// Tool pairs an MCP definition with its handler.
type Tool struct {
	Def    mcp.Tool
	Handle Handler
}

// Register adds every tool to s behind the audit wrapper.
func Register(s *server.MCPServer, log *audit.Logger, list []Tool) {
	for _, t := range list {
		s.AddTool(t.Def, wrap(t.Def.Name, t.Handle, log))
	}
}

func wrap(name string, h Handler, log *audit.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		p, ok := auth.FromContext(ctx)
		if !ok {
			// Unreachable behind the router; never act without a credential.
			return mcp.NewToolResultError("unauthenticated"), nil
		}
		args := Args(req.GetArguments())
		if args == nil {
			args = Args{}
		}
		// Snapshot what was asked for before the handler runs: Args is a
		// plain map, and a handler is free to add, delete or rewrite keys
		// for its own convenience. Auditing the post-call map would then log
		// what the handler did to its arguments, not what the caller sent.
		targets := audit.Targets(args)
		sanitized := audit.SanitizeArgs(args)
		rec := discord.NewRecorder()
		text, err := h(discord.WithRecorder(ctx, rec), p, args)
		outcome, msg := classify(err)
		if outcome == audit.OutcomeCredentialRejected {
			p.Invalidate()
		}
		log.Action(audit.Action{
			Actor:    p.Actor(),
			Tool:     name,
			Outcome:  outcome,
			Duration: time.Since(start),
			Targets:  targets,
			Args:     sanitized,
			Calls:    rec.Calls(),
			Error:    msg,
		})
		if err != nil {
			return mcp.NewToolResultError(msg), nil
		}
		return mcp.NewToolResultText(text), nil
	}
}

func classify(err error) (audit.Outcome, string) {
	if err == nil {
		return audit.OutcomeOK, ""
	}
	var ae *discord.ArgError
	var rl *discord.RateLimitedError
	switch {
	case errors.As(err, &ae):
		return audit.OutcomeInvalidArgs, err.Error()
	case discord.IsUnauthorized(err):
		return audit.OutcomeCredentialRejected, "Discord rejected the credential (401): it is no longer valid"
	case errors.As(err, &rl):
		return audit.OutcomeRateLimited, err.Error()
	default:
		return audit.OutcomeDiscordError, err.Error()
	}
}
