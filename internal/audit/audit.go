// Package audit writes the action log: one JSON line on stdout per tool call,
// reads included, plus authentication rejections and Gateway lifecycle
// events. Discord enforces permissions; this log is how an operator sees what
// was done with them.
//
// Nothing here receives a credential. Actors identify config instances by name
// and direct credentials by a short hash prefix, Discord calls by route
// template, and SanitizeArgs strips attachment content before arguments are
// logged.
package audit

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/Hellhium/discord-mcp/internal/discord"
)

// Outcome classifies a tool call.
type Outcome string

const (
	OutcomeOK                 Outcome = "ok"
	OutcomeInvalidArgs        Outcome = "invalid_args"
	OutcomeDiscordError       Outcome = "discord_error"
	OutcomeRateLimited        Outcome = "rate_limited"
	OutcomeCredentialRejected Outcome = "credential_rejected"
)

// Actor is who made the call.
type Actor struct {
	Kind       string `json:"kind"`
	Instance   string `json:"instance,omitempty"`
	Credential string `json:"credential,omitempty"`
}

// Action is one tool call.
type Action struct {
	Actor    Actor
	Tool     string
	Outcome  Outcome
	Duration time.Duration
	Targets  map[string]string
	Args     map[string]any
	Calls    []discord.CallRecord
	Error    string
}

// Logger writes audit lines.
type Logger struct{ l *slog.Logger }

// New returns a logger writing JSON lines to w.
func New(w io.Writer) *Logger {
	return &Logger{l: slog.New(slog.NewJSONHandler(w, nil))}
}

// Action logs one tool call: INFO when ok, WARN otherwise.
func (l *Logger) Action(a Action) {
	level := slog.LevelInfo
	if a.Outcome != OutcomeOK {
		level = slog.LevelWarn
	}
	targets := a.Targets
	if targets == nil {
		targets = map[string]string{}
	}
	calls := a.Calls
	if calls == nil {
		calls = []discord.CallRecord{}
	}
	args := a.Args
	if args == nil {
		args = map[string]any{}
	}
	attrs := []slog.Attr{
		slog.Any("principal", a.Actor),
		slog.String("tool", a.Tool),
		slog.String("outcome", string(a.Outcome)),
		slog.Int64("duration_ms", a.Duration.Milliseconds()),
		slog.Any("targets", targets),
		slog.Any("args", args),
		slog.Any("discord", calls),
	}
	if a.Error != "" {
		attrs = append(attrs, slog.String("error", a.Error))
	}
	l.l.LogAttrs(context.Background(), level, "action", attrs...)
}

// AuthRejected logs a refused request. header is the header the credential
// was read from ("" when none was present); the value is never logged.
func (l *Logger) AuthRejected(reason, header string) {
	l.l.LogAttrs(context.Background(), slog.LevelWarn, "auth_rejected",
		slog.String("reason", reason), slog.String("header", header))
}

// Gateway logs a Gateway lifecycle event for a configured bot.
func (l *Logger) Gateway(instance, event string, err error) {
	if err != nil {
		l.l.LogAttrs(context.Background(), slog.LevelWarn, event,
			slog.String("instance", instance), slog.String("error", err.Error()))
		return
	}
	l.l.LogAttrs(context.Background(), slog.LevelInfo, event, slog.String("instance", instance))
}

// Info logs an operational message (startup, shutdown).
func (l *Logger) Info(msg string, args ...any) { l.l.Info(msg, args...) }
