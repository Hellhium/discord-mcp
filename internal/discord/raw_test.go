package discord

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/Hellhium/discord-mcp/internal/discordtest"
)

func TestValidateRawRoute(t *testing.T) {
	tests := []struct {
		route string
		ok    bool
	}{
		{"/users/@me", true},
		{"/guilds/1/emojis", true},
		{"/channels/1/messages/2/reactions/%F0%9F%91%8D/@me", true},
		{"", false},
		{"users/@me", false},
		{"//evil.example/x", false},
		{"/users//me", false},
		{"https://evil.example/x", false},
		{"/x/https://evil.example", false},
		{"/../oauth2", false},
		{"/channels/1/..", false},
		{"/channels/%2e%2e/x", false},
		{"/channels/%2E%2E/x", false},
		{"/channels/1%2F..%2Fx", false},
		{"/channels\\1", false},
		{"/users/@me?x=1", false},
		{"/users/@me#frag", false},
		{"/users/@ me", false},
		{"/users/\x00", false},
		{"/channels/%zz", false},
		{"/api/v10/users/@me", false},
	}
	for _, tt := range tests {
		err := ValidateRawRoute(tt.route)
		if (err == nil) != tt.ok {
			t.Errorf("ValidateRawRoute(%q) = %v, want ok=%v", tt.route, err, tt.ok)
		}
		var ae *ArgError
		if err != nil && !errors.As(err, &ae) {
			t.Errorf("ValidateRawRoute(%q) returned %T, want *ArgError", tt.route, err)
		}
	}
}

func TestRedactRoute(t *testing.T) {
	tests := map[string]string{
		"/users/@me":                                      "/users/@me",
		"/channels/123/messages/456":                      "/channels/{id}/messages/{id}",
		"/webhooks/123/SeCrEt_token":                      "/webhooks/{id}/{token}",
		"/webhooks/123/SeCrEt_token/messages/9":           "/webhooks/{id}/{token}/messages/{id}",
		"/interactions/123/tok/callback":                  "/interactions/{id}/{token}/callback",
		"/webhooks/123":                                   "/webhooks/{id}",
		"/Webhooks/123/SeCrEt_token":                      "/Webhooks/{id}/{token}",
		"/INTERACTIONS/123/tok/callback":                  "/INTERACTIONS/{id}/{token}/callback",
		"/Webhooks/987654321098765432/SECRETWEBHOOKTOKEN": "/Webhooks/{id}/{token}",
	}
	for in, want := range tests {
		if got := RedactRoute(in); got != want {
			t.Errorf("RedactRoute(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDoRawSendsToFixedHost(t *testing.T) {
	fake := discordtest.New(t)
	fake.Handle("PATCH /api/v10/guilds/{id}/emojis/{e}", discordtest.JSON(200, map[string]any{"ok": true}))
	c := newBot(t, fake, time.Second)
	rec := NewRecorder()
	resp, err := c.DoRaw(WithRecorder(context.Background(), rec), "PATCH", "/guilds/1/emojis/2",
		url.Values{"a": {"b"}}, map[string]any{"name": "x"}, "rename")
	if err != nil || resp.Status != 200 {
		t.Fatalf("resp %d err %v", resp.Status, err)
	}
	got := fake.Requests()[0]
	if got.Path != "/api/v10/guilds/1/emojis/2" || got.Query.Get("a") != "b" || got.Header.Get("X-Audit-Log-Reason") != "rename" {
		t.Fatalf("request = %+v", got)
	}
	if calls := rec.Calls(); len(calls) != 1 || calls[0].Route != "/guilds/{id}/emojis/{id}" {
		t.Fatalf("recorder = %+v", calls)
	}
}

func TestDoRawRejectsBeforeSending(t *testing.T) {
	fake := discordtest.New(t)
	c := newBot(t, fake, time.Second)
	for _, tc := range []struct{ method, route string }{
		{"GET", "//evil.example/x"},
		{"TRACE", "/users/@me"},
		{"get", "/users/@me"},
	} {
		_, err := c.DoRaw(context.Background(), tc.method, tc.route, nil, nil, "")
		var ae *ArgError
		if !errors.As(err, &ae) {
			t.Errorf("%s %s: err = %v, want ArgError", tc.method, tc.route, err)
		}
	}
	if n := len(fake.Requests()); n != 0 {
		t.Fatalf("%d request(s) reached Discord", n)
	}
}

func TestDoRawMixedCaseWebhookRedaction(t *testing.T) {
	fake := discordtest.New(t)
	fake.Handle("POST /api/v10/Webhooks/{id}/{token}", discordtest.JSON(204, map[string]any{}))
	c := newBot(t, fake, time.Second)
	rec := NewRecorder()
	_, err := c.DoRaw(WithRecorder(context.Background(), rec), "POST", "/Webhooks/987654321098765432/SECRETWEBHOOKTOKEN",
		nil, map[string]any{"content": "test"}, "")
	if err != nil {
		t.Fatalf("DoRaw failed: %v", err)
	}
	calls := rec.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(calls))
	}
	recorded := calls[0].Route
	// The recorded route should have no plaintext ID or token
	if recorded != "/Webhooks/{id}/{token}" {
		t.Errorf("recorded route = %q, want /Webhooks/{id}/{token}", recorded)
	}
	if u := recorded; u != "/Webhooks/{id}/{token}" {
		t.Errorf("recorded route %q contains secrets or wrong format", u)
	}
}
