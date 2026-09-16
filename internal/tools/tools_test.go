package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

func fakeTool(h Handler) Tool {
	return Tool{Def: mcp.NewTool("discord_fake", mcp.WithDescription("test")), Handle: h}
}

func TestWrapOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		outcome audit.Outcome
		isError bool
		text    string
	}{
		{"ok", nil, audit.OutcomeOK, false, "done"},
		{"invalid args", &discord.ArgError{Msg: "channel_id is required"}, audit.OutcomeInvalidArgs, true, "channel_id is required"},
		{"discord error", &discord.APIError{Status: 403, Code: 50013, Message: "Missing Permissions"}, audit.OutcomeDiscordError, true, "Discord 403: Missing Permissions (50013)"},
		{"unauthorized", &discord.APIError{Status: 401, Message: "401: Unauthorized"}, audit.OutcomeCredentialRejected, true, "Discord rejected the credential"},
		{"rate limited", &discord.RateLimitedError{RetryAfter: 3 * time.Second}, audit.OutcomeRateLimited, true, "retry after 3s"},
		{"unavailable", &discord.UnavailableError{Cause: "timeout"}, audit.OutcomeDiscordError, true, "Discord unavailable: timeout"},
		{"other", errors.New("decode Discord response: boom"), audit.OutcomeDiscordError, true, "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, auth.CapBot)
			text, isErr := h.call(fakeTool(func(context.Context, *auth.Principal, Args) (string, error) {
				return "done", tt.err
			}), map[string]any{"channel_id": "7", "content": "hi"})
			if isErr != tt.isError || !strings.Contains(text, tt.text) {
				t.Fatalf("text=%q isError=%v", text, isErr)
			}
			a := h.lastAudit()
			if a["outcome"] != string(tt.outcome) || a["tool"] != "discord_fake" {
				t.Fatalf("audit = %v", a)
			}
			if !reflect.DeepEqual(a["targets"], map[string]any{"channel_id": "7"}) {
				t.Fatalf("targets = %v", a["targets"])
			}
			if p := a["principal"].(map[string]any); p["kind"] != "instance" || p["instance"] != "test" {
				t.Fatalf("principal = %v", p)
			}
		})
	}
}

func TestWrapRecordsDiscordCalls(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/users/@me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"42"}`))
	})
	h.call(fakeTool(func(ctx context.Context, p *auth.Principal, _ Args) (string, error) {
		_, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/users/@me"})
		return "", err
	}), nil)
	calls := h.lastAudit()["discord"].([]any)
	if len(calls) != 1 || calls[0].(map[string]any)["route"] != "/users/@me" {
		t.Fatalf("discord calls = %v", calls)
	}
}

func TestWrapAuditsArgumentsAsSent(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.call(fakeTool(func(_ context.Context, _ *auth.Principal, a Args) (string, error) {
		// A handler rewriting its own arguments must not change what gets
		// audited: the log records what the caller asked for.
		a["content"] = "REWRITTEN"
		delete(a, "channel_id")
		a["extra"] = "added by handler"
		return "done", nil
	}), map[string]any{"channel_id": "7", "content": "hi"})
	a := h.lastAudit()
	if !reflect.DeepEqual(a["targets"], map[string]any{"channel_id": "7"}) {
		t.Fatalf("targets = %v, want the original channel_id", a["targets"])
	}
	args := a["args"].(map[string]any)
	if args["content"] != "hi" {
		t.Fatalf("args.content = %v, want the original value", args["content"])
	}
	if _, ok := args["extra"]; ok {
		t.Fatalf("args = %v, must not contain a key the handler added", args)
	}
}

func TestWrapWithoutPrincipalRefuses(t *testing.T) {
	var called bool
	tool := fakeTool(func(context.Context, *auth.Principal, Args) (string, error) { called = true; return "", nil })
	var req mcp.CallToolRequest
	res, err := wrap(tool.Def.Name, tool.Handle, audit.New(&strings.Builder{}))(context.Background(), req)
	if err != nil || !res.IsError || called {
		t.Fatalf("res=%v err=%v called=%v", res, err, called)
	}
}

func TestArgs(t *testing.T) {
	a := Args{
		"s": "x", "empty": "", "num": float64(5), "frac": 1.5, "big": float64(500), "b": true,
		"id": "123", "badid": "12a", "numid": float64(123), "obj": map[string]any{"k": "v"},
		"list": []any{"a", "b"}, "mixed": []any{"a", 1.0},
	}
	check := func(name string, err error, wantErr bool) {
		t.Helper()
		var ae *discord.ArgError
		if (err != nil) != wantErr || (err != nil && !errors.As(err, &ae)) {
			t.Errorf("%s: err = %v, wantErr %v", name, err, wantErr)
		}
	}
	s, err := a.String("s")
	check("String", err, s != "x")
	_, err = a.String("num")
	check("String wrong type", err, true)
	s, err = a.String("absent")
	check("String absent", err, s != "")
	_, err = a.RequiredString("empty")
	check("RequiredString empty", err, true)
	id, err := a.ID("id")
	check("ID", err, id != "123")
	_, err = a.ID("badid")
	check("ID bad", err, true)
	_, err = a.ID("numid")
	check("ID number", err, true)
	_, err = a.ID("absent")
	check("ID absent", err, true)
	id, err = a.OptionalID("absent")
	check("OptionalID absent", err, id != "")
	n, err := a.Int("num", 1, 1, 10)
	check("Int", err, n != 5)
	n, err = a.Int("absent", 7, 1, 10)
	check("Int default", err, n != 7)
	_, err = a.Int("frac", 1, 1, 10)
	check("Int fractional", err, true)
	_, err = a.Int("big", 1, 1, 100)
	check("Int out of range", err, true)
	b, err := a.Bool("b", false)
	check("Bool", err, !b)
	b, err = a.Bool("absent", true)
	check("Bool default", err, !b)
	o, err := a.Object("obj")
	check("Object", err, o["k"] != "v")
	o, err = a.Object("absent")
	check("Object absent", err, o != nil)
	l, err := a.StringList("list")
	check("StringList", err, len(l) != 2)
	_, err = a.StringList("mixed")
	check("StringList mixed", err, true)
	k, err := a.OneOf("absent", "s")
	check("OneOf", err, k != "s")
	_, err = a.OneOf("s", "id")
	check("OneOf two set", err, true)
	k, err = a.OneOf("absent", "other")
	check("OneOf none", err, k != "")
}

func TestMessagePayload(t *testing.T) {
	data := base64.StdEncoding.EncodeToString([]byte("file body"))
	body, files, err := messagePayload(Args{
		"content":        "hi @everyone",
		"embeds":         []any{map[string]any{"title": "t"}},
		"allow_mentions": false,
		"attachments":    []any{map[string]any{"filename": "a.txt", "content_base64": data, "description": "d"}},
	}, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if body["content"] != "hi @everyone" || len(body["embeds"].([]any)) != 1 {
		t.Fatalf("body = %v", body)
	}
	if !reflect.DeepEqual(body["allowed_mentions"], map[string]any{"parse": []string{}}) {
		t.Fatalf("allowed_mentions = %#v", body["allowed_mentions"])
	}
	if len(files) != 1 || files[0].Name != "a.txt" || string(files[0].Data) != "file body" || files[0].ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("files = %+v", files)
	}
	if !reflect.DeepEqual(body["attachments"], []map[string]any{{"id": 0, "filename": "a.txt", "description": "d"}}) {
		t.Fatalf("attachments = %#v", body["attachments"])
	}

	body, _, err = messagePayload(Args{"content": "hi"}, true, true)
	if err != nil || body["allowed_mentions"] != nil {
		t.Fatalf("allow_mentions default must send nothing: %v %v", body, err)
	}

	for name, args := range map[string]Args{
		"nothing to send":        {},
		"bad base64":             {"content": "x", "attachments": []any{map[string]any{"filename": "a", "content_base64": "!!"}}},
		"path in filename":       {"content": "x", "attachments": []any{map[string]any{"filename": "../a", "content_base64": data}}},
		"too many embeds":        {"embeds": make([]any, 11)},
		"missing content_base64": {"content": "x", "attachments": []any{map[string]any{"filename": "a.txt"}}},
	} {
		if _, _, err := messagePayload(args, true, true); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, _, err := messagePayload(Args{"content": "x", "attachments": []any{}}, true, false); err == nil {
		t.Error("attachments on a tool that does not allow them: want error")
	}
}

func TestRenderMessages(t *testing.T) {
	msgs := []messageJSON{
		{ID: "2", Author: userJSON{ID: "8", Username: "bob"}, Content: "second", Timestamp: time.Date(2026, 9, 14, 11, 3, 0, 0, time.UTC)},
		{ID: "1", Author: userJSON{ID: "7", Username: "alice", GlobalName: "Alice"}, Content: "first",
			Timestamp:   time.Date(2026, 9, 14, 11, 2, 0, 0, time.UTC),
			Attachments: []attachmentJSON{{Filename: "a.png", URL: "https://cdn.discordapp.com/a.png"}}},
	}
	got := renderMessages(msgs)
	want := "[2026-09-14 11:02] Alice (7) #1: first\n  attachment: a.png https://cdn.discordapp.com/a.png\n" +
		"[2026-09-14 11:03] bob (8) #2: second"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if renderMessages(nil) != "No messages." {
		t.Fatal("empty list")
	}
}
