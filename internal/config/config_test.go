package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	botToken   = "MTIzNDU2Nzg5MDEyMzQ1Njc4.GAbCdE.abcdefghijklmnopqrstuvwxyz0123"
	webhookURL = "https://discord.com/api/webhooks/123456789012345678/AbC-def_GHI123"
	tokenA     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tokenB     = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

var fullConfig = fmt.Sprintf(`
server:
  listen: ":9090"
direct_auth:
  enabled: true
  cache_ttl: 5m
discord:
  timeout: 20s
instances:
  - name: assistant
    auth:
      tokens: [%q]
    bot:
      token: %q
      events:
        intents: [guilds, guild_messages, message_content]
        buffer_size: 50
  - name: alerts
    auth:
      tokens: [%q]
    webhook:
      url: %q
`, tokenA, botToken, tokenB, webhookURL)

func TestParseFullConfig(t *testing.T) {
	c, err := Parse([]byte(fullConfig))
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Listen != ":9090" || !c.DirectAuth.Enabled ||
		c.DirectAuth.CacheTTL.Duration() != 5*time.Minute || c.Discord.Timeout.Duration() != 20*time.Second {
		t.Fatalf("top-level values not loaded: %+v", c)
	}
	if len(c.Instances) != 2 {
		t.Fatalf("want 2 instances, got %d", len(c.Instances))
	}
	a := c.Instances[0]
	if a.Bot == nil || a.Bot.Token.Reveal() != botToken || a.Bot.Events == nil ||
		*a.Bot.Events.BufferSize != 50 || len(a.Bot.Events.Intents) != 3 || a.Auth.Tokens[0].Reveal() != tokenA {
		t.Fatalf("bot instance not loaded: %+v", a)
	}
	w := c.Instances[1]
	if w.Webhook == nil || w.Webhook.URL.Reveal() != webhookURL || w.Bot != nil {
		t.Fatalf("webhook instance not loaded: %+v", w)
	}
}

func TestParseDefaults(t *testing.T) {
	c, err := Parse([]byte(fmt.Sprintf(`
instances:
  - name: a
    auth: {tokens: [%q]}
    bot:
      token: %q
      events: {intents: [guilds]}
`, tokenA, botToken)))
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Listen != DefaultListen || c.DirectAuth.Enabled ||
		c.DirectAuth.CacheTTL.Duration() != DefaultCacheTTL || c.Discord.Timeout.Duration() != DefaultTimeout ||
		*c.Instances[0].Bot.Events.BufferSize != DefaultBufferSize {
		t.Fatalf("defaults not applied: %+v", c)
	}
}

func TestParseDirectAuthOnlyIsValid(t *testing.T) {
	if _, err := Parse([]byte("direct_auth: {enabled: true}\n")); err != nil {
		t.Fatalf("direct auth without instances must be valid: %v", err)
	}
}

func TestParseErrors(t *testing.T) {
	bot := func(extra string) string {
		return fmt.Sprintf("instances:\n  - name: a\n    auth: {tokens: [%q]}\n    bot:\n      token: %q\n%s", tokenA, botToken, extra)
	}
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"empty file", "", "config is empty"},
		{"unknown key", "server: {listen: ':1', port: 2}\n", "field port not found"},
		{"nothing can authenticate", "server: {listen: ':1'}\n", "no instances"},
		{"negative ttl", "direct_auth: {enabled: true, cache_ttl: -1s}\n", "direct_auth.cache_ttl"},
		{"bad duration", "direct_auth: {enabled: true, cache_ttl: soon}\n", "invalid duration"},
		{"negative timeout", "direct_auth: {enabled: true}\ndiscord: {timeout: -1s}\n", "discord.timeout"},
		{"missing name", fmt.Sprintf("instances:\n  - auth: {tokens: [%q]}\n    bot: {token: %q}\n", tokenA, botToken), "name is required"},
		{"duplicate name", fmt.Sprintf("instances:\n  - name: a\n    auth: {tokens: [%q]}\n    bot: {token: %q}\n  - name: a\n    auth: {tokens: [%q]}\n    bot: {token: %q}\n", tokenA, botToken, tokenB, botToken), `duplicate instance name "a"`},
		{"no tokens", fmt.Sprintf("instances:\n  - name: a\n    auth: {tokens: []}\n    bot: {token: %q}\n", botToken), "auth.tokens must list at least one token"},
		{"empty token", fmt.Sprintf("instances:\n  - name: a\n    auth: {tokens: ['']}\n    bot: {token: %q}\n", botToken), "empty token"},
		{"shared token", fmt.Sprintf("instances:\n  - name: a\n    auth: {tokens: [%q]}\n    bot: {token: %q}\n  - name: b\n    auth: {tokens: [%q]}\n    bot: {token: %q}\n", tokenA, botToken, tokenA, botToken), `also used by instance "a"`},
		{"neither bot nor webhook", fmt.Sprintf("instances:\n  - name: a\n    auth: {tokens: [%q]}\n", tokenA), "exactly one of bot or webhook"},
		{"both bot and webhook", fmt.Sprintf("instances:\n  - name: a\n    auth: {tokens: [%q]}\n    bot: {token: %q}\n    webhook: {url: %q}\n", tokenA, botToken, webhookURL), "exactly one of bot or webhook"},
		{"bad bot token", fmt.Sprintf("instances:\n  - name: a\n    auth: {tokens: [%q]}\n    bot: {token: nope}\n", tokenA), "bot.token"},
		{"bad webhook url", fmt.Sprintf("instances:\n  - name: a\n    auth: {tokens: [%q]}\n    webhook: {url: 'https://evil.example/x'}\n", tokenA), "webhook.url"},
		{"no intents", bot("      events: {intents: []}\n"), "intents must list at least one"},
		{"unknown intent", bot("      events: {intents: [guilds, nope]}\n"), `unknown gateway intent "nope"`},
		{"zero buffer", bot("      events: {intents: [guilds], buffer_size: 0}\n"), "buffer_size must be at least 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestErrorsNeverContainSecrets(t *testing.T) {
	// A token shared between instances must not be echoed in the error.
	y := fmt.Sprintf("instances:\n  - name: a\n    auth: {tokens: [%q]}\n    bot: {token: %q}\n  - name: b\n    auth: {tokens: [%q]}\n    bot: {token: %q}\n", tokenA, botToken, tokenA, botToken)
	_, err := Parse([]byte(y))
	if err == nil || strings.Contains(err.Error(), tokenA) {
		t.Fatalf("error leaks token: %v", err)
	}
}

func TestSecretRedacts(t *testing.T) {
	s := Secret("hunter2")
	for name, got := range map[string]string{
		"String":  s.String(),
		"%v":      fmt.Sprintf("%v", s),
		"%#v":     fmt.Sprintf("%#v", struct{ S Secret }{s}),
		"yaml":    mustYAML(t, struct{ S Secret }{s}),
		"json":    mustJSON(t, struct{ S Secret }{s}),
		"%+v cfg": fmt.Sprintf("%+v", Bot{Token: s}),
	} {
		if strings.Contains(got, "hunter2") {
			t.Errorf("%s leaks the secret: %s", name, got)
		}
	}
	if s.Reveal() != "hunter2" {
		t.Fatal("Reveal must return the plaintext")
	}
	if Secret("").String() != "" {
		t.Fatal("empty secret must render empty")
	}
}

func TestLoadReadsFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(fullConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("want error for missing file")
	}
}

func mustYAML(t *testing.T, v any) string {
	t.Helper()
	b, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
