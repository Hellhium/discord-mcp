package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hellhium/discord-mcp/internal/discord"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
)

const (
	instToken  = "config-token-config-token-config"
	botTok     = "MTIz.GAbC.directdirectdirect"
	webhookID  = "123456789012345678"
	webhookTok = "AbC-def_GHI123"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func setup(t *testing.T, direct bool) (*Resolver, *discordtest.Server, *clock, *Principal) {
	t.Helper()
	fake := discordtest.New(t)
	inst := &Principal{Kind: KindInstance, Capability: CapBot, Instance: "assistant"}
	r, err := NewResolver(
		[]InstanceEntry{{Tokens: []string{instToken}, Principal: inst}},
		DirectOptions{Enabled: direct, CacheTTL: 10 * time.Minute, Discord: discord.Options{Timeout: time.Second, HTTPClient: fake.HTTPClient()}},
	)
	if err != nil {
		t.Fatal(err)
	}
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	r.now = c.now
	return r, fake, c, inst
}

func TestInstanceTokenResolvesWithoutDiscord(t *testing.T) {
	for _, direct := range []bool{false, true} {
		r, fake, _, inst := setup(t, direct)
		p, rej := r.Resolve(context.Background(), instToken)
		if rej != "" || p != inst {
			t.Fatalf("direct=%v: p=%v rej=%s", direct, p, rej)
		}
		if n := len(fake.Requests()); n != 0 {
			t.Fatalf("instance token contacted Discord %d time(s)", n)
		}
	}
}

func TestRejectedWithoutDiscord(t *testing.T) {
	tests := []struct {
		name   string
		direct bool
		cred   string
	}{
		{"unknown token, direct off", false, "nope"},
		{"bot-shaped, direct off", false, botTok},
		{"webhook-shaped, direct off", false, webhookID + "/" + webhookTok},
		{"garbage, direct on", true, "not a credential"},
		{"near-miss config token, direct on", true, instToken + "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, fake, _, _ := setup(t, tt.direct)
			if p, rej := r.Resolve(context.Background(), tt.cred); p != nil || rej != RejectUnknown {
				t.Fatalf("p=%v rej=%s", p, rej)
			}
			if n := len(fake.Requests()); n != 0 {
				t.Fatalf("contacted Discord %d time(s)", n)
			}
		})
	}
}

func TestDirectBotVerifiedAndCached(t *testing.T) {
	r, fake, _, _ := setup(t, true)
	fake.Handle("GET /api/v10/users/@me", discordtest.JSON(200, map[string]any{"id": "42", "username": "b"}))
	p, rej := r.Resolve(context.Background(), botTok)
	if rej != "" || p.Kind != KindDirectBot || p.Capability != CapBot || p.BotUserID != "42" || p.Client == nil {
		t.Fatalf("p=%+v rej=%s", p, rej)
	}
	if !strings.HasPrefix(p.CredentialHash, "sha256:") || len(p.CredentialHash) != len("sha256:")+8 {
		t.Fatalf("hash = %q", p.CredentialHash)
	}
	if a := p.Actor(); a.Kind != "direct_bot" || a.Credential != p.CredentialHash || a.Instance != "" {
		t.Fatalf("actor = %+v", a)
	}
	p2, _ := r.Resolve(context.Background(), botTok)
	if p2 != p || len(fake.Requests()) != 1 {
		t.Fatalf("second resolve not cached: same=%v requests=%d", p2 == p, len(fake.Requests()))
	}
}

func TestDirectCacheExpires(t *testing.T) {
	r, fake, c, _ := setup(t, true)
	fake.Handle("GET /api/v10/users/@me", discordtest.JSON(200, map[string]any{"id": "42"}))
	r.Resolve(context.Background(), botTok)
	c.t = c.t.Add(11 * time.Minute)
	r.Resolve(context.Background(), botTok)
	if n := len(fake.Requests()); n != 2 {
		t.Fatalf("expired entry must be verified again: %d request(s)", n)
	}
}

func TestDirectFailureCache(t *testing.T) {
	r, fake, c, _ := setup(t, true)
	fake.Handle("GET /api/v10/users/@me", discordtest.JSON(401, map[string]any{"message": "401: Unauthorized", "code": 0}))
	if _, rej := r.Resolve(context.Background(), botTok); rej != RejectDiscord {
		t.Fatalf("rej = %s", rej)
	}
	if _, rej := r.Resolve(context.Background(), botTok); rej != RejectRecentFailure {
		t.Fatalf("rej = %s", rej)
	}
	if n := len(fake.Requests()); n != 1 {
		t.Fatalf("failure not cached: %d request(s)", n)
	}
	c.t = c.t.Add(61 * time.Second)
	r.Resolve(context.Background(), botTok)
	if n := len(fake.Requests()); n != 2 {
		t.Fatalf("failure cache must expire after a minute: %d request(s)", n)
	}
}

func TestDirectUnavailableNotCached(t *testing.T) {
	r, fake, _, _ := setup(t, true)
	fake.Handle("GET /api/v10/users/@me", discordtest.JSON(500, map[string]any{"message": "oops"}))
	for i := 0; i < 2; i++ {
		if _, rej := r.Resolve(context.Background(), botTok); rej != RejectUnavailable {
			t.Fatalf("rej = %s", rej)
		}
	}
	if n := len(fake.Requests()); n < 2 {
		t.Fatalf("unavailable must not be cached: %d request(s)", n)
	}
}

func TestDirectWebhookFormsShareCache(t *testing.T) {
	r, fake, _, _ := setup(t, true)
	fake.Handle("GET /api/v10/webhooks/{id}/{token}", discordtest.JSON(200, map[string]any{"id": webhookID}))
	p, rej := r.Resolve(context.Background(), "https://discord.com/api/webhooks/"+webhookID+"/"+webhookTok)
	if rej != "" || p.Kind != KindDirectWebhook || p.Capability != CapWebhook || p.Webhook.ID != webhookID || p.Webhook.Token != webhookTok {
		t.Fatalf("p=%+v rej=%s", p, rej)
	}
	p2, _ := r.Resolve(context.Background(), webhookID+"/"+webhookTok)
	if p2 != p || len(fake.Requests()) != 1 {
		t.Fatalf("URL and id/token forms must share a cache entry: %d request(s)", len(fake.Requests()))
	}
}

func TestDirectWebhookUnknown(t *testing.T) {
	r, fake, _, _ := setup(t, true)
	fake.Handle("GET /api/v10/webhooks/{id}/{token}", discordtest.JSON(404, map[string]any{"message": "Unknown Webhook", "code": 10015}))
	if _, rej := r.Resolve(context.Background(), webhookID+"/"+webhookTok); rej != RejectDiscord {
		t.Fatalf("rej = %s", rej)
	}
}

func TestInvalidateEvicts(t *testing.T) {
	r, fake, _, _ := setup(t, true)
	fake.Handle("GET /api/v10/users/@me", discordtest.JSON(200, map[string]any{"id": "42"}))
	p, _ := r.Resolve(context.Background(), botTok)
	p.Invalidate()
	if _, rej := r.Resolve(context.Background(), botTok); rej != RejectRecentFailure {
		t.Fatalf("invalidated credential: rej = %s", rej)
	}
}

func TestNewResolverRejectsDuplicateTokens(t *testing.T) {
	p := &Principal{}
	_, err := NewResolver([]InstanceEntry{{Tokens: []string{"a"}, Principal: p}, {Tokens: []string{"a"}, Principal: p}}, DirectOptions{})
	if err == nil {
		t.Fatal("want error for duplicate token")
	}
}

func TestCredential(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		value, hdr string
		ok         bool
	}{
		{"bearer", map[string]string{"Authorization": "Bearer abc"}, "abc", HeaderAuthorization, true},
		{"bearer lowercase", map[string]string{"Authorization": "bearer  abc "}, "abc", HeaderAuthorization, true},
		{"api key", map[string]string{"X-API-Key": "abc"}, "abc", HeaderAPIKey, true},
		{"bearer wins", map[string]string{"Authorization": "Bearer one", "X-API-Key": "two"}, "one", HeaderAuthorization, true},
		{"other scheme falls through", map[string]string{"Authorization": "Basic xyz", "X-API-Key": "two"}, "two", HeaderAPIKey, true},
		{"empty bearer falls through", map[string]string{"Authorization": "Bearer ", "X-API-Key": "two"}, "two", HeaderAPIKey, true},
		{"none", map[string]string{}, "", "", false},
		{"empty api key", map[string]string{"X-API-Key": " "}, "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}
			v, h, ok := Credential(r)
			if v != tt.value || h != tt.hdr || ok != tt.ok {
				t.Fatalf("Credential = %q %q %v", v, h, ok)
			}
		})
	}
}

func TestPrincipalContext(t *testing.T) {
	p := &Principal{Kind: KindInstance, Instance: "a"}
	got, ok := FromContext(WithPrincipal(context.Background(), p))
	if !ok || got != p {
		t.Fatal("principal not carried by context")
	}
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("empty context must have no principal")
	}
	if a := p.Actor(); a.Kind != "instance" || a.Instance != "a" || a.Credential != "" {
		t.Fatalf("actor = %+v", a)
	}
}
