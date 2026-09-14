# discord-mcp Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `discord-mcp`, an HTTP MCP server that lets an LLM drive Discord through a bot account or a webhook, with Discord as the only permission system and one audit log line per action.

**Architecture:** One Go process serves `GET /healthz` and `POST /mcp` (mcp-go Streamable HTTP, stateless). Each request's credential is resolved into a `Principal` (config instance, direct bot token or direct webhook) that carries its own Discord REST client; the router hands the request to one of three MCP servers built at startup (bot, bot+events, webhook), whose tool handlers read the principal from the request context. Every tool handler is wrapped by one function that classifies the outcome and writes the audit line. Configured bots may open a Gateway session feeding an in-memory event buffer.

**Tech Stack:** Go 1.26, `github.com/mark3labs/mcp-go` v0.57.0, `github.com/bwmarrin/discordgo` v0.29.0, `gopkg.in/yaml.v3`, `log/slog`, `net/http/httptest`.

**Spec:** `docs/superpowers/specs/2026-09-14-discord-mcp-design.md` — read it before starting any task; this plan argues from it.

## Global Constraints

- Module path: `github.com/Hellhium/discord-mcp`; `go` directive `1.26.5` (the toolchain installed locally).
- Binary name `discord-mcp`; entrypoint `./cmd/server`; all logic under `internal/`.
- Discord REST base URL is always `https://discord.com/api/v10` (constant `discord.APIBase`). No code path takes a host from a tool argument or a client header.
- MCP over Streamable HTTP at `POST /mcp`, `server.WithStateLess(true)`; `GET /healthz` is the only unauthenticated path. No stdio transport.
- Credentials (config tokens, bot tokens, webhook tokens, webhook URLs) never appear in logs, error messages, tool descriptions or MCP instructions. Config credentials use `config.Secret`.
- All tool names start with `discord_`. IDs are snowflake strings checked with `credential.IsSnowflake` before any Discord call.
- Audit log: `log/slog` JSON handler on stdout, one `action` line per tool call; outcomes exactly `ok`, `invalid_args`, `discord_error`, `rate_limited`, `credential_rejected`; `INFO` for `ok`, `WARN` otherwise.
- Defaults: `server.listen` `:8080`, `direct_auth.enabled` `false`, `direct_auth.cache_ttl` `10m`, `discord.timeout` `15s`, `events.buffer_size` `1000`; direct-auth failure cache 1 minute.
- `wait_for_message` timeout default 60s, max 300s. `read_messages` limit ≤ 100; `poll_events` limit ≤ 100; `search_messages` limit ≤ 25.
- `discord_request` response truncated past 64 KB; audit copy of its body truncated at 16 KB.
- `allow_mentions` defaults to `true` (no `allowed_mentions` sent); `false` sends `{"parse": []}`.
- Attachments are base64 only; the server never fetches a URL on a tool's behalf.
- Tests are table-driven where there is more than one case, use no network (fake Discord via `internal/discordtest`), and live next to the code.
- Commits follow Conventional Commits (see `CLAUDE.md`); never add a co-author or tool-attribution trailer.
- The Claude Code Stop hook runs `go vet ./...` and `go test ./...`; every task must leave both green.

## Spec amendment made while planning

The spec deferred message search until the endpoint was verified. It is verified: `GET /guilds/{guild.id}/messages/search` is documented (needs `READ_MESSAGE_HISTORY`; results are limited by the `MESSAGE_CONTENT` intent; `limit` 1–25; `offset` ≤ 9975; answers `202` with `retry_after` while the guild is being indexed). This plan adds `discord_search_messages` (Task 11) and the spec is updated in the same commit as this plan.

Pins use the current routes `PUT|DELETE /channels/{channel.id}/messages/pins/{message.id}` (the `/channels/{id}/pins/...` routes are deprecated).

## File Structure

| Path | Responsibility |
|---|---|
| `go.mod`, `go.sum`, `Makefile` | Module and build targets (Task 1) |
| `internal/credential/credential.go` | Shapes: snowflake, bot token, webhook URL / `id/token` (Task 1) |
| `internal/intents/intents.go` | Gateway intent names → bitmask; privileged intents vs. application flags (Task 1) |
| `internal/config/config.go` | YAML types, `Secret`, `Duration`, defaults, `Load`/`Parse` (Task 2) |
| `internal/config/validate.go` | Startup validation (Task 2) |
| `internal/discordtest/discordtest.go` | Fake Discord REST server + host-rewriting `http.Client` (Task 3) |
| `internal/discord/client.go` | `Client`, `Call`, `Do`, body encoding, rate-limit handling (Task 3) |
| `internal/discord/route.go` | Route template expansion, bucket keys (Task 3); raw route guard and redaction (Task 4) |
| `internal/discord/errors.go` | `ArgError`, `APIError`, `RateLimitedError`, `UnavailableError` (Task 3) |
| `internal/discord/recorder.go` | Per-request record of Discord calls for the audit log (Task 3) |
| `internal/discord/verify.go` | `VerifyBot`, `VerifyWebhook`, `CheckPrivilegedIntents` (Task 4) |
| `internal/audit/audit.go` | JSON audit logger: `Action`, `AuthRejected`, `Gateway` (Task 5) |
| `internal/audit/sanitize.go` | Argument sanitising: attachments, body truncation, route redaction (Task 5) |
| `internal/events/buffer.go` | Ring buffer, cursors, `Since`, `Wait`, `Close` (Task 6) |
| `internal/events/gateway.go` | discordgo Gateway session → buffer; lifecycle logging (Task 7) |
| `internal/auth/principal.go` | `Principal`, `Capability`, context helpers (Task 8) |
| `internal/auth/resolver.go` | Credential extraction and resolution, success/failure caches (Task 8) |
| `internal/tools/tools.go` | `Tool`, `Handler`, `Register`, the audit/outcome wrapper (Task 9) |
| `internal/tools/args.go` | Typed argument accessors returning `*discord.ArgError` (Task 9) |
| `internal/tools/common.go` | `format`, `allow_mentions`, attachments, embeds, `reason` helpers (Task 9) |
| `internal/tools/render.go` | Text renderers for messages, channels, guilds, members, roles, events (Task 9) |
| `internal/tools/discovery.go` | `get_me`, `list_guilds`, `get_guild`, `list_channels`, `get_channel` (Task 10) |
| `internal/tools/messages.go` | Message, reaction, pin, search and DM tools (Task 11) |
| `internal/tools/channels.go` | Thread and channel tools (Task 12) |
| `internal/tools/members.go` | Member, role and moderation tools (Task 13) |
| `internal/tools/request.go` | `discord_request` (Task 14) |
| `internal/tools/events.go` | `poll_events`, `wait_for_message` (Task 15) |
| `internal/tools/webhook.go` | Webhook tools (Task 16) |
| `internal/tools/sets.go` | `BotTools`, `EventTools`, `WebhookTools`, instructions per capability (Tasks 10–16 append; Task 17 finalises) |
| `internal/router/router.go` | `/healthz`, `/mcp`, auth → principal → MCP server (Task 17) |
| `internal/app/app.go` | Builds everything from a `*config.Config`; owns gateways; `Close` (Task 17) |
| `internal/e2e/e2e_test.go` | Full stack over HTTP against the fake Discord (Task 17) |
| `cmd/server/main.go` | Flags, load, build, serve, graceful shutdown (Task 18) |
| `config.example.yaml`, `README.md` | Example config and Getting started (Task 18) |
| `Dockerfile`, `.dockerignore`, `.github/workflows/docker.yml` | Image and CI (Task 19) |

## Dependency order

```
credential, intents ─► config
credential ─► discord ─► audit ─► events ─► auth ─► tools ─► router, app ─► cmd/server
discordtest (test helper, used from Task 3 on)
```

---
### Task 1: Module, Makefile, credential shapes and intent names

**Files:**
- Create: `go.mod`, `Makefile`
- Create: `internal/credential/credential.go`, `internal/credential/credential_test.go`
- Create: `internal/intents/intents.go`, `internal/intents/intents_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `credential.Webhook{ID, Token string}`
  - `credential.IsSnowflake(s string) bool`, `credential.IsBotToken(s string) bool`, `credential.IsWebhookToken(s string) bool`
  - `credential.ParseWebhook(s string) (credential.Webhook, bool)`
  - `intents.Mask` (`uint64`), `intents.Parse(names []string) (intents.Mask, error)`
  - `intents.MissingPrivileged(m intents.Mask, appFlags uint64) []string`

- [ ] **Step 1: Create the module and Makefile**

```bash
go mod init github.com/Hellhium/discord-mcp
go mod edit -go=1.26.5
```

`Makefile`:

```make
.PHONY: all build test vet fmt fmt-check tidy run clean

BINARY := discord-mcp
CONFIG  ?= config.yaml

all: fmt-check vet test build

build:
	go build -o $(BINARY) ./cmd/server

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi

tidy:
	go mod tidy

run: build
	./$(BINARY) -config $(CONFIG)

clean:
	rm -f $(BINARY)
```

(Recipe lines must be indented with a tab.)

- [ ] **Step 2: Write the failing credential tests**

`internal/credential/credential_test.go`:

```go
package credential

import "testing"

func TestIsSnowflake(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"1", true},
		{"123456789012345678", true},
		{"12345678901234567890", true},
		{"123456789012345678901", false}, // 21 digits
		{"0123", false},
		{"", false},
		{"12a", false},
		{"-1", false},
		{"1 2", false},
		{"@me", false},
	}
	for _, tt := range tests {
		if got := IsSnowflake(tt.in); got != tt.want {
			t.Errorf("IsSnowflake(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestIsBotToken(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"MTIzNDU2Nzg5MDEyMzQ1Njc4.GAbCdE.abcdefghijklmnopqrstuvwxyz0123456789_-", true},
		{"a.b.c", true},
		{"a.b", false},
		{"a..c", false},
		{".b.c", false},
		{"a.b.c.d", false},
		{"a.b.c=", false},
		{"a b.c.d", false},
		{"", false},
		{"123/abc", false},
	}
	for _, tt := range tests {
		if got := IsBotToken(tt.in); got != tt.want {
			t.Errorf("IsBotToken(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseWebhook(t *testing.T) {
	const id, tok = "123456789012345678", "AbC-def_GHI123"
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"short form", id + "/" + tok, true},
		{"url discord.com", "https://discord.com/api/webhooks/" + id + "/" + tok, true},
		{"url with version", "https://discord.com/api/v10/webhooks/" + id + "/" + tok, true},
		{"url trailing slash", "https://discord.com/api/webhooks/" + id + "/" + tok + "/", true},
		{"url discordapp.com", "https://discordapp.com/api/webhooks/" + id + "/" + tok, true},
		{"url ptb", "https://ptb.discord.com/api/webhooks/" + id + "/" + tok, true},
		{"url canary", "https://canary.discord.com/api/webhooks/" + id + "/" + tok, true},
		{"url host case", "https://Discord.com/api/webhooks/" + id + "/" + tok, true},
		{"url with query ignored", "https://discord.com/api/webhooks/" + id + "/" + tok + "?wait=true", true},
		{"http scheme", "http://discord.com/api/webhooks/" + id + "/" + tok, false},
		{"other host", "https://evil.example/api/webhooks/" + id + "/" + tok, false},
		{"lookalike host", "https://discord.com.evil.example/api/webhooks/" + id + "/" + tok, false},
		{"host with port", "https://discord.com:8443/api/webhooks/" + id + "/" + tok, false},
		{"userinfo", "https://x@discord.com/api/webhooks/" + id + "/" + tok, false},
		{"extra path", "https://discord.com/api/webhooks/" + id + "/" + tok + "/messages/1", false},
		{"bad id", "abc/" + tok, false},
		{"empty token", id + "/", false},
		{"token with slash", id + "/" + tok + "/x", false},
		{"token with dot", id + "/a.b", false},
		{"no slash", id, false},
		{"empty", "", false},
		{"bot token", "a.b.c", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseWebhook(tt.in)
			if ok != tt.ok {
				t.Fatalf("ParseWebhook(%q) ok = %v, want %v", tt.in, ok, tt.ok)
			}
			if ok && (got.ID != id || got.Token != tok) {
				t.Fatalf("ParseWebhook(%q) = %+v, want id %s token %s", tt.in, got, id, tok)
			}
		})
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/credential/`
Expected: FAIL — `undefined: IsSnowflake` (and the other functions).

- [ ] **Step 4: Implement the credential package**

`internal/credential/credential.go`:

```go
// Package credential recognises the shape of Discord credentials and IDs
// without contacting Discord. It is a leaf package so the config validator,
// the auth resolver and tool argument checks share one definition of what a
// snowflake, a bot token and a webhook look like.
//
// Shape is not validity. A value that passes here may still be rejected by
// Discord; a value that fails here is never sent to Discord at all, which keeps
// garbage from counting against Discord's invalid-request limit.
package credential

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	snowflakeRe    = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	botTokenRe     = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)
	webhookTokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	webhookPathRe  = regexp.MustCompile(`^/api(?:/v[0-9]+)?/webhooks/([^/]+)/([^/]+)/?$`)
)

// webhookHosts are the hosts Discord serves webhook URLs from. The port is part
// of url.URL.Host, so "discord.com:8443" does not match.
var webhookHosts = map[string]bool{
	"discord.com":        true,
	"discordapp.com":     true,
	"ptb.discord.com":    true,
	"canary.discord.com": true,
}

// Webhook identifies a Discord webhook. Token is a credential: anyone holding
// ID and Token can post as the webhook.
type Webhook struct {
	ID    string
	Token string
}

// IsSnowflake reports whether s looks like a Discord ID: 1 to 20 decimal
// digits, no leading zero.
func IsSnowflake(s string) bool { return snowflakeRe.MatchString(s) }

// IsBotToken reports whether s looks like a Discord bot token: three non-empty
// base64url segments separated by dots.
func IsBotToken(s string) bool { return botTokenRe.MatchString(s) }

// IsWebhookToken reports whether s looks like a webhook token.
func IsWebhookToken(s string) bool { return webhookTokenRe.MatchString(s) }

// ParseWebhook accepts a full webhook URL on a Discord host, or the short
// "<id>/<token>" form. A query string on a URL is ignored.
func ParseWebhook(s string) (Webhook, bool) {
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil || u.Scheme != "https" || u.User != nil || !webhookHosts[strings.ToLower(u.Host)] {
			return Webhook{}, false
		}
		m := webhookPathRe.FindStringSubmatch(u.Path)
		if m == nil {
			return Webhook{}, false
		}
		return webhook(m[1], m[2])
	}
	id, token, ok := strings.Cut(s, "/")
	if !ok {
		return Webhook{}, false
	}
	return webhook(id, token)
}

func webhook(id, token string) (Webhook, bool) {
	if !IsSnowflake(id) || !IsWebhookToken(token) {
		return Webhook{}, false
	}
	return Webhook{ID: id, Token: token}, true
}
```

- [ ] **Step 5: Run the credential tests**

Run: `go test ./internal/credential/`
Expected: PASS.

- [ ] **Step 6: Write the failing intents tests**

`internal/intents/intents_test.go`:

```go
package intents

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	m, err := Parse([]string{"guilds", "guild_messages", "message_content", "guild_messages"})
	if err != nil {
		t.Fatal(err)
	}
	if want := Mask(1<<0 | 1<<9 | 1<<15); m != want {
		t.Fatalf("Parse = %b, want %b", m, want)
	}
}

func TestParseUnknown(t *testing.T) {
	_, err := Parse([]string{"guilds", "guild_mesages"})
	if err == nil || !strings.Contains(err.Error(), `"guild_mesages"`) {
		t.Fatalf("want error naming the unknown intent, got %v", err)
	}
}

func TestMissingPrivileged(t *testing.T) {
	all := Mask(1<<1 | 1<<8 | 1<<15 | 1<<9)
	tests := []struct {
		name  string
		flags uint64
		want  []string
	}{
		{"none enabled", 0, []string{"guild_members", "guild_presences", "message_content"}},
		{"limited flags count", 1<<13 | 1<<15 | 1<<19, nil},
		{"full flags count", 1<<12 | 1<<14 | 1<<18, nil},
		{"only content", 1 << 18, []string{"guild_members", "guild_presences"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MissingPrivileged(all, tt.flags); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("MissingPrivileged = %v, want %v", got, tt.want)
			}
		})
	}
	if got := MissingPrivileged(Mask(1<<9), 0); got != nil {
		t.Fatalf("non-privileged mask: got %v, want nil", got)
	}
}
```

- [ ] **Step 7: Run to verify failure**

Run: `go test ./internal/intents/`
Expected: FAIL — `undefined: Parse`.

- [ ] **Step 8: Implement the intents package**

`internal/intents/intents.go`:

```go
// Package intents maps the Gateway intent names used in the YAML config to
// Discord's intent bitmask, and knows which intents are privileged. It has no
// Discord client dependency so config validation can use it.
package intents

import "fmt"

// Mask is a Gateway intents bitmask, as sent in the Identify payload.
type Mask uint64

// byName uses Discord's documented intent names in snake_case.
var byName = map[string]Mask{
	"guilds":                        1 << 0,
	"guild_members":                 1 << 1,
	"guild_moderation":              1 << 2,
	"guild_expressions":             1 << 3,
	"guild_integrations":            1 << 4,
	"guild_webhooks":                1 << 5,
	"guild_invites":                 1 << 6,
	"guild_voice_states":            1 << 7,
	"guild_presences":               1 << 8,
	"guild_messages":                1 << 9,
	"guild_message_reactions":       1 << 10,
	"guild_message_typing":          1 << 11,
	"direct_messages":               1 << 12,
	"direct_message_reactions":      1 << 13,
	"direct_message_typing":         1 << 14,
	"message_content":               1 << 15,
	"guild_scheduled_events":        1 << 16,
	"auto_moderation_configuration": 1 << 20,
	"auto_moderation_execution":     1 << 21,
	"guild_message_polls":           1 << 24,
	"direct_message_polls":          1 << 25,
}

// Parse ORs the named intents together. An unknown name is an error that
// names it.
func Parse(names []string) (Mask, error) {
	var m Mask
	for _, n := range names {
		bit, ok := byName[n]
		if !ok {
			return 0, fmt.Errorf("unknown gateway intent %q", n)
		}
		m |= bit
	}
	return m, nil
}

// privileged lists, in a stable order, each privileged intent and the
// application flags that enable it: Discord sets the full flag for verified
// bots in 100+ servers and the _LIMITED flag below that, and either suffices.
var privileged = []struct {
	name   string
	intent Mask
	flags  uint64
}{
	{"guild_members", 1 << 1, 1<<14 | 1<<15},
	{"guild_presences", 1 << 8, 1<<12 | 1<<13},
	{"message_content", 1 << 15, 1<<18 | 1<<19},
}

// MissingPrivileged returns the privileged intents requested in m that the
// application flags do not enable, or nil when there are none. Opening a
// Gateway with such an intent fails with close code 4014; checking first lets
// startup name the intent instead.
func MissingPrivileged(m Mask, appFlags uint64) []string {
	var missing []string
	for _, p := range privileged {
		if m&p.intent != 0 && appFlags&p.flags == 0 {
			missing = append(missing, p.name)
		}
	}
	return missing
}
```

- [ ] **Step 9: Run all tests and vet**

Run: `go test ./... && go vet ./...`
Expected: PASS for `credential` and `intents`.

- [ ] **Step 10: Commit**

```bash
git add go.mod Makefile internal/credential internal/intents
git commit -m "feat(credential): recognise discord ids, bot tokens, webhooks and intents" -m "Add the Go module and Makefile, and two leaf packages every later layer
shares. credential decides by shape alone whether a value is a snowflake,
a bot token or a webhook (full URL on a Discord host, or id/token), so
malformed input is rejected before anything reaches Discord. intents maps
config intent names to the Gateway bitmask and reports privileged intents
the application flags do not enable."
```

---
### Task 2: Config loading and validation

**Files:**
- Create: `internal/config/config.go`, `internal/config/validate.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `credential.IsBotToken`, `credential.ParseWebhook`, `intents.Parse` (Task 1).
- Produces:
  - `config.Secret` (`string`) with `Reveal() string`, `String()`, `GoString()`, `MarshalYAML()`, `MarshalJSON()`
  - `config.Duration` (`time.Duration`) with `UnmarshalYAML`, `Duration() time.Duration`
  - `config.Config{Server Server; DirectAuth DirectAuth; Discord Discord; Instances []Instance}`
  - `config.Server{Listen string}`, `config.DirectAuth{Enabled bool; CacheTTL Duration}`, `config.Discord{Timeout Duration}`
  - `config.Instance{Name string; Auth InstanceAuth; Bot *Bot; Webhook *Webhook}`, `config.InstanceAuth{Tokens []Secret}`
  - `config.Bot{Token Secret; Events *Events}`, `config.Events{Intents []string; BufferSize *int}`, `config.Webhook{URL Secret}`
  - `config.Load(path string) (*Config, error)`, `config.Parse(b []byte) (*Config, error)`
  - Constants `DefaultListen`, `DefaultCacheTTL`, `DefaultTimeout`, `DefaultBufferSize`
  - After `Parse`, every default is applied: `Listen != ""`, `CacheTTL > 0`, `Timeout > 0`, every `Events.BufferSize != nil`.

- [ ] **Step 1: Write the failing tests**

`internal/config/config_test.go`:

```go
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
		"String":   s.String(),
		"%v":       fmt.Sprintf("%v", s),
		"%#v":      fmt.Sprintf("%#v", struct{ S Secret }{s}),
		"yaml":     mustYAML(t, struct{ S Secret }{s}),
		"json":     mustJSON(t, struct{ S Secret }{s}),
		"%+v cfg":  fmt.Sprintf("%+v", Bot{Token: s}),
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go get gopkg.in/yaml.v3 && go test ./internal/config/`
Expected: FAIL — `undefined: Parse`.

- [ ] **Step 3: Implement the config types and loading**

`internal/config/config.go`:

```go
// Package config loads the single YAML file that drives discord-mcp: the
// listen address, direct authentication, Discord client settings, and the
// instances — each one a set of client tokens mapped to exactly one bot or one
// webhook.
//
// The file is parsed once at startup. Unknown keys are errors, defaults are
// applied, and validation runs before anything is built, so everything
// downstream receives a complete, valid value and never re-checks it.
//
// Every credential in the file is a Secret, which redacts itself in every
// rendering; call Reveal at the exact point the plaintext is needed.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Defaults applied by Parse to absent (or zero) values.
const (
	DefaultListen     = ":8080"
	DefaultCacheTTL   = 10 * time.Minute
	DefaultTimeout    = 15 * time.Second
	DefaultBufferSize = 1000
)

// Duration is a time.Duration that unmarshals from a YAML string like "30s".
type Duration time.Duration

// UnmarshalYAML parses a Go duration string.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("duration must be a string like \"30s\": %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Duration returns the value as a time.Duration.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// Secret is a credential read from the config. Every rendering method redacts
// it, so an accidental %v of a config struct, a log line or a re-serialised
// config cannot leak it.
type Secret string

const redacted = "[redacted]"

// Reveal returns the plaintext. This is the only way to read a Secret.
func (s Secret) Reveal() string { return string(s) }

// String renders "[redacted]", or "" when unset.
func (s Secret) String() string {
	if s == "" {
		return ""
	}
	return redacted
}

// GoString redacts under %#v.
func (s Secret) GoString() string { return `"` + s.String() + `"` }

// MarshalYAML redacts when a config struct is re-serialised.
func (s Secret) MarshalYAML() (any, error) { return s.String(), nil }

// MarshalJSON redacts when a config struct is serialised to JSON.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + s.String() + `"`), nil }

// Config is the whole file.
type Config struct {
	Server     Server     `yaml:"server"`
	DirectAuth DirectAuth `yaml:"direct_auth"`
	Discord    Discord    `yaml:"discord"`
	Instances  []Instance `yaml:"instances"`
}

// Server holds listener settings.
type Server struct {
	Listen string `yaml:"listen"`
}

// DirectAuth controls accepting a Discord bot token or webhook in place of a
// config token.
type DirectAuth struct {
	Enabled  bool     `yaml:"enabled"`
	CacheTTL Duration `yaml:"cache_ttl"`
}

// Discord holds REST client settings shared by every principal.
type Discord struct {
	Timeout Duration `yaml:"timeout"`
}

// Instance maps client tokens to exactly one bot or one webhook.
type Instance struct {
	Name    string       `yaml:"name"`
	Auth    InstanceAuth `yaml:"auth"`
	Bot     *Bot         `yaml:"bot"`
	Webhook *Webhook     `yaml:"webhook"`
}

// InstanceAuth lists the tokens clients present (Bearer or X-API-Key).
type InstanceAuth struct {
	Tokens []Secret `yaml:"tokens"`
}

// Bot is a bot account. Events, when set, opens a Gateway session.
type Bot struct {
	Token  Secret  `yaml:"token"`
	Events *Events `yaml:"events"`
}

// Events configures the Gateway session and its buffer. BufferSize is a
// pointer so an explicit 0 is rejected rather than silently defaulted.
type Events struct {
	Intents    []string `yaml:"intents"`
	BufferSize *int     `yaml:"buffer_size"`
}

// Webhook is a webhook. The URL embeds the webhook token, so it is a Secret.
type Webhook struct {
	URL Secret `yaml:"url"`
}

// Load reads and parses the config file at path.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(b)
}

// Parse decodes, applies defaults and validates a config document.
func Parse(b []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("config is empty")
		}
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := c.validateDurations(); err != nil {
		return nil, err
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Listen == "" {
		c.Server.Listen = DefaultListen
	}
	if c.DirectAuth.CacheTTL == 0 {
		c.DirectAuth.CacheTTL = Duration(DefaultCacheTTL)
	}
	if c.Discord.Timeout == 0 {
		c.Discord.Timeout = Duration(DefaultTimeout)
	}
	for i := range c.Instances {
		if b := c.Instances[i].Bot; b != nil && b.Events != nil && b.Events.BufferSize == nil {
			size := DefaultBufferSize
			b.Events.BufferSize = &size
		}
	}
}
```

- [ ] **Step 4: Implement validation**

`internal/config/validate.go`:

```go
package config

import (
	"errors"
	"fmt"

	"github.com/Hellhium/discord-mcp/internal/credential"
	"github.com/Hellhium/discord-mcp/internal/intents"
)

func (c *Config) validateDurations() error {
	if c.DirectAuth.CacheTTL < 0 {
		return errors.New("direct_auth.cache_ttl must be positive")
	}
	if c.Discord.Timeout < 0 {
		return errors.New("discord.timeout must be positive")
	}
	return nil
}

// validate checks everything that can be checked without Discord. Errors name
// the instance and the key, never a credential value.
func (c *Config) validate() error {
	if len(c.Instances) == 0 && !c.DirectAuth.Enabled {
		return errors.New("no instances configured and direct_auth is disabled: no client could authenticate")
	}
	names := make(map[string]bool)
	owners := make(map[string]string) // token plaintext -> instance name; local to this call
	for i, in := range c.Instances {
		if in.Name == "" {
			return fmt.Errorf("instances[%d]: name is required", i)
		}
		if names[in.Name] {
			return fmt.Errorf("instances[%d]: duplicate instance name %q", i, in.Name)
		}
		names[in.Name] = true
		where := fmt.Sprintf("instance %q", in.Name)

		if len(in.Auth.Tokens) == 0 {
			return fmt.Errorf("%s: auth.tokens must list at least one token", where)
		}
		for _, tok := range in.Auth.Tokens {
			if tok == "" {
				return fmt.Errorf("%s: auth.tokens contains an empty token", where)
			}
			if prev, ok := owners[tok.Reveal()]; ok {
				return fmt.Errorf("%s: an auth token is also used by instance %q", where, prev)
			}
			owners[tok.Reveal()] = in.Name
		}

		if (in.Bot == nil) == (in.Webhook == nil) {
			return fmt.Errorf("%s: exactly one of bot or webhook is required", where)
		}
		if in.Bot != nil {
			if !credential.IsBotToken(in.Bot.Token.Reveal()) {
				return fmt.Errorf("%s: bot.token is missing or is not a Discord bot token", where)
			}
			if ev := in.Bot.Events; ev != nil {
				if len(ev.Intents) == 0 {
					return fmt.Errorf("%s: bot.events.intents must list at least one intent", where)
				}
				if _, err := intents.Parse(ev.Intents); err != nil {
					return fmt.Errorf("%s: bot.events: %w", where, err)
				}
				if *ev.BufferSize < 1 {
					return fmt.Errorf("%s: bot.events.buffer_size must be at least 1", where)
				}
			}
			continue
		}
		if _, ok := credential.ParseWebhook(in.Webhook.URL.Reveal()); !ok {
			return fmt.Errorf("%s: webhook.url is missing or is not a Discord webhook URL", where)
		}
	}
	return nil
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/config/`
Expected: PASS. If `unknown key` fails on wording, print the error: yaml.v3 reports `field port not found in type config.Server`.

- [ ] **Step 6: Commit**

```bash
go mod tidy
git add go.mod go.sum internal/config
git commit -m "feat(config): load and validate the yaml config" -m "Parse the single config file into typed structs: listen address, direct
auth and its cache TTL, the Discord timeout, and instances mapping client
tokens to exactly one bot or webhook, with optional Gateway events.
Unknown keys, duplicate names or tokens, malformed bot tokens or webhook
URLs, unknown intents and a buffer below one entry all refuse to start.
Credentials are Secrets that redact themselves in every rendering, and
no validation error echoes one."
```

---
### Task 3: Fake Discord and the REST client core

**Files:**
- Create: `internal/discordtest/discordtest.go`
- Create: `internal/discord/client.go`, `internal/discord/route.go`, `internal/discord/errors.go`, `internal/discord/recorder.go`
- Test: `internal/discord/client_test.go`

**Interfaces:**
- Consumes: `credential.IsSnowflake`, `credential.IsWebhookToken` (Task 1).
- Produces:
  - `discordtest.New(t testing.TB) *discordtest.Server`
  - `(*discordtest.Server).Handle(pattern string, h http.HandlerFunc)` — Go 1.22 `ServeMux` pattern including the `/api/v10` prefix, e.g. `"POST /api/v10/channels/{channel_id}/messages"`
  - `(*discordtest.Server).HTTPClient() *http.Client` — rewrites `https://discord.com` to the fake; refuses any other host
  - `(*discordtest.Server).Requests() []discordtest.Request`, `discordtest.Request{Method, Path, RawPath string; Query url.Values; Header http.Header; Body []byte}` (`RawPath` is the escaped path)
  - `discordtest.JSON(status int, v any) http.HandlerFunc`
  - `discord.APIBase = "https://discord.com/api/v10"`
  - `discord.Options{Timeout time.Duration; HTTPClient *http.Client}`
  - `discord.NewBot(token string, opts Options) *Client`, `discord.NewWebhook(opts Options) *Client`, `(*Client).Session() *discordgo.Session`
  - `discord.Call{Method, Route string; Params map[string]string; Query url.Values; Body any; Files []File; Reason string}`
  - `discord.File{Name, ContentType string; Data []byte}`
  - `discord.Response{Status int; Body json.RawMessage}`
  - `(*Client).Do(ctx context.Context, call Call) (Response, error)` — a `202` is returned as a `Response` with a nil error
  - Errors: `*discord.ArgError{Msg}`, `*discord.APIError{Status, Code int; Message string}`, `*discord.RateLimitedError{RetryAfter time.Duration}`, `*discord.UnavailableError{Cause string}`; `discord.IsUnauthorized(err error) bool`
  - `discord.CallRecord{Method, Route string; Status int; RateLimitedMS int64}` (JSON tags `method`, `route`, `status`, `rate_limited_ms`)
  - `discord.NewRecorder() *Recorder`, `discord.WithRecorder(ctx, *Recorder) context.Context`, `(*Recorder).Calls() []CallRecord`
  - Route parameter rules: `emoji` → `url.PathEscape`; `webhook_token` → `credential.IsWebhookToken`; any other name ending in `_id` → `credential.IsSnowflake`; anything else is an internal error.

- [ ] **Step 1: Write the fake Discord helper**

`internal/discordtest/discordtest.go`:

```go
// Package discordtest is a fake Discord REST API for tests. It records every
// request and serves handlers registered with Go ServeMux patterns. Its
// HTTPClient rewrites requests for https://discord.com to the fake and refuses
// every other host, so a test also proves nothing was sent elsewhere.
package discordtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

// Request is one request the fake received.
type Request struct {
	Method  string
	Path    string // decoded, e.g. /api/v10/channels/1/messages
	RawPath string // escaped, as sent on the wire
	Query   url.Values
	Header  http.Header
	Body    []byte
}

// Server is the fake. Unmatched requests get Discord's 404 shape.
type Server struct {
	srv  *httptest.Server
	mux  *http.ServeMux
	mu   sync.Mutex
	reqs []Request
}

// New starts a fake closed at the end of the test.
func New(t testing.TB) *Server {
	s := &Server{mux: http.NewServeMux()}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.reqs = append(s.reqs, Request{
		Method:  r.Method,
		Path:    r.URL.Path,
		RawPath: r.URL.EscapedPath(),
		Query:   r.URL.Query(),
		Header:  r.Header.Clone(),
		Body:    body,
	})
	s.mu.Unlock()
	r.Body = io.NopCloser(bytes.NewReader(body))
	if _, pattern := s.mux.Handler(r); pattern == "" {
		JSON(http.StatusNotFound, map[string]any{"message": "404: Not Found", "code": 0})(w, r)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// Handle registers h for a ServeMux pattern such as
// "GET /api/v10/users/@me".
func (s *Server) Handle(pattern string, h http.HandlerFunc) { s.mux.HandleFunc(pattern, h) }

// Requests returns a copy of every request received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.reqs...)
}

// HTTPClient returns a client that sends discord.com traffic to the fake.
func (s *Server) HTTPClient() *http.Client {
	target, _ := url.Parse(s.srv.URL)
	return &http.Client{Transport: rewrite{target: target, base: s.srv.Client().Transport}}
}

type rewrite struct {
	target *url.URL
	base   http.RoundTripper
}

func (rw rewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "discord.com" {
		return nil, fmt.Errorf("discordtest: refusing request to host %q", r.URL.Host)
	}
	r2 := r.Clone(r.Context())
	r2.URL.Scheme = rw.target.Scheme
	r2.URL.Host = rw.target.Host
	r2.Host = rw.target.Host
	return rw.base.RoundTrip(r2)
}

// JSON answers with status and v encoded as JSON (no body for 204).
func JSON(status int, v any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if status == http.StatusNoContent {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
}
```

- [ ] **Step 2: Write the failing client tests**

`internal/discord/client_test.go`:

```go
package discord

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hellhium/discord-mcp/internal/discordtest"
)

const testBotToken = "MTIz.GAbC.secretsecretsecret"

func newBot(t *testing.T, fake *discordtest.Server, timeout time.Duration) *Client {
	t.Helper()
	return NewBot(testBotToken, Options{Timeout: timeout, HTTPClient: fake.HTTPClient()})
}

func TestDoSendsJSONWithBotAuthAndReason(t *testing.T) {
	fake := discordtest.New(t)
	fake.Handle("POST /api/v10/channels/{channel_id}/messages", discordtest.JSON(200, map[string]any{"id": "9"}))
	c := newBot(t, fake, time.Second)

	rec := NewRecorder()
	resp, err := c.Do(WithRecorder(context.Background(), rec), Call{
		Method: http.MethodPost,
		Route:  "/channels/{channel_id}/messages",
		Params: map[string]string{"channel_id": "123"},
		Query:  url.Values{"x": {"1"}},
		Body:   map[string]any{"content": "hi"},
		Reason: "clean up été",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 || !strings.Contains(string(resp.Body), `"id":"9"`) {
		t.Fatalf("unexpected response %d %s", resp.Status, resp.Body)
	}
	got := fake.Requests()[0]
	if got.Path != "/api/v10/channels/123/messages" || got.Query.Get("x") != "1" {
		t.Fatalf("wrong request %s ?%s", got.Path, got.Query.Encode())
	}
	if h := got.Header.Get("Authorization"); h != "Bot "+testBotToken {
		t.Fatalf("Authorization = %q", h)
	}
	if h := got.Header.Get("Content-Type"); h != "application/json" {
		t.Fatalf("Content-Type = %q", h)
	}
	if h := got.Header.Get("X-Audit-Log-Reason"); h != url.PathEscape("clean up été") {
		t.Fatalf("X-Audit-Log-Reason = %q", h)
	}
	var body map[string]any
	if err := json.Unmarshal(got.Body, &body); err != nil || body["content"] != "hi" {
		t.Fatalf("body = %s", got.Body)
	}
	calls := rec.Calls()
	if len(calls) != 1 || calls[0] != (CallRecord{Method: "POST", Route: "/channels/{channel_id}/messages", Status: 200}) {
		t.Fatalf("recorder = %+v", calls)
	}
}

func TestDoWebhookClientSendsNoAuthorization(t *testing.T) {
	fake := discordtest.New(t)
	fake.Handle("GET /api/v10/webhooks/{id}/{token}", discordtest.JSON(200, map[string]any{"id": "1"}))
	c := NewWebhook(Options{HTTPClient: fake.HTTPClient()})
	_, err := c.Do(context.Background(), Call{
		Method: http.MethodGet,
		Route:  "/webhooks/{webhook_id}/{webhook_token}",
		Params: map[string]string{"webhook_id": "1", "webhook_token": "tok"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h := fake.Requests()[0].Header.Get("Authorization"); h != "" {
		t.Fatalf("webhook request carried Authorization %q", h)
	}
}

func TestDoRejectsInvalidParamsWithoutSending(t *testing.T) {
	tests := []struct {
		name   string
		route  string
		params map[string]string
		arg    bool // want *ArgError (true) or an internal error (false)
	}{
		{"non-numeric id", "/channels/{channel_id}", map[string]string{"channel_id": "12a"}, true},
		{"path in id", "/channels/{channel_id}", map[string]string{"channel_id": "1/../2"}, true},
		{"bad webhook token", "/webhooks/{webhook_id}/{webhook_token}", map[string]string{"webhook_id": "1", "webhook_token": "a/b"}, true},
		{"empty emoji", "/r/{emoji}", map[string]string{"emoji": ""}, true},
		{"missing param", "/channels/{channel_id}", map[string]string{}, false},
		{"unused param", "/users/@me", map[string]string{"guild_id": "1"}, false},
		{"unknown param kind", "/x/{name}", map[string]string{"name": "a"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := discordtest.New(t)
			c := newBot(t, fake, time.Second)
			_, err := c.Do(context.Background(), Call{Method: "GET", Route: tt.route, Params: tt.params})
			var ae *ArgError
			if err == nil || errors.As(err, &ae) != tt.arg {
				t.Fatalf("err = %v (ArgError=%v), want ArgError=%v", err, errors.As(err, &ae), tt.arg)
			}
			if n := len(fake.Requests()); n != 0 {
				t.Fatalf("%d request(s) reached Discord", n)
			}
		})
	}
}

func TestDoEscapesEmoji(t *testing.T) {
	fake := discordtest.New(t)
	fake.Handle("PUT /api/v10/channels/{c}/messages/{m}/reactions/{emoji}/@me", discordtest.JSON(204, nil))
	c := newBot(t, fake, time.Second)
	resp, err := c.Do(context.Background(), Call{
		Method: "PUT",
		Route:  "/channels/{channel_id}/messages/{message_id}/reactions/{emoji}/@me",
		Params: map[string]string{"channel_id": "1", "message_id": "2", "emoji": "👍"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 204 {
		t.Fatalf("status = %d", resp.Status)
	}
	if raw := fake.Requests()[0].RawPath; !strings.Contains(raw, "/reactions/%F0%9F%91%8D/@me") {
		t.Fatalf("emoji not escaped: %s", raw)
	}
}

func TestDoErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    any
		check   func(t *testing.T, resp Response, err error)
	}{
		{"api error with code", 403, map[string]any{"message": "Missing Permissions", "code": 50013}, func(t *testing.T, _ Response, err error) {
			var ae *APIError
			if !errors.As(err, &ae) || err.Error() != "Discord 403: Missing Permissions (50013)" {
				t.Fatalf("err = %v", err)
			}
		}},
		{"api error without body", 404, "not json", func(t *testing.T, _ Response, err error) {
			if err == nil || err.Error() != "Discord 404: Not Found" {
				t.Fatalf("err = %v", err)
			}
		}},
		{"unauthorized", 401, map[string]any{"message": "401: Unauthorized", "code": 0}, func(t *testing.T, _ Response, err error) {
			if !IsUnauthorized(err) {
				t.Fatalf("IsUnauthorized(%v) = false", err)
			}
		}},
		{"accepted is not an error", 202, map[string]any{"message": "Index not yet available", "retry_after": 2}, func(t *testing.T, resp Response, err error) {
			if err != nil || resp.Status != 202 || !strings.Contains(string(resp.Body), "retry_after") {
				t.Fatalf("resp = %d %s, err = %v", resp.Status, resp.Body, err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := discordtest.New(t)
			fake.Handle("GET /api/v10/users/@me", discordtest.JSON(tt.status, tt.body))
			c := newBot(t, fake, time.Second)
			resp, err := c.Do(context.Background(), Call{Method: "GET", Route: "/users/@me"})
			tt.check(t, resp, err)
		})
	}
}

func TestDoRetriesRateLimitWithinTimeout(t *testing.T) {
	fake := discordtest.New(t)
	var n atomic.Int32
	fake.Handle("GET /api/v10/users/@me", func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			discordtest.JSON(429, map[string]any{"message": "You are being rate limited.", "retry_after": 0.05, "global": false})(w, r)
			return
		}
		discordtest.JSON(200, map[string]any{"id": "1"})(w, r)
	})
	c := newBot(t, fake, 2*time.Second)
	rec := NewRecorder()
	if _, err := c.Do(WithRecorder(context.Background(), rec), Call{Method: "GET", Route: "/users/@me"}); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 {
		t.Fatalf("want 2 requests, got %d", n.Load())
	}
	calls := rec.Calls()
	if len(calls) != 1 || calls[0].Status != 200 || calls[0].RateLimitedMS < 50 {
		t.Fatalf("recorder = %+v", calls)
	}
}

func TestDoReturnsRateLimitedBeyondTimeout(t *testing.T) {
	fake := discordtest.New(t)
	fake.Handle("GET /api/v10/users/@me", discordtest.JSON(429, map[string]any{"message": "slow down", "retry_after": 30}))
	c := newBot(t, fake, 200*time.Millisecond)
	start := time.Now()
	_, err := c.Do(context.Background(), Call{Method: "GET", Route: "/users/@me"})
	var rl *RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter != 30*time.Second {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("Do waited for a retry that could not fit in the timeout")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDoUnavailableNeverLeaksURL(t *testing.T) {
	hc := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})}
	c := NewWebhook(Options{Timeout: time.Second, HTTPClient: hc})
	_, err := c.Do(context.Background(), Call{
		Method: "GET",
		Route:  "/webhooks/{webhook_id}/{webhook_token}",
		Params: map[string]string{"webhook_id": "1", "webhook_token": "SUPERSECRETTOKEN"},
	})
	var ue *UnavailableError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "SUPERSECRETTOKEN") || strings.Contains(err.Error(), "discord.com") {
		t.Fatalf("error leaks the URL: %v", err)
	}
}

func TestDoMultipartFiles(t *testing.T) {
	fake := discordtest.New(t)
	fake.Handle("POST /api/v10/channels/{id}/messages", discordtest.JSON(200, map[string]any{"id": "1"}))
	c := newBot(t, fake, time.Second)
	_, err := c.Do(context.Background(), Call{
		Method: "POST",
		Route:  "/channels/{channel_id}/messages",
		Params: map[string]string{"channel_id": "1"},
		Body:   map[string]any{"content": "see file"},
		Files:  []File{{Name: "a.txt", ContentType: "text/plain", Data: []byte("hello file")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := fake.Requests()[0]
	if !strings.HasPrefix(got.Header.Get("Content-Type"), "multipart/form-data") {
		t.Fatalf("Content-Type = %q", got.Header.Get("Content-Type"))
	}
	for _, want := range []string{`name="payload_json"`, `"content":"see file"`, `name="files[0]"; filename="a.txt"`, "hello file"} {
		if !strings.Contains(string(got.Body), want) {
			t.Fatalf("multipart body missing %q:\n%s", want, got.Body)
		}
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `go get github.com/bwmarrin/discordgo@v0.29.0 && go test ./internal/discord/`
Expected: FAIL — `undefined: NewBot`.

- [ ] **Step 4: Implement errors and recorder**

`internal/discord/errors.go`:

```go
package discord

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ArgError is an invalid tool argument caught before any request was sent.
type ArgError struct{ Msg string }

func (e *ArgError) Error() string { return e.Msg }

// APIError is a non-2xx answer from Discord.
type APIError struct {
	Status  int
	Code    int
	Message string
}

func (e *APIError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("Discord %d: %s (%d)", e.Status, e.Message, e.Code)
	}
	return fmt.Sprintf("Discord %d: %s", e.Status, e.Message)
}

// RateLimitedError means Discord asked to wait longer than the call's timeout
// allows.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("Discord rate limit: retry after %s", e.RetryAfter.Round(10*time.Millisecond))
}

// UnavailableError is a transport failure or timeout. Cause never contains the
// request URL, which may embed a webhook token.
type UnavailableError struct{ Cause string }

func (e *UnavailableError) Error() string { return "Discord unavailable: " + e.Cause }

// IsUnauthorized reports whether Discord rejected the credential itself.
func IsUnauthorized(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusUnauthorized
}
```

`internal/discord/recorder.go`:

```go
package discord

import (
	"context"
	"sync"
	"time"
)

// CallRecord is one Discord request as the audit log reports it. Route is the
// template (or, for discord_request, the redacted path) — never the expanded
// path, which may contain a webhook token.
type CallRecord struct {
	Method        string `json:"method"`
	Route         string `json:"route"`
	Status        int    `json:"status"`
	RateLimitedMS int64  `json:"rate_limited_ms"`
}

// Recorder collects the Discord calls made while serving one tool call.
type Recorder struct {
	mu    sync.Mutex
	calls []CallRecord
}

// NewRecorder returns an empty recorder.
func NewRecorder() *Recorder { return &Recorder{} }

type recorderKey struct{}

// WithRecorder attaches r to ctx; Client.Do appends to it.
func WithRecorder(ctx context.Context, r *Recorder) context.Context {
	return context.WithValue(ctx, recorderKey{}, r)
}

func recorderFrom(ctx context.Context) *Recorder {
	r, _ := ctx.Value(recorderKey{}).(*Recorder)
	return r
}

func (r *Recorder) add(method, route string, status int, waited time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, CallRecord{Method: method, Route: route, Status: status, RateLimitedMS: waited.Milliseconds()})
}

// Calls returns a copy of the recorded calls.
func (r *Recorder) Calls() []CallRecord {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]CallRecord(nil), r.calls...)
}
```

- [ ] **Step 5: Implement route expansion**

`internal/discord/route.go`:

```go
package discord

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Hellhium/discord-mcp/internal/credential"
)

var paramRe = regexp.MustCompile(`\{([a-z_]+)\}`)

// expandRoute substitutes validated parameters into a route template. Every
// value is checked by kind before substitution, so a tool argument can never
// add path segments.
func expandRoute(route string, params map[string]string) (string, error) {
	var bad error
	used := 0
	out := paramRe.ReplaceAllStringFunc(route, func(m string) string {
		name := m[1 : len(m)-1]
		v, ok := params[name]
		if !ok {
			bad = fmt.Errorf("internal: route %s has no value for %s", route, name)
			return m
		}
		used++
		switch {
		case name == "emoji":
			if v == "" || len(v) > 100 {
				bad = &ArgError{Msg: "emoji must be a unicode emoji or name:id"}
			}
			return url.PathEscape(v)
		case name == "webhook_token":
			if !credential.IsWebhookToken(v) {
				bad = &ArgError{Msg: "invalid webhook token"}
			}
			return v
		case strings.HasSuffix(name, "_id"):
			if !credential.IsSnowflake(v) {
				bad = &ArgError{Msg: fmt.Sprintf("%s must be a Discord ID (digits only), got %q", name, v)}
			}
			return v
		default:
			bad = fmt.Errorf("internal: route parameter %s has no validation rule", name)
			return m
		}
	})
	if bad != nil {
		return "", bad
	}
	if used != len(params) {
		return "", errors.New("internal: route " + route + " was given unused parameters")
	}
	return out, nil
}

// majorParams are the IDs that make a Discord rate-limit bucket distinct.
var majorParams = []string{"channel_id", "guild_id", "webhook_id"}

// bucketFor keys discordgo's rate limiter the way Discord buckets requests:
// per method and route, per major resource.
func bucketFor(method, route string, params map[string]string) string {
	b := route
	for _, p := range majorParams {
		if v, ok := params[p]; ok {
			b = strings.ReplaceAll(b, "{"+p+"}", v)
		}
	}
	return method + " " + b
}
```

- [ ] **Step 6: Implement the client**

`internal/discord/client.go`:

```go
// Package discord is the only code that talks to Discord's REST API. It wraps
// a discordgo session per credential — discordgo keeps the rate-limit buckets —
// and adds what the tools and the audit log need: validated route templates,
// a fixed API host, typed errors that never contain a URL, a per-call timeout
// that also bounds rate-limit waits, and a record of every call made.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/bwmarrin/discordgo"
)

// APIBase is the only place requests are sent. No argument can change it.
const APIBase = "https://discord.com/api/v10"

const defaultTimeout = 15 * time.Second

// Options configures a client.
type Options struct {
	// Timeout bounds one Do call, rate-limit waits included.
	Timeout time.Duration
	// HTTPClient supplies the transport (tests inject the fake). Its Timeout
	// is ignored; Timeout above applies instead.
	HTTPClient *http.Client
}

// Client sends REST calls with one credential.
type Client struct {
	session *discordgo.Session
	timeout time.Duration
}

// NewBot returns a client authenticating as a bot.
func NewBot(token string, opts Options) *Client { return newClient("Bot "+token, opts) }

// NewWebhook returns a client with no Authorization header: webhook routes
// authenticate with the token in the path.
func NewWebhook(opts Options) *Client { return newClient("", opts) }

func newClient(authorization string, opts Options) *Client {
	s, _ := discordgo.New(authorization) // never fails in v0.29
	base := http.DefaultTransport
	if opts.HTTPClient != nil && opts.HTTPClient.Transport != nil {
		base = opts.HTTPClient.Transport
	}
	s.Client = &http.Client{Transport: statusTransport{base: base}}
	s.StateEnabled = false
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{session: s, timeout: timeout}
}

// Session exposes the discordgo session, for the Gateway.
func (c *Client) Session() *discordgo.Session { return c.session }

// Call describes one REST request.
type Call struct {
	Method string
	// Route is a template such as "/channels/{channel_id}/messages".
	Route  string
	Params map[string]string
	Query  url.Values
	// Body is JSON-encoded when non-nil; with Files it becomes payload_json.
	Body   any
	Files  []File
	// Reason is sent as X-Audit-Log-Reason.
	Reason string
}

// File is an attachment uploaded as files[n].
type File struct {
	Name        string
	ContentType string
	Data        []byte
}

// Response is a successful answer (2xx, including 202).
type Response struct {
	Status int
	Body   json.RawMessage
}

// Do validates and sends call.
func (c *Client) Do(ctx context.Context, call Call) (Response, error) {
	path, err := expandRoute(call.Route, call.Params)
	if err != nil {
		return Response{}, err
	}
	return c.send(ctx, call, path, call.Route, bucketFor(call.Method, call.Route, call.Params))
}

// send performs the request. recordRoute is what the recorder stores.
func (c *Client) send(ctx context.Context, call Call, path, recordRoute, bucket string) (Response, error) {
	u := APIBase + path
	if len(call.Query) > 0 {
		u += "?" + call.Query.Encode()
	}
	contentType, body, err := encodeBody(call.Body, call.Files)
	if err != nil {
		return Response{}, &ArgError{Msg: err.Error()}
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	rec := recorderFrom(ctx)

	var waited time.Duration
	for {
		sink := &statusSink{}
		opts := []discordgo.RequestOption{
			discordgo.WithContext(context.WithValue(ctx, sinkKey{}, sink)),
			// Rate limits are handled here so a wait can be bounded by ctx.
			discordgo.WithRetryOnRatelimit(false),
		}
		if call.Reason != "" {
			// Discord requires the header value to be URL-encoded.
			opts = append(opts, discordgo.WithAuditLogReason(url.PathEscape(call.Reason)))
		}
		raw, err := c.session.RequestRaw(call.Method, u, contentType, body, bucket, 0, opts...)

		var rl *discordgo.RateLimitError
		if errors.As(err, &rl) {
			retry := rl.RetryAfter
			if dl, ok := ctx.Deadline(); ok && time.Until(dl) <= retry {
				rec.add(call.Method, recordRoute, sink.status, waited)
				return Response{}, &RateLimitedError{RetryAfter: retry}
			}
			select {
			case <-time.After(retry):
				waited += retry
				continue
			case <-ctx.Done():
				rec.add(call.Method, recordRoute, sink.status, waited)
				return Response{}, &RateLimitedError{RetryAfter: retry}
			}
		}
		rec.add(call.Method, recordRoute, sink.status, waited)
		return c.result(ctx, raw, sink.status, err)
	}
}

func (c *Client) result(ctx context.Context, raw []byte, status int, err error) (Response, error) {
	if err == nil {
		return Response{Status: status, Body: raw}, nil
	}
	var re *discordgo.RESTError
	if errors.As(err, &re) {
		st := re.Response.StatusCode
		if st == http.StatusAccepted {
			return Response{Status: st, Body: re.ResponseBody}, nil
		}
		ae := &APIError{Status: st, Message: http.StatusText(st)}
		if re.Message != nil {
			ae.Code = re.Message.Code
			if re.Message.Message != "" {
				ae.Message = re.Message.Message
			}
		}
		return Response{Status: st, Body: re.ResponseBody}, ae
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Response{}, &UnavailableError{Cause: fmt.Sprintf("no response within %s", c.timeout)}
	}
	if ctx.Err() != nil {
		return Response{}, &UnavailableError{Cause: "request cancelled"}
	}
	// *url.Error embeds the full URL, which can contain a webhook token.
	var ue *url.Error
	if errors.As(err, &ue) {
		return Response{}, &UnavailableError{Cause: ue.Err.Error()}
	}
	return Response{}, &UnavailableError{Cause: err.Error()}
}

func encodeBody(body any, files []File) (string, []byte, error) {
	if len(files) > 0 {
		payload := body
		if payload == nil {
			payload = map[string]any{}
		}
		df := make([]*discordgo.File, len(files))
		for i, f := range files {
			df[i] = &discordgo.File{Name: f.Name, ContentType: f.ContentType, Reader: bytes.NewReader(f.Data)}
		}
		return discordgo.MultipartBodyWithJSON(payload, df)
	}
	if body == nil {
		return "", nil, nil
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "", nil, fmt.Errorf("encode request body: %w", err)
	}
	return "application/json", b, nil
}

type sinkKey struct{}

type statusSink struct{ status int }

// statusTransport captures the HTTP status discordgo does not return on
// success, for the audit record.
type statusTransport struct{ base http.RoundTripper }

func (t statusTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(r)
	if sink, ok := r.Context().Value(sinkKey{}).(*statusSink); ok && resp != nil {
		sink.status = resp.StatusCode
	}
	return resp, err
}
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/discord/ ./internal/discordtest/`
Expected: PASS. Two things to check if a test fails:
- `TestDoErrors/"api error without body"`: `discordtest.JSON(404, "not json")` encodes the JSON string `"not json"`; decoding it into `*APIErrorMessage` fails, so `re.Message` is nil and the message falls back to `http.StatusText`. If discordgo decodes it differently, assert on `Discord 404:` prefix only.
- `TestDoRetriesRateLimitWithinTimeout`: discordgo sleeps for its own bucket bookkeeping only when rate-limit headers are present; the fake sends none.

- [ ] **Step 8: Commit**

```bash
go mod tidy
git add go.mod go.sum internal/discord internal/discordtest
git commit -m "feat(discord): add rest client with validated routes and typed errors" -m "All Discord traffic goes through discord.Client.Do: route templates whose
parameters are checked by kind before substitution (snowflakes, webhook
tokens, path-escaped emoji), a fixed https://discord.com/api/v10 host,
JSON or multipart bodies, and X-Audit-Log-Reason. discordgo keeps the
rate-limit buckets, but 429s are handled here so a wait never exceeds
discord.timeout; past it the call fails with retry_after.

Errors are typed for the audit outcome and never carry the request URL,
which may embed a webhook token. A 202 is returned as a response so
message search can report an index that is not ready. Each call is
recorded with its route template and status for the audit log.

discordtest is a fake Discord REST API whose client refuses every host
but discord.com."
```

---
### Task 4: Raw route guard and credential verification

**Files:**
- Modify: `internal/discord/route.go` (append)
- Create: `internal/discord/raw.go`, `internal/discord/verify.go`
- Test: `internal/discord/raw_test.go`, `internal/discord/verify_test.go`

**Interfaces:**
- Consumes: `Client`, `Call`, `Response`, `send`, `ArgError` (Task 3); `credential.Webhook`, `credential.IsSnowflake` (Task 1); `intents.Mask`, `intents.MissingPrivileged` (Task 1).
- Produces:
  - `discord.ValidateRawRoute(route string) error` (returns `*ArgError`)
  - `discord.RedactRoute(path string) string` — snowflakes → `{id}`, token after `webhooks/{id}` or `interactions/{id}` → `{token}`
  - `(*Client).DoRaw(ctx context.Context, method, route string, query url.Values, body any, reason string) (Response, error)`
  - `discord.BotIdentity{UserID, Username string}`, `(*Client).VerifyBot(ctx) (BotIdentity, error)`
  - `discord.WebhookInfo{ID, Name, ChannelID, GuildID string}`, `(*Client).VerifyWebhook(ctx, credential.Webhook) (WebhookInfo, error)`
  - `(*Client).CheckPrivilegedIntents(ctx, intents.Mask) error`

- [ ] **Step 1: Write the failing raw-route tests**

`internal/discord/raw_test.go`:

```go
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
		"/users/@me":                         "/users/@me",
		"/channels/123/messages/456":         "/channels/{id}/messages/{id}",
		"/webhooks/123/SeCrEt_token":         "/webhooks/{id}/{token}",
		"/webhooks/123/SeCrEt_token/messages/9": "/webhooks/{id}/{token}/messages/{id}",
		"/interactions/123/tok/callback":     "/interactions/{id}/{token}/callback",
		"/webhooks/123":                      "/webhooks/{id}",
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/discord/ -run 'Raw|Redact'`
Expected: FAIL — `undefined: ValidateRawRoute`.

- [ ] **Step 3: Implement the guard, redaction and DoRaw**

Append to `internal/discord/route.go`:

```go
// ValidateRawRoute checks a discord_request route. It must be a plain path
// relative to APIBase: no scheme, host, authority, query, fragment, backslash,
// empty segment or dot-dot segment — also when percent-encoded — so the
// request can only ever reach a path under https://discord.com/api/v10.
func ValidateRawRoute(route string) error {
	fail := func(why string) error { return &ArgError{Msg: "invalid route: " + why} }
	if !strings.HasPrefix(route, "/") {
		return fail("must start with /")
	}
	if strings.HasPrefix(route, "/api/") {
		return fail("must be relative to /api/v10 (drop the /api/v10 prefix)")
	}
	for _, b := range []byte(route) {
		if b <= ' ' || b == 0x7f {
			return fail("must not contain spaces or control characters")
		}
	}
	if strings.ContainsAny(route, `\?#`) {
		return fail(`must not contain \, ? or # (pass query parameters in query)`)
	}
	if strings.Contains(route, "://") {
		return fail("must not contain a URL")
	}
	for _, seg := range strings.Split(route[1:], "/") {
		if seg == "" {
			return fail("must not contain empty segments")
		}
		dec, err := url.PathUnescape(seg)
		if err != nil {
			return fail("contains an invalid percent-encoding")
		}
		if dec == ".." || dec == "." || strings.ContainsAny(dec, `/\`) {
			return fail("must not contain dot or encoded slash segments")
		}
	}
	return nil
}

// RedactRoute turns a concrete path into a loggable template: IDs become {id}
// and the token segment of webhook and interaction routes becomes {token}.
func RedactRoute(path string) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		switch {
		case credential.IsSnowflake(s):
			segs[i] = "{id}"
		case i >= 2 && (segs[i-2] == "webhooks" || segs[i-2] == "interactions") && segs[i-1] == "{id}":
			segs[i] = "{token}"
		}
	}
	return strings.Join(segs, "/")
}
```

(`RedactRoute` rewrites in place left to right, so by the time the token segment is examined its preceding ID has already become `{id}`.)

`internal/discord/raw.go`:

```go
package discord

import (
	"context"
	"net/url"
)

var rawMethods = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}

// DoRaw sends a discord_request call. The route is validated by
// ValidateRawRoute and always appended to APIBase; the recorder stores the
// redacted route.
func (c *Client) DoRaw(ctx context.Context, method, route string, query url.Values, body any, reason string) (Response, error) {
	if !rawMethods[method] {
		return Response{}, &ArgError{Msg: "method must be one of GET, POST, PUT, PATCH, DELETE"}
	}
	if err := ValidateRawRoute(route); err != nil {
		return Response{}, err
	}
	call := Call{Method: method, Route: route, Query: query, Body: body, Reason: reason}
	return c.send(ctx, call, route, RedactRoute(route), method+" "+route)
}
```

- [ ] **Step 4: Run the raw tests**

Run: `go test ./internal/discord/ -run 'Raw|Redact'`
Expected: PASS.

- [ ] **Step 5: Write the failing verification tests**

`internal/discord/verify_test.go`:

```go
package discord

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Hellhium/discord-mcp/internal/credential"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
	"github.com/Hellhium/discord-mcp/internal/intents"
)

func TestVerifyBot(t *testing.T) {
	fake := discordtest.New(t)
	fake.Handle("GET /api/v10/users/@me", discordtest.JSON(200, map[string]any{"id": "42", "username": "helper", "bot": true}))
	got, err := newBot(t, fake, time.Second).VerifyBot(context.Background())
	if err != nil || got != (BotIdentity{UserID: "42", Username: "helper"}) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestVerifyBotRejected(t *testing.T) {
	fake := discordtest.New(t)
	fake.Handle("GET /api/v10/users/@me", discordtest.JSON(401, map[string]any{"message": "401: Unauthorized", "code": 0}))
	if _, err := newBot(t, fake, time.Second).VerifyBot(context.Background()); !IsUnauthorized(err) {
		t.Fatalf("err = %v, want unauthorized", err)
	}
}

func TestVerifyWebhook(t *testing.T) {
	fake := discordtest.New(t)
	fake.Handle("GET /api/v10/webhooks/{id}/{token}", discordtest.JSON(200, map[string]any{
		"id": "7", "name": "alerts", "channel_id": "8", "guild_id": "9",
	}))
	c := NewWebhook(Options{HTTPClient: fake.HTTPClient()})
	got, err := c.VerifyWebhook(context.Background(), credential.Webhook{ID: "7", Token: "tok"})
	if err != nil || got != (WebhookInfo{ID: "7", Name: "alerts", ChannelID: "8", GuildID: "9"}) {
		t.Fatalf("got %+v, %v", got, err)
	}
	if p := fake.Requests()[0].Path; p != "/api/v10/webhooks/7/tok" {
		t.Fatalf("path = %s", p)
	}
}

func TestCheckPrivilegedIntents(t *testing.T) {
	tests := []struct {
		name  string
		flags uint64
		mask  []string
		want  string // "" = no error
	}{
		{"not privileged", 0, []string{"guilds", "guild_messages"}, ""},
		{"content enabled (limited)", 1 << 19, []string{"message_content"}, ""},
		{"content missing", 0, []string{"message_content", "guild_members"}, `privileged gateway intent(s) guild_members, message_content`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := discordtest.New(t)
			fake.Handle("GET /api/v10/applications/@me", discordtest.JSON(200, map[string]any{"id": "1", "flags": tt.flags}))
			m, err := intents.Parse(tt.mask)
			if err != nil {
				t.Fatal(err)
			}
			err = newBot(t, fake, time.Second).CheckPrivilegedIntents(context.Background(), m)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if tt.name == "not privileged" && len(fake.Requests()) != 0 {
					t.Fatal("no privileged intents requested: must not call Discord")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}
```

- [ ] **Step 6: Run to verify failure**

Run: `go test ./internal/discord/ -run Verify\|Privileged`
Expected: FAIL — `VerifyBot undefined`.

- [ ] **Step 7: Implement verification**

`internal/discord/verify.go`:

```go
package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Hellhium/discord-mcp/internal/credential"
	"github.com/Hellhium/discord-mcp/internal/intents"
)

// BotIdentity is the bot user behind a token.
type BotIdentity struct {
	UserID   string
	Username string
}

// VerifyBot checks the client's bot token with GET /users/@me.
func (c *Client) VerifyBot(ctx context.Context) (BotIdentity, error) {
	resp, err := c.Do(ctx, Call{Method: "GET", Route: "/users/@me"})
	if err != nil {
		return BotIdentity{}, err
	}
	var u struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(resp.Body, &u); err != nil {
		return BotIdentity{}, fmt.Errorf("decode /users/@me: %w", err)
	}
	return BotIdentity{UserID: u.ID, Username: u.Username}, nil
}

// WebhookInfo describes a webhook as Discord reports it.
type WebhookInfo struct {
	ID        string
	Name      string
	ChannelID string
	GuildID   string
}

// VerifyWebhook checks a webhook with GET /webhooks/{id}/{token}.
func (c *Client) VerifyWebhook(ctx context.Context, wh credential.Webhook) (WebhookInfo, error) {
	resp, err := c.Do(ctx, Call{
		Method: "GET",
		Route:  "/webhooks/{webhook_id}/{webhook_token}",
		Params: map[string]string{"webhook_id": wh.ID, "webhook_token": wh.Token},
	})
	if err != nil {
		return WebhookInfo{}, err
	}
	var w struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		ChannelID string `json:"channel_id"`
		GuildID   string `json:"guild_id"`
	}
	if err := json.Unmarshal(resp.Body, &w); err != nil {
		return WebhookInfo{}, fmt.Errorf("decode webhook: %w", err)
	}
	return WebhookInfo{ID: w.ID, Name: w.Name, ChannelID: w.ChannelID, GuildID: w.GuildID}, nil
}

// privilegedMask is every intent MissingPrivileged knows about.
const privilegedMask = intents.Mask(1<<1 | 1<<8 | 1<<15)

// CheckPrivilegedIntents fails, naming the intents, when m requests a
// privileged intent that is not enabled for the application. It calls
// Discord only when m contains a privileged intent.
func (c *Client) CheckPrivilegedIntents(ctx context.Context, m intents.Mask) error {
	if m&privilegedMask == 0 {
		return nil
	}
	resp, err := c.Do(ctx, Call{Method: "GET", Route: "/applications/@me"})
	if err != nil {
		return fmt.Errorf("read application flags: %w", err)
	}
	var app struct {
		Flags uint64 `json:"flags"`
	}
	if err := json.Unmarshal(resp.Body, &app); err != nil {
		return fmt.Errorf("decode /applications/@me: %w", err)
	}
	if missing := intents.MissingPrivileged(m, app.Flags); len(missing) > 0 {
		return fmt.Errorf("privileged gateway intent(s) %s not enabled for this application: enable them in the Discord Developer Portal (Bot → Privileged Gateway Intents)",
			strings.Join(missing, ", "))
	}
	return nil
}
```

- [ ] **Step 8: Run all discord tests**

Run: `go test ./internal/discord/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/discord
git commit -m "feat(discord): guard raw routes and verify credentials" -m "discord_request needs a route that can only reach a path under
https://discord.com/api/v10. ValidateRawRoute rejects schemes, hosts,
queries, fragments, backslashes, empty segments and dot or encoded slash
segments, including percent-encoded ones, and DoRaw sends only validated
routes. RedactRoute gives the audit log a template with IDs and webhook
or interaction tokens replaced.

VerifyBot and VerifyWebhook back the startup checks and direct auth.
CheckPrivilegedIntents reads the application flags before a Gateway is
opened, so a missing privileged intent is reported by name instead of as
an opaque 4014 close."
```

---
### Task 5: Audit logger

**Files:**
- Create: `internal/audit/audit.go`, `internal/audit/sanitize.go`
- Test: `internal/audit/audit_test.go`

**Interfaces:**
- Consumes: `discord.CallRecord`, `discord.RedactRoute` (Tasks 3–4).
- Produces:
  - `audit.Outcome` (`string`) and constants `OutcomeOK`, `OutcomeInvalidArgs`, `OutcomeDiscordError`, `OutcomeRateLimited`, `OutcomeCredentialRejected`
  - `audit.Actor{Kind, Instance, Credential string}` (JSON `kind`, `instance,omitempty`, `credential,omitempty`)
  - `audit.Action{Actor Actor; Tool string; Outcome Outcome; Duration time.Duration; Targets map[string]string; Args map[string]any; Calls []discord.CallRecord; Error string}`
  - `audit.New(w io.Writer) *Logger`
  - `(*Logger).Action(a Action)`, `(*Logger).AuthRejected(reason, header string)`, `(*Logger).Gateway(instance, event string, err error)`, `(*Logger).Info(msg string, args ...any)`
  - `audit.SanitizeArgs(args map[string]any) map[string]any`, `audit.Targets(args map[string]any) map[string]string`, `audit.MaxBodyBytes = 16 << 10`

- [ ] **Step 1: Write the failing tests**

`internal/audit/audit_test.go`:

```go
package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Hellhium/discord-mcp/internal/discord"
)

func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("not a JSON line: %q", line)
		}
		out = append(out, m)
	}
	return out
}

func TestActionLine(t *testing.T) {
	var buf bytes.Buffer
	New(&buf).Action(Action{
		Actor:    Actor{Kind: "instance", Instance: "assistant"},
		Tool:     "discord_send_message",
		Outcome:  OutcomeOK,
		Duration: 142 * time.Millisecond,
		Targets:  map[string]string{"channel_id": "1"},
		Args:     map[string]any{"content": "hello"},
		Calls:    []discord.CallRecord{{Method: "POST", Route: "/channels/{channel_id}/messages", Status: 200}},
	})
	m := decodeLines(t, &buf)[0]
	want := map[string]any{
		"level":       "INFO",
		"msg":         "action",
		"principal":   map[string]any{"kind": "instance", "instance": "assistant"},
		"tool":        "discord_send_message",
		"outcome":     "ok",
		"duration_ms": float64(142),
		"targets":     map[string]any{"channel_id": "1"},
		"args":        map[string]any{"content": "hello"},
		"discord":     []any{map[string]any{"method": "POST", "route": "/channels/{channel_id}/messages", "status": float64(200), "rate_limited_ms": float64(0)}},
	}
	for k, v := range want {
		if !reflect.DeepEqual(m[k], v) {
			t.Errorf("%s = %#v, want %#v", k, m[k], v)
		}
	}
	if _, ok := m["time"]; !ok {
		t.Error("missing time")
	}
	if _, ok := m["error"]; ok {
		t.Error("error must be omitted when empty")
	}
}

func TestActionFailureIsWarnWithError(t *testing.T) {
	var buf bytes.Buffer
	New(&buf).Action(Action{
		Actor:   Actor{Kind: "direct_bot", Credential: "sha256:1a2b3c4d"},
		Tool:    "discord_get_me",
		Outcome: OutcomeDiscordError,
		Error:   "Discord 403: Missing Permissions (50013)",
	})
	m := decodeLines(t, &buf)[0]
	if m["level"] != "WARN" || m["error"] != "Discord 403: Missing Permissions (50013)" {
		t.Fatalf("line = %v", m)
	}
	if p := m["principal"].(map[string]any); p["credential"] != "sha256:1a2b3c4d" || p["instance"] != nil {
		t.Fatalf("principal = %v", p)
	}
	if !reflect.DeepEqual(m["discord"], []any{}) || !reflect.DeepEqual(m["targets"], map[string]any{}) {
		t.Fatalf("empty calls/targets must render as [] and {}: %v %v", m["discord"], m["targets"])
	}
}

func TestAuthRejectedAndGateway(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.AuthRejected("unknown_token", "Authorization")
	l.Gateway("assistant", "gateway_connected", nil)
	l.Gateway("assistant", "gateway_disconnected", errors.New("eof"))
	lines := decodeLines(t, &buf)
	if lines[0]["msg"] != "auth_rejected" || lines[0]["reason"] != "unknown_token" || lines[0]["header"] != "Authorization" || lines[0]["level"] != "WARN" {
		t.Fatalf("auth line = %v", lines[0])
	}
	if lines[1]["msg"] != "gateway_connected" || lines[1]["instance"] != "assistant" || lines[1]["level"] != "INFO" {
		t.Fatalf("gateway line = %v", lines[1])
	}
	if lines[2]["level"] != "WARN" || lines[2]["error"] != "eof" {
		t.Fatalf("gateway error line = %v", lines[2])
	}
}

func TestSanitizeArgs(t *testing.T) {
	data := []byte("hello attachment")
	sum := sha256.Sum256(data)
	big := strings.Repeat("x", MaxBodyBytes+100)
	in := map[string]any{
		"content": "hi",
		"attachments": []any{
			map[string]any{"filename": "a.txt", "content_base64": base64.StdEncoding.EncodeToString(data), "description": "doc"},
			map[string]any{"filename": "b.bin", "content_base64": "!!!not base64"},
		},
		"route": "/webhooks/123/SECRET/messages/9",
		"body":  map[string]any{"text": big},
	}
	out := SanitizeArgs(in)

	if out["content"] != "hi" {
		t.Errorf("content changed: %v", out["content"])
	}
	atts := out["attachments"].([]any)
	a := atts[0].(map[string]any)
	if a["filename"] != "a.txt" || a["size"] != len(data) || a["sha256"] != hex.EncodeToString(sum[:]) || a["description"] != "doc" {
		t.Errorf("attachment = %v", a)
	}
	if _, ok := a["content_base64"]; ok {
		t.Error("attachment content must be removed")
	}
	if b := atts[1].(map[string]any); b["invalid_base64"] != true {
		t.Errorf("invalid attachment = %v", b)
	}
	if out["route"] != "/webhooks/{id}/{token}/messages/{id}" {
		t.Errorf("route = %v", out["route"])
	}
	body := out["body"].(map[string]any)
	if body["truncated"] != true || len(body["prefix"].(string)) > MaxBodyBytes {
		t.Errorf("body not truncated: truncated=%v len=%d", body["truncated"], len(body["prefix"].(string)))
	}
	// The input must not be modified.
	if _, ok := in["attachments"].([]any)[0].(map[string]any)["content_base64"]; !ok {
		t.Error("SanitizeArgs mutated its input")
	}
}

func TestSanitizeSmallBodyKept(t *testing.T) {
	out := SanitizeArgs(map[string]any{"body": map[string]any{"name": "x"}})
	if !reflect.DeepEqual(out["body"], map[string]any{"name": "x"}) {
		t.Fatalf("small body changed: %v", out["body"])
	}
}

func TestTargets(t *testing.T) {
	got := Targets(map[string]any{"channel_id": "1", "message_id": "2", "user_id": 3, "content": "x"})
	want := map[string]string{"channel_id": "1", "message_id": "2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Targets = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/audit/`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Implement the logger**

`internal/audit/audit.go`:

```go
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
```

- [ ] **Step 4: Implement sanitising**

`internal/audit/sanitize.go`:

```go
package audit

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/Hellhium/discord-mcp/internal/discord"
)

// MaxBodyBytes caps a logged discord_request body.
const MaxBodyBytes = 16 << 10

// SanitizeArgs returns a copy of tool arguments safe to log: attachment content
// becomes filename, size and SHA-256; a discord_request body over MaxBodyBytes
// is truncated; a discord_request route is redacted. Everything else, message
// text included, is logged as given.
func SanitizeArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		switch k {
		case "attachments":
			out[k] = sanitizeAttachments(v)
		case "body":
			out[k] = truncateJSON(v)
		case "route":
			if s, ok := v.(string); ok {
				out[k] = discord.RedactRoute(s)
			} else {
				out[k] = v
			}
		default:
			out[k] = v
		}
	}
	return out
}

func sanitizeAttachments(v any) any {
	list, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			out = append(out, "[invalid attachment]")
			continue
		}
		s := map[string]any{"filename": m["filename"]}
		if d, ok := m["description"]; ok {
			s["description"] = d
		}
		b64, _ := m["content_base64"].(string)
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			s["size"] = len(b64)
			s["invalid_base64"] = true
		} else {
			sum := sha256.Sum256(data)
			s["size"] = len(data)
			s["sha256"] = hex.EncodeToString(sum[:])
		}
		out = append(out, s)
	}
	return out
}

func truncateJSON(v any) any {
	b, err := json.Marshal(v)
	if err != nil || len(b) <= MaxBodyBytes {
		return v
	}
	return map[string]any{
		"truncated": true,
		"bytes":     len(b),
		"prefix":    strings.ToValidUTF8(string(b[:MaxBodyBytes]), ""),
	}
}

// Targets extracts the Discord IDs a call was aimed at: every string argument
// whose name ends in _id.
func Targets(args map[string]any) map[string]string {
	t := make(map[string]string)
	for k, v := range args {
		if s, ok := v.(string); ok && strings.HasSuffix(k, "_id") {
			t[k] = s
		}
	}
	return t
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/audit/`
Expected: PASS. Note `TestSanitizeArgs` compares `a["size"] != len(data)`: the value is stored as `int`, so the comparison is between `any(int)` and `int` and holds.

- [ ] **Step 6: Commit**

```bash
git add internal/audit
git commit -m "feat(audit): log every action as one json line" -m "Every tool call will produce one action line on stdout: the principal
(instance name or a short credential hash, never a token), tool,
outcome, duration, target IDs, arguments and the Discord calls made with
their route templates and statuses. Failures log at WARN with the error.

SanitizeArgs replaces attachment content with filename, size and
SHA-256, truncates discord_request bodies past 16 KB and redacts the
tokens a raw route could carry. Authentication rejections and Gateway
lifecycle events get their own lines."
```

---
### Task 6: Event buffer

**Files:**
- Create: `internal/events/buffer.go`
- Test: `internal/events/buffer_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `events.Event{Seq uint64; Type string; Time time.Time; GuildID, ChannelID, AuthorID string; Data json.RawMessage}` (JSON `seq`, `type`, `time`, `guild_id,omitempty`, `channel_id,omitempty`, `author_id,omitempty`, `data,omitempty`)
  - `events.Filter{Types []string; GuildID, ChannelID, AuthorID, ExcludeAuthorID string}`, `(Filter).Match(Event) bool`
  - `events.Page{Events []Event; NextCursor string; Gap bool}`
  - `events.NewBuffer(size int) *Buffer`
  - `(*Buffer).Append(typ string, data json.RawMessage) Event`
  - `(*Buffer).Cursor() string` — `"<boot-id>:<last seq>"`
  - `(*Buffer).Since(cursor string, f Filter, limit int) (Page, error)`
  - `(*Buffer).Wait(ctx context.Context, cursor string, f Filter) (ev *Event, next string, gap bool, err error)` — returns `ctx.Err()` when ctx ends, `ErrClosed` after `Close`
  - `(*Buffer).Close()`
  - `events.ErrClosed`, `events.ErrBadCursor` (errors are wrapped with `%w`)

- [ ] **Step 1: Write the failing tests**

`internal/events/buffer_test.go`:

```go
package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

func msg(channel, author string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"id":"1","guild_id":"9","channel_id":%q,"author":{"id":%q},"content":"x"}`, channel, author))
}

func seqs(evs []Event) []uint64 {
	out := make([]uint64, len(evs))
	for i, e := range evs {
		out[i] = e.Seq
	}
	return out
}

func TestAppendExtractsIDs(t *testing.T) {
	b := newBuffer(10, "boot")
	e := b.Append("MESSAGE_CREATE", msg("5", "7"))
	if e.Seq != 1 || e.GuildID != "9" || e.ChannelID != "5" || e.AuthorID != "7" || e.Time.IsZero() {
		t.Fatalf("event = %+v", e)
	}
	r := b.Append("MESSAGE_REACTION_ADD", json.RawMessage(`{"user_id":"3","channel_id":"5","guild_id":"9"}`))
	if r.AuthorID != "3" || r.Seq != 2 {
		t.Fatalf("reaction = %+v", r)
	}
	c := b.Append("CHANNEL_CREATE", json.RawMessage(`{"id":"44","guild_id":"9"}`))
	if c.ChannelID != "44" {
		t.Fatalf("channel event = %+v", c)
	}
	if b.Cursor() != "boot:3" {
		t.Fatalf("cursor = %s", b.Cursor())
	}
}

func TestSinceWithoutCursorReturnsLatest(t *testing.T) {
	b := newBuffer(10, "boot")
	for i := 0; i < 5; i++ {
		b.Append("MESSAGE_CREATE", msg("5", "7"))
	}
	p, err := b.Since("", Filter{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := seqs(p.Events); fmt.Sprint(got) != "[4 5]" || p.NextCursor != "boot:5" || p.Gap {
		t.Fatalf("page = %v next=%s gap=%v", got, p.NextCursor, p.Gap)
	}
}

func TestSinceCursorFilterAndPaging(t *testing.T) {
	b := newBuffer(10, "boot")
	b.Append("MESSAGE_CREATE", msg("5", "7"))   // 1
	b.Append("MESSAGE_CREATE", msg("6", "7"))   // 2
	b.Append("TYPING_START", msg("5", "7"))     // 3
	b.Append("MESSAGE_CREATE", msg("5", "8"))   // 4
	b.Append("MESSAGE_CREATE", msg("5", "7"))   // 5

	f := Filter{Types: []string{"MESSAGE_CREATE"}, ChannelID: "5"}
	p, err := b.Since("boot:0", f, 2)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(seqs(p.Events)) != "[1 4]" || p.NextCursor != "boot:4" {
		t.Fatalf("first page = %v next=%s", seqs(p.Events), p.NextCursor)
	}
	p, _ = b.Since(p.NextCursor, f, 2)
	if fmt.Sprint(seqs(p.Events)) != "[5]" || p.NextCursor != "boot:5" {
		t.Fatalf("second page = %v next=%s", seqs(p.Events), p.NextCursor)
	}
	p, _ = b.Since(p.NextCursor, f, 2)
	if len(p.Events) != 0 || p.NextCursor != "boot:5" {
		t.Fatalf("empty page = %v next=%s", seqs(p.Events), p.NextCursor)
	}
	p, _ = b.Since("boot:0", Filter{ExcludeAuthorID: "7"}, 10)
	if fmt.Sprint(seqs(p.Events)) != "[4]" {
		t.Fatalf("exclude author = %v", seqs(p.Events))
	}
}

func TestSinceGapAfterWraparound(t *testing.T) {
	b := newBuffer(3, "boot")
	for i := 0; i < 6; i++ { // keeps 4,5,6
		b.Append("MESSAGE_CREATE", msg("5", "7"))
	}
	p, err := b.Since("boot:1", Filter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Gap || fmt.Sprint(seqs(p.Events)) != "[4 5 6]" {
		t.Fatalf("page = %v gap=%v", seqs(p.Events), p.Gap)
	}
	p, _ = b.Since("boot:3", Filter{}, 10)
	if p.Gap || fmt.Sprint(seqs(p.Events)) != "[4 5 6]" {
		t.Fatalf("cursor at oldest-1 must not be a gap: %v gap=%v", seqs(p.Events), p.Gap)
	}
}

func TestSinceOtherBootIsGap(t *testing.T) {
	b := newBuffer(3, "boot")
	b.Append("MESSAGE_CREATE", msg("5", "7"))
	p, err := b.Since("previous:99", Filter{}, 10)
	if err != nil || !p.Gap || len(p.Events) != 1 {
		t.Fatalf("page = %+v err=%v", p, err)
	}
}

func TestSinceBadCursor(t *testing.T) {
	b := newBuffer(3, "boot")
	for _, c := range []string{"nocolon", "boot:x", "boot:-1", "boot:5"} {
		if _, err := b.Since(c, Filter{}, 10); !errors.Is(err, ErrBadCursor) {
			t.Errorf("Since(%q) err = %v, want ErrBadCursor", c, err)
		}
	}
}

func TestWaitReturnsBufferedEvent(t *testing.T) {
	b := newBuffer(10, "boot")
	b.Append("MESSAGE_CREATE", msg("5", "7"))
	ev, next, gap, err := b.Wait(context.Background(), "boot:0", Filter{Types: []string{"MESSAGE_CREATE"}})
	if err != nil || ev == nil || ev.Seq != 1 || next != "boot:1" || gap {
		t.Fatalf("ev=%v next=%s gap=%v err=%v", ev, next, gap, err)
	}
}

func TestWaitBlocksUntilMatchingAppend(t *testing.T) {
	b := newBuffer(10, "boot")
	done := make(chan *Event, 1)
	go func() {
		ev, _, _, _ := b.Wait(context.Background(), "", Filter{ChannelID: "5", ExcludeAuthorID: "7"})
		done <- ev
	}()
	time.Sleep(20 * time.Millisecond)
	b.Append("MESSAGE_CREATE", msg("5", "7")) // own message: ignored
	b.Append("MESSAGE_CREATE", msg("6", "8")) // other channel: ignored
	b.Append("MESSAGE_CREATE", msg("5", "8")) // match
	select {
	case ev := <-done:
		if ev == nil || ev.Seq != 3 {
			t.Fatalf("ev = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("Wait did not wake")
	}
}

func TestWaitTimeout(t *testing.T) {
	b := newBuffer(10, "boot")
	b.Append("MESSAGE_CREATE", msg("6", "8"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	ev, next, _, err := b.Wait(ctx, "boot:0", Filter{ChannelID: "5"})
	if ev != nil || !errors.Is(err, context.DeadlineExceeded) || next != "boot:1" {
		t.Fatalf("ev=%v next=%s err=%v", ev, next, err)
	}
}

func TestCloseWakesWaiters(t *testing.T) {
	b := newBuffer(10, "boot")
	errc := make(chan error, 1)
	go func() {
		_, _, _, err := b.Wait(context.Background(), "", Filter{})
		errc <- err
	}()
	time.Sleep(20 * time.Millisecond)
	b.Close()
	select {
	case err := <-errc:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not wake the waiter")
	}
	b.Append("MESSAGE_CREATE", msg("5", "7")) // must not panic after Close
}

func TestNewBufferBootIDsDiffer(t *testing.T) {
	if NewBuffer(1).Cursor() == NewBuffer(1).Cursor() {
		t.Fatal("two buffers share a boot id")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/events/`
Expected: FAIL — `undefined: newBuffer`.

- [ ] **Step 3: Implement the buffer**

`internal/events/buffer.go`:

```go
// Package events buffers Gateway events for configured bots and serves them
// to the event tools.
//
// The buffer is a bounded ring shared read-only by every client; there is no
// per-client state. A client holds a cursor "<boot-id>:<seq>" naming the last
// event it saw. The boot ID is random per process, so a cursor from before a
// restart — or one older than the oldest buffered event — is reported as a
// gap instead of silently skipping events.
package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrClosed is returned by Wait once the buffer is closed.
	ErrClosed = errors.New("event buffer closed")
	// ErrBadCursor is wrapped by errors about malformed cursors.
	ErrBadCursor = errors.New("invalid cursor")
)

// Event is one buffered Gateway dispatch.
type Event struct {
	Seq       uint64          `json:"seq"`
	Type      string          `json:"type"`
	Time      time.Time       `json:"time"`
	GuildID   string          `json:"guild_id,omitempty"`
	ChannelID string          `json:"channel_id,omitempty"`
	AuthorID  string          `json:"author_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// Filter selects events. Empty fields match everything.
type Filter struct {
	Types           []string
	GuildID         string
	ChannelID       string
	AuthorID        string
	ExcludeAuthorID string
}

// Match reports whether e passes the filter.
func (f Filter) Match(e Event) bool {
	switch {
	case len(f.Types) > 0 && !slices.Contains(f.Types, e.Type):
		return false
	case f.GuildID != "" && e.GuildID != f.GuildID:
		return false
	case f.ChannelID != "" && e.ChannelID != f.ChannelID:
		return false
	case f.AuthorID != "" && e.AuthorID != f.AuthorID:
		return false
	case f.ExcludeAuthorID != "" && e.AuthorID == f.ExcludeAuthorID:
		return false
	}
	return true
}

// Page is a batch of events and the cursor to continue from.
type Page struct {
	Events     []Event
	NextCursor string
	Gap        bool
}

// Buffer is a bounded, concurrency-safe ring of events.
type Buffer struct {
	mu      sync.Mutex
	bootID  string
	ring    []Event
	start   int // index of the oldest event
	count   int
	lastSeq uint64
	changed chan struct{} // closed and replaced on every Append
	closed  bool
}

// NewBuffer returns a buffer holding the last size events.
func NewBuffer(size int) *Buffer {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return newBuffer(size, hex.EncodeToString(b[:]))
}

func newBuffer(size int, bootID string) *Buffer {
	return &Buffer{bootID: bootID, ring: make([]Event, size), changed: make(chan struct{})}
}

// Append stores an event, dropping the oldest when full, and wakes waiters.
func (b *Buffer) Append(typ string, data json.RawMessage) Event {
	ids := extractIDs(typ, data)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return Event{}
	}
	b.lastSeq++
	e := Event{Seq: b.lastSeq, Type: typ, Time: time.Now().UTC(), GuildID: ids.guild, ChannelID: ids.channel, AuthorID: ids.author, Data: data}
	if b.count < len(b.ring) {
		b.ring[(b.start+b.count)%len(b.ring)] = e
		b.count++
	} else {
		b.ring[b.start] = e
		b.start = (b.start + 1) % len(b.ring)
	}
	close(b.changed)
	b.changed = make(chan struct{})
	return e
}

// Cursor returns the cursor of the newest event (seq 0 when empty).
func (b *Buffer) Cursor() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cursor(b.lastSeq)
}

func (b *Buffer) cursor(seq uint64) string { return b.bootID + ":" + strconv.FormatUint(seq, 10) }

// Since returns up to limit events matching f after cursor. With an empty
// cursor it returns the latest limit matching events.
func (b *Buffer) Since(cursor string, f Filter, limit int) (Page, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.since(cursor, f, limit)
}

func (b *Buffer) since(cursor string, f Filter, limit int) (Page, error) {
	if cursor == "" {
		var out []Event
		for i := b.count - 1; i >= 0 && len(out) < limit; i-- {
			if e := b.at(i); f.Match(e) {
				out = append(out, e)
			}
		}
		slices.Reverse(out)
		return Page{Events: out, NextCursor: b.cursor(b.lastSeq)}, nil
	}

	after, gap, err := b.parse(cursor)
	if err != nil {
		return Page{}, err
	}
	next := b.lastSeq
	var out []Event
	for i := 0; i < b.count; i++ {
		e := b.at(i)
		if e.Seq <= after || !f.Match(e) {
			continue
		}
		out = append(out, e)
		if len(out) == limit {
			next = e.Seq
			break
		}
	}
	if next < after {
		next = after
	}
	return Page{Events: out, NextCursor: b.cursor(next), Gap: gap}, nil
}

// at returns the i-th oldest event.
func (b *Buffer) at(i int) Event { return b.ring[(b.start+i)%len(b.ring)] }

// parse returns the sequence to read after and whether events were missed.
func (b *Buffer) parse(cursor string) (uint64, bool, error) {
	boot, seqStr, ok := strings.Cut(cursor, ":")
	if !ok || boot == "" {
		return 0, false, fmt.Errorf("%w: %q (want the next_cursor of a previous call)", ErrBadCursor, cursor)
	}
	seq, err := strconv.ParseUint(seqStr, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("%w: %q (want the next_cursor of a previous call)", ErrBadCursor, cursor)
	}
	if boot != b.bootID {
		return 0, true, nil // cursor from before a restart
	}
	if seq > b.lastSeq {
		return 0, false, fmt.Errorf("%w: %q is ahead of the newest event", ErrBadCursor, cursor)
	}
	oldest := b.lastSeq + 1
	if b.count > 0 {
		oldest = b.at(0).Seq
	}
	return seq, seq+1 < oldest, nil
}

// Wait blocks until an event matching f arrives after cursor (after the
// newest event when cursor is empty). It returns the cursor to resume from
// whatever happens, and whether any gap was crossed.
func (b *Buffer) Wait(ctx context.Context, cursor string, f Filter) (*Event, string, bool, error) {
	if cursor == "" {
		cursor = b.Cursor()
	}
	gap := false
	for {
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return nil, cursor, gap, ErrClosed
		}
		p, err := b.since(cursor, f, 1)
		ch := b.changed
		b.mu.Unlock()
		if err != nil {
			return nil, cursor, gap, err
		}
		gap = gap || p.Gap
		if len(p.Events) == 1 {
			return &p.Events[0], p.NextCursor, gap, nil
		}
		cursor = p.NextCursor
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, cursor, gap, ctx.Err()
		}
	}
}

// Close wakes every waiter with ErrClosed; later Appends are dropped.
func (b *Buffer) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	close(b.changed)
}

type eventIDs struct{ guild, channel, author string }

// extractIDs reads the IDs the filters use. Message events carry author.id,
// reaction events user_id, and channel/thread events their own id.
func extractIDs(typ string, data json.RawMessage) eventIDs {
	var v struct {
		ID        string `json:"id"`
		GuildID   string `json:"guild_id"`
		ChannelID string `json:"channel_id"`
		UserID    string `json:"user_id"`
		Author    *struct {
			ID string `json:"id"`
		} `json:"author"`
	}
	_ = json.Unmarshal(data, &v)
	ids := eventIDs{guild: v.GuildID, channel: v.ChannelID, author: v.UserID}
	if v.Author != nil {
		ids.author = v.Author.ID
	}
	if ids.channel == "" && (strings.HasPrefix(typ, "CHANNEL_") || strings.HasPrefix(typ, "THREAD_")) {
		ids.channel = v.ID
	}
	return ids
}
```

- [ ] **Step 4: Run the tests with the race detector**

Run: `go test -race ./internal/events/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/events
git commit -m "feat(events): add bounded event buffer with cursors and waiters" -m "Gateway events for configured bots will land in a ring buffer holding
the last buffer_size events. Clients read it with a cursor naming the
last event they saw; the cursor carries a random boot ID so a cursor from
before a restart, or one older than the oldest buffered event, is
reported as a gap rather than silently skipping events.

Since pages through events matching a type, guild, channel or author
filter. Wait blocks until a matching event arrives, returning the cursor
to resume from on timeout, and Close wakes every waiter for shutdown."
```

---
### Task 7: Gateway adapter

**Files:**
- Create: `internal/events/gateway.go`
- Test: `internal/events/gateway_test.go`

**Interfaces:**
- Consumes: `Buffer`, `Buffer.Append` (Task 6); `audit.Logger.Gateway` (Task 5); `intents.Mask` (Task 1); `*discordgo.Session` from `discord.Client.Session()` (Task 3).
- Produces:
  - `events.NewGateway(instance string, session *discordgo.Session, mask intents.Mask, buf *Buffer, log *audit.Logger) *Gateway`
  - `(*Gateway).Start(readyTimeout time.Duration) error` — opens the session and waits for the first `READY`
  - `(*Gateway).Close() error`
  - Synthetic event type `events.TypeGatewayReconnected = "gateway_reconnected"`

The Gateway itself cannot be faked with `discordtest` (it is a WebSocket to `gateway.discord.gg`), so this task tests the event handling by calling `handle` directly; `Start` is exercised manually against a real bot (Task 18, Step 6).

- [ ] **Step 1: Write the failing tests**

`internal/events/gateway_test.go`:

```go
package events

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/intents"
)

func newTestGateway(t *testing.T) (*Gateway, *Buffer, *bytes.Buffer) {
	t.Helper()
	s, err := discordgo.New("Bot a.b.c")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	buf := newBuffer(10, "boot")
	m, _ := intents.Parse([]string{"guilds", "guild_messages", "message_content"})
	return NewGateway("assistant", s, m, buf, audit.New(&logs)), buf, &logs
}

func types(t *testing.T, b *Buffer) []string {
	t.Helper()
	p, err := b.Since("boot:0", Filter{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range p.Events {
		out = append(out, e.Type)
	}
	return out
}

func TestNewGatewaySetsIntents(t *testing.T) {
	g, _, _ := newTestGateway(t)
	if want := discordgo.Intent(1<<0 | 1<<9 | 1<<15); g.session.Identify.Intents != want {
		t.Fatalf("intents = %b, want %b", g.session.Identify.Intents, want)
	}
}

func TestGatewayHandling(t *testing.T) {
	g, buf, logs := newTestGateway(t)

	g.handle("READY", json.RawMessage(`{"guilds":[{"id":"1","unavailable":true},{"id":"2","unavailable":true}]}`))
	select {
	case <-g.ready:
	default:
		t.Fatal("first READY must mark the gateway ready")
	}
	g.handle("GUILD_CREATE", json.RawMessage(`{"id":"1"}`)) // startup burst: dropped
	g.handle("GUILD_CREATE", json.RawMessage(`{"id":"3"}`)) // joined a new guild: kept
	g.handle("MESSAGE_CREATE", json.RawMessage(`{"channel_id":"5","author":{"id":"7"}}`))
	g.handle("RESUMED", json.RawMessage(`{}`)) // clean resume: no marker
	g.handle("GUILD_CREATE", json.RawMessage(`{"id":"1"}`)) // already consumed: kept now

	g.handle("READY", json.RawMessage(`{"guilds":[{"id":"1"}]}`)) // new session after a failed resume
	g.handle("GUILD_CREATE", json.RawMessage(`{"id":"1"}`))       // burst of the new session: dropped

	want := []string{"GUILD_CREATE", "MESSAGE_CREATE", "GUILD_CREATE", TypeGatewayReconnected}
	if got := types(t, buf); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("buffered = %v, want %v", got, want)
	}
	for _, msg := range []string{`"msg":"gateway_connected"`, `"msg":"gateway_resumed"`, `"msg":"gateway_reconnected"`} {
		if !strings.Contains(logs.String(), msg) {
			t.Errorf("logs missing %s:\n%s", msg, logs.String())
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/events/ -run Gateway`
Expected: FAIL — `undefined: NewGateway`.

- [ ] **Step 3: Implement the gateway adapter**

`internal/events/gateway.go`:

```go
package events

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/intents"
)

// TypeGatewayReconnected marks a point where events may be missing: the
// session was re-identified instead of resumed. Lowercase so it can never
// collide with a Discord dispatch type.
const TypeGatewayReconnected = "gateway_reconnected"

// Gateway feeds one bot's Gateway dispatches into a Buffer. discordgo handles
// heartbeats, resumes and reconnects; this type decides what is buffered.
type Gateway struct {
	instance string
	session  *discordgo.Session
	buf      *Buffer
	log      *audit.Logger

	mu            sync.Mutex
	readyCount    int
	startupGuilds map[string]bool
	ready         chan struct{}
}

// NewGateway prepares a session with the given intents. Nothing connects until
// Start.
func NewGateway(instance string, session *discordgo.Session, mask intents.Mask, buf *Buffer, log *audit.Logger) *Gateway {
	session.Identify.Intents = discordgo.Intent(mask)
	session.StateEnabled = false
	g := &Gateway{instance: instance, session: session, buf: buf, log: log, ready: make(chan struct{})}
	session.AddHandler(func(_ *discordgo.Session, e *discordgo.Event) { g.handle(e.Type, e.RawData) })
	session.AddHandler(func(_ *discordgo.Session, _ *discordgo.Disconnect) {
		g.log.Gateway(g.instance, "gateway_disconnected", nil)
	})
	return g
}

// Start opens the Gateway and waits for the first READY. A bad token or a
// disallowed intent never produces READY, so this fails after readyTimeout
// instead of letting discordgo retry forever in the background.
func (g *Gateway) Start(readyTimeout time.Duration) error {
	if err := g.session.Open(); err != nil {
		return fmt.Errorf("instance %q: open gateway: %w", g.instance, err)
	}
	select {
	case <-g.ready:
		return nil
	case <-time.After(readyTimeout):
		_ = g.session.Close()
		return fmt.Errorf("instance %q: gateway not ready within %s (check the bot token and intents)", g.instance, readyTimeout)
	}
}

// Close disconnects the Gateway.
func (g *Gateway) Close() error { return g.session.Close() }

// handle buffers dispatches except lifecycle ones: READY and RESUMED, and
// the GUILD_CREATE burst that follows each READY for the guilds it listed.
func (g *Gateway) handle(typ string, raw json.RawMessage) {
	switch typ {
	case "":
		return
	case "READY":
		var r struct {
			Guilds []struct {
				ID string `json:"id"`
			} `json:"guilds"`
		}
		_ = json.Unmarshal(raw, &r)
		g.mu.Lock()
		g.startupGuilds = make(map[string]bool, len(r.Guilds))
		for _, gu := range r.Guilds {
			g.startupGuilds[gu.ID] = true
		}
		g.readyCount++
		first := g.readyCount == 1
		g.mu.Unlock()
		if first {
			g.log.Gateway(g.instance, "gateway_connected", nil)
			close(g.ready)
			return
		}
		g.log.Gateway(g.instance, "gateway_reconnected", nil)
		g.buf.Append(TypeGatewayReconnected, json.RawMessage(`{}`))
		return
	case "RESUMED":
		g.log.Gateway(g.instance, "gateway_resumed", nil)
		return
	case "GUILD_CREATE":
		var gc struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &gc)
		g.mu.Lock()
		startup := g.startupGuilds[gc.ID]
		delete(g.startupGuilds, gc.ID)
		g.mu.Unlock()
		if startup {
			return
		}
	}
	g.buf.Append(typ, raw)
}
```

- [ ] **Step 4: Run the events tests**

Run: `go test -race ./internal/events/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
go mod tidy
git add go.mod go.sum internal/events
git commit -m "feat(events): feed gateway dispatches into the buffer" -m "A configured bot with events opens a discordgo Gateway session with its
intents. Every dispatch is buffered except READY, RESUMED and the
GUILD_CREATE burst that follows READY for the guilds it listed, so the
buffer holds what happened rather than connection noise.

Start waits for the first READY and fails after a timeout, since a bad
token or a disallowed intent never produces one. A later READY means
the session could not be resumed: a gateway_reconnected marker event
tells readers events may be missing. Connects, resumes, reconnects and
disconnects are logged."
```

---
### Task 8: Principals and credential resolution

**Files:**
- Create: `internal/auth/principal.go`, `internal/auth/resolver.go`
- Test: `internal/auth/resolver_test.go`

**Interfaces:**
- Consumes: `credential.ParseWebhook`, `credential.IsBotToken`, `credential.Webhook` (Task 1); `discord.Client`, `discord.Options`, `discord.NewBot`, `discord.NewWebhook`, `(*Client).VerifyBot`, `(*Client).VerifyWebhook`, `discord.APIError` (Tasks 3–4); `audit.Actor` (Task 5); `events.Buffer` (Task 6).
- Produces:
  - `auth.Kind` with `KindInstance = "instance"`, `KindDirectBot = "direct_bot"`, `KindDirectWebhook = "direct_webhook"`
  - `auth.Capability` with `CapBot`, `CapBotEvents`, `CapWebhook`
  - `auth.Principal{Kind Kind; Capability Capability; Instance string; CredentialHash string; Client *discord.Client; BotUserID string; Webhook credential.Webhook; Events *events.Buffer}`
  - `(*Principal).Actor() audit.Actor`, `(*Principal).Invalidate()`
  - `auth.WithPrincipal(ctx, *Principal) context.Context`, `auth.FromContext(ctx) (*Principal, bool)`
  - `auth.Credential(r *http.Request) (value, header string, ok bool)`; `auth.HeaderAuthorization`, `auth.HeaderAPIKey`
  - `auth.Rejection` with `RejectMissing`, `RejectUnknown`, `RejectDiscord`, `RejectRecentFailure`, `RejectUnavailable`
  - `auth.DirectOptions{Enabled bool; CacheTTL, FailureTTL time.Duration; Discord discord.Options}`
  - `auth.InstanceEntry{Tokens []string; Principal *Principal}`
  - `auth.NewResolver(entries []InstanceEntry, direct DirectOptions) (*Resolver, error)`
  - `(*Resolver).Resolve(ctx context.Context, cred string) (*Principal, Rejection)` — `Rejection` is `""` on success

- [ ] **Step 1: Write the failing tests**

`internal/auth/resolver_test.go`:

```go
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
	instToken   = "config-token-config-token-config"
	botTok      = "MTIz.GAbC.directdirectdirect"
	webhookID   = "123456789012345678"
	webhookTok  = "AbC-def_GHI123"
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/auth/`
Expected: FAIL — `undefined: NewResolver`.

- [ ] **Step 3: Implement principals**

`internal/auth/principal.go`:

```go
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
```

- [ ] **Step 4: Implement the resolver**

`internal/auth/resolver.go`:

```go
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Hellhium/discord-mcp/internal/credential"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

// Headers a credential is read from.
const (
	HeaderAuthorization = "Authorization"
	HeaderAPIKey        = "X-API-Key"
)

// Credential reads the client credential: a well-formed Authorization: Bearer
// header wins, otherwise X-API-Key. header names where it was found.
func Credential(r *http.Request) (value, header string, ok bool) {
	const bearer = "bearer "
	if h := r.Header.Get(HeaderAuthorization); len(h) >= len(bearer) && strings.EqualFold(h[:len(bearer)], bearer) {
		if v := strings.TrimSpace(h[len(bearer):]); v != "" {
			return v, HeaderAuthorization, true
		}
	}
	if v := strings.TrimSpace(r.Header.Get(HeaderAPIKey)); v != "" {
		return v, HeaderAPIKey, true
	}
	return "", "", false
}

// Rejection is why a credential was refused. It is logged, never sent to the
// client: every rejection gets the same 401.
type Rejection string

const (
	RejectMissing       Rejection = "missing_credential"
	RejectUnknown       Rejection = "unknown_token"
	RejectDiscord       Rejection = "discord_rejected"
	RejectRecentFailure Rejection = "recently_rejected"
	RejectUnavailable   Rejection = "discord_unavailable"
)

const defaultFailureTTL = time.Minute

// DirectOptions configures direct authentication.
type DirectOptions struct {
	Enabled bool
	// CacheTTL is how long a verified credential stays cached without use.
	CacheTTL time.Duration
	// FailureTTL is how long a credential Discord rejected is refused without
	// asking Discord again (default one minute). Discord bans an IP after
	// 10,000 invalid requests in 10 minutes; a client retrying a bad token
	// must not get the server banned.
	FailureTTL time.Duration
	Discord    discord.Options
}

// InstanceEntry maps config tokens to a prebuilt principal.
type InstanceEntry struct {
	Tokens    []string
	Principal *Principal
}

type indexed struct {
	token string
	p     *Principal
}

type cached struct {
	p        *Principal
	lastUsed time.Time
}

// Resolver resolves credentials. It is safe for concurrent use.
type Resolver struct {
	byHash map[[sha256.Size]byte]indexed
	direct DirectOptions
	now    func() time.Time

	mu        sync.Mutex
	ok        map[[sha256.Size]byte]*cached
	bad       map[[sha256.Size]byte]time.Time
	lastSweep time.Time
}

// NewResolver indexes config tokens by SHA-256. A token is never used as a
// map key in plaintext, and a hash hit is confirmed in constant time.
func NewResolver(entries []InstanceEntry, direct DirectOptions) (*Resolver, error) {
	if direct.FailureTTL <= 0 {
		direct.FailureTTL = defaultFailureTTL
	}
	r := &Resolver{
		byHash: make(map[[sha256.Size]byte]indexed),
		direct: direct,
		now:    time.Now,
		ok:     make(map[[sha256.Size]byte]*cached),
		bad:    make(map[[sha256.Size]byte]time.Time),
	}
	for _, e := range entries {
		for _, tok := range e.Tokens {
			if tok == "" {
				return nil, errors.New("empty instance token")
			}
			h := sha256.Sum256([]byte(tok))
			if _, dup := r.byHash[h]; dup {
				return nil, fmt.Errorf("instance %q: token already used by another instance", e.Principal.Instance)
			}
			r.byHash[h] = indexed{token: tok, p: e.Principal}
		}
	}
	return r, nil
}

// Resolve returns the principal for cred, or the reason it was refused.
func (r *Resolver) Resolve(ctx context.Context, cred string) (*Principal, Rejection) {
	if cred == "" {
		return nil, RejectMissing
	}
	h := sha256.Sum256([]byte(cred))
	if in, ok := r.byHash[h]; ok && subtle.ConstantTimeCompare([]byte(cred), []byte(in.token)) == 1 {
		return in.p, ""
	}
	if !r.direct.Enabled {
		return nil, RejectUnknown
	}
	wh, isWebhook := credential.ParseWebhook(cred)
	if !isWebhook && !credential.IsBotToken(cred) {
		return nil, RejectUnknown
	}
	key := h
	if isWebhook {
		// URL and id/token forms of one webhook share an entry.
		key = sha256.Sum256([]byte("webhook:" + wh.ID + "/" + wh.Token))
	}

	now := r.now()
	r.mu.Lock()
	r.sweepLocked(now)
	if c, ok := r.ok[key]; ok && now.Sub(c.lastUsed) <= r.direct.CacheTTL {
		c.lastUsed = now
		r.mu.Unlock()
		return c.p, ""
	}
	if t, ok := r.bad[key]; ok && now.Sub(t) < r.direct.FailureTTL {
		r.mu.Unlock()
		return nil, RejectRecentFailure
	}
	r.mu.Unlock()

	p, rej := r.verify(ctx, cred, wh, isWebhook, key)

	r.mu.Lock()
	defer r.mu.Unlock()
	switch rej {
	case "":
		if c, ok := r.ok[key]; ok { // a concurrent request verified it first
			c.lastUsed = now
			return c.p, ""
		}
		r.ok[key] = &cached{p: p, lastUsed: now}
	case RejectDiscord:
		r.bad[key] = now
	}
	return p, rej
}

func (r *Resolver) verify(ctx context.Context, cred string, wh credential.Webhook, isWebhook bool, key [sha256.Size]byte) (*Principal, Rejection) {
	hash := "sha256:" + hex.EncodeToString(key[:4])
	if isWebhook {
		c := discord.NewWebhook(r.direct.Discord)
		if _, err := c.VerifyWebhook(ctx, wh); err != nil {
			return nil, classify(err)
		}
		return &Principal{Kind: KindDirectWebhook, Capability: CapWebhook, CredentialHash: hash, Client: c, Webhook: wh, invalidate: r.invalidator(key)}, ""
	}
	c := discord.NewBot(cred, r.direct.Discord)
	id, err := c.VerifyBot(ctx)
	if err != nil {
		return nil, classify(err)
	}
	return &Principal{Kind: KindDirectBot, Capability: CapBot, CredentialHash: hash, Client: c, BotUserID: id.UserID, invalidate: r.invalidator(key)}, ""
}

// classify separates "Discord says this credential is invalid" (cached) from
// "Discord could not answer" (not cached).
func classify(err error) Rejection {
	var ae *discord.APIError
	if errors.As(err, &ae) && (ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden || ae.Status == http.StatusNotFound) {
		return RejectDiscord
	}
	return RejectUnavailable
}

func (r *Resolver) invalidator(key [sha256.Size]byte) func() {
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.ok, key)
		r.bad[key] = r.now()
	}
}

// sweepLocked drops expired entries at most once per FailureTTL.
func (r *Resolver) sweepLocked(now time.Time) {
	if now.Sub(r.lastSweep) < r.direct.FailureTTL {
		return
	}
	r.lastSweep = now
	for k, c := range r.ok {
		if now.Sub(c.lastUsed) > r.direct.CacheTTL {
			delete(r.ok, k)
		}
	}
	for k, t := range r.bad {
		if now.Sub(t) >= r.direct.FailureTTL {
			delete(r.bad, k)
		}
	}
}
```

- [ ] **Step 5: Run the tests**

Run: `go test -race ./internal/auth/`
Expected: PASS. `TestDirectUnavailableNotCached` asserts `>= 2` requests because discordgo retries a 502 but not a 500; with a 500 there is exactly one request per resolve.

- [ ] **Step 6: Commit**

```bash
git add internal/auth
git commit -m "feat(auth): resolve config tokens and direct discord credentials" -m "A request credential comes from Authorization: Bearer, or X-API-Key when
no bearer credential is present. Config tokens are matched by SHA-256
with a constant-time confirmation and never contact Discord.

With direct auth enabled, a value shaped like a webhook (URL or id/token)
or a bot token is verified against Discord and cached for cache_ttl
since last use, one Discord client per credential; the URL and id/token
forms of a webhook share an entry. A credential Discord rejects is
refused for a minute without asking again, so a retrying client cannot
push the server toward Discord's invalid-request ban. Anything else is
refused without contacting Discord. A principal whose credential is
later rejected mid-call can evict itself."
```

---
### Task 9: Tool framework — wrapper, arguments, shared helpers

**Files:**
- Create: `internal/tools/tools.go`, `internal/tools/args.go`, `internal/tools/common.go`, `internal/tools/render.go`, `internal/tools/sets.go`
- Test: `internal/tools/harness_test.go`, `internal/tools/tools_test.go`

**Interfaces:**
- Consumes: `auth.Principal`, `auth.FromContext`, `auth.WithPrincipal`, `auth.Capability` (Task 8); `audit.Logger`, `audit.Action`, `audit.Targets`, `audit.SanitizeArgs`, outcomes (Task 5); `discord.NewRecorder`, `discord.WithRecorder`, `discord.ArgError`, `discord.IsUnauthorized`, `discord.RateLimitedError`, `discord.File`, `discord.NewBot`, `discord.Options` (Tasks 3–4); `events.NewBuffer` (Task 6); `discordtest` (Task 3).
- Produces (used by Tasks 10–17):
  - `tools.Handler` = `func(ctx context.Context, p *auth.Principal, a Args) (string, error)`
  - `tools.Tool{Def mcp.Tool; Handle Handler}`
  - `tools.Register(s *server.MCPServer, log *audit.Logger, list []Tool)`
  - `tools.Args` (`map[string]any`) with `String(key) (string, error)`, `RequiredString(key) (string, error)`, `ID(key) (string, error)`, `OptionalID(key) (string, error)`, `Int(key string, def, min, max int) (int, error)`, `Bool(key string, def bool) (bool, error)`, `Object(key) (map[string]any, error)`, `List(key) ([]any, error)`, `StringList(key) ([]string, error)`, `OneOf(keys ...string) (string, error)`
  - `tools.argErr(format string, a ...any) error` (returns `*discord.ArgError`)
  - Schema helpers: `readOnly()`, `mutating()`, `destructive()` (`mcp.ToolOption`); `idParam(name, desc string)`, `optIDParam(name, desc string)`, `withFormat()`, `withReason()`, `withAllowMentions()`, `withAttachments()`, `withEmbeds()`
  - `formatArg(a Args) (string, error)` → `"text"` or `"json"`
  - `messagePayload(a Args, needBody bool, allowFiles bool) (map[string]any, []discord.File, error)`
  - `output[T any](body json.RawMessage, format string, render func(T) string) (string, error)`
  - Shared JSON shapes and renderers: `userJSON`, `messageJSON`, `channelJSON`; `displayName(userJSON) string`, `renderMessage(messageJSON) string`, `renderMessages([]messageJSON) string` (oldest first), `renderChannel(channelJSON) string`, `channelTypeName(int) string`
  - `tools.BotTools() []Tool`, `tools.EventTools() []Tool`, `tools.WebhookTools() []Tool` (empty now; Tasks 10–16 fill them)
  - Test harness (package `tools`, `_test.go`): `newHarness(t, auth.Capability) *harness`; `(*harness).call(Tool, map[string]any) (text string, isError bool)`; `(*harness).handle(pattern string, h http.HandlerFunc)`; `(*harness).last() discordtest.Request`; `(*harness).lastBody() map[string]any`; `(*harness).lastAudit() map[string]any`; `(*harness).requests() int`; constants `hBotUserID = "42"`, `hWebhookID = "555"`, `hWebhookToken = "hooktoken"`

- [ ] **Step 1: Write the test harness**

`internal/tools/harness_test.go`:

```go
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
```

- [ ] **Step 2: Write the failing framework tests**

`internal/tools/tools_test.go`:

```go
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
		"nothing to send":  {},
		"bad base64":       {"content": "x", "attachments": []any{map[string]any{"filename": "a", "content_base64": "!!"}}},
		"path in filename": {"content": "x", "attachments": []any{map[string]any{"filename": "../a", "content_base64": data}}},
		"too many embeds":  {"embeds": make([]any, 11)},
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
			Timestamp: time.Date(2026, 9, 14, 11, 2, 0, 0, time.UTC),
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
```

- [ ] **Step 3: Run to verify failure**

Run: `go get github.com/mark3labs/mcp-go@v0.57.0 && go test ./internal/tools/`
Expected: FAIL — `undefined: Handler`.

- [ ] **Step 4: Implement the wrapper and registration**

`internal/tools/tools.go`:

```go
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
			Targets:  audit.Targets(args),
			Args:     audit.SanitizeArgs(args),
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
```

- [ ] **Step 5: Implement argument accessors**

`internal/tools/args.go`:

```go
package tools

import (
	"fmt"
	"math"
	"strings"

	"github.com/Hellhium/discord-mcp/internal/credential"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

// Args are a tool call's arguments as decoded JSON. Every accessor returns a
// *discord.ArgError the LLM can act on.
type Args map[string]any

func argErr(format string, a ...any) error {
	return &discord.ArgError{Msg: fmt.Sprintf(format, a...)}
}

func (a Args) present(key string) (any, bool) {
	v, ok := a[key]
	return v, ok && v != nil
}

// String returns an optional string ("" when absent).
func (a Args) String(key string) (string, error) {
	v, ok := a.present(key)
	if !ok {
		return "", nil
	}
	s, isStr := v.(string)
	if !isStr {
		return "", argErr("%s must be a string", key)
	}
	return s, nil
}

// RequiredString returns a non-empty string.
func (a Args) RequiredString(key string) (string, error) {
	s, err := a.String(key)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(s) == "" {
		return "", argErr("%s is required", key)
	}
	return s, nil
}

// ID returns a required Discord ID. IDs must be strings: JSON numbers lose
// precision above 2^53, which every modern snowflake exceeds.
func (a Args) ID(key string) (string, error) {
	v, ok := a.present(key)
	if !ok {
		return "", argErr("%s is required", key)
	}
	return checkID(key, v)
}

// OptionalID returns a Discord ID or "".
func (a Args) OptionalID(key string) (string, error) {
	v, ok := a.present(key)
	if !ok {
		return "", nil
	}
	if s, isStr := v.(string); isStr && s == "" {
		return "", nil
	}
	return checkID(key, v)
}

func checkID(key string, v any) (string, error) {
	s, isStr := v.(string)
	if !isStr {
		return "", argErr("%s must be a Discord ID passed as a string, e.g. \"123456789012345678\"", key)
	}
	if !credential.IsSnowflake(s) {
		return "", argErr("%s must be a Discord ID (digits only), got %q", key, s)
	}
	return s, nil
}

// Int returns an integer in [min, max], or def when absent.
func (a Args) Int(key string, def, min, max int) (int, error) {
	v, ok := a.present(key)
	if !ok {
		return def, nil
	}
	f, isNum := v.(float64)
	if !isNum || f != math.Trunc(f) {
		return 0, argErr("%s must be an integer", key)
	}
	if f < float64(min) || f > float64(max) {
		return 0, argErr("%s must be between %d and %d", key, min, max)
	}
	return int(f), nil
}

// Bool returns a boolean, or def when absent.
func (a Args) Bool(key string, def bool) (bool, error) {
	v, ok := a.present(key)
	if !ok {
		return def, nil
	}
	b, isBool := v.(bool)
	if !isBool {
		return false, argErr("%s must be true or false", key)
	}
	return b, nil
}

// Object returns a JSON object, or nil when absent.
func (a Args) Object(key string) (map[string]any, error) {
	v, ok := a.present(key)
	if !ok {
		return nil, nil
	}
	m, isObj := v.(map[string]any)
	if !isObj {
		return nil, argErr("%s must be an object", key)
	}
	return m, nil
}

// List returns a JSON array, or nil when absent.
func (a Args) List(key string) ([]any, error) {
	v, ok := a.present(key)
	if !ok {
		return nil, nil
	}
	l, isList := v.([]any)
	if !isList {
		return nil, argErr("%s must be an array", key)
	}
	return l, nil
}

// StringList returns an array of strings, or nil when absent.
func (a Args) StringList(key string) ([]string, error) {
	l, err := a.List(key)
	if err != nil || l == nil {
		return nil, err
	}
	out := make([]string, len(l))
	for i, v := range l {
		s, isStr := v.(string)
		if !isStr {
			return nil, argErr("%s must be an array of strings", key)
		}
		out[i] = s
	}
	return out, nil
}

// OneOf returns which of keys is set, "" when none, and an error when more
// than one is.
func (a Args) OneOf(keys ...string) (string, error) {
	found := ""
	for _, k := range keys {
		if v, ok := a.present(k); ok && v != "" {
			if found != "" {
				return "", argErr("set at most one of %s", strings.Join(keys, ", "))
			}
			found = k
		}
	}
	return found, nil
}
```

- [ ] **Step 6: Implement shared schema and payload helpers**

`internal/tools/common.go`:

```go
package tools

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/discord"
)

const (
	maxAttachments = 10
	maxEmbeds      = 10
)

// Annotations. mcp.NewTool defaults to destructive, so every tool sets both.
func readOnly() mcp.ToolOption {
	return func(t *mcp.Tool) {
		mcp.WithReadOnlyHintAnnotation(true)(t)
		mcp.WithDestructiveHintAnnotation(false)(t)
	}
}

func mutating() mcp.ToolOption {
	return func(t *mcp.Tool) {
		mcp.WithReadOnlyHintAnnotation(false)(t)
		mcp.WithDestructiveHintAnnotation(false)(t)
	}
}

func destructive() mcp.ToolOption {
	return func(t *mcp.Tool) {
		mcp.WithReadOnlyHintAnnotation(false)(t)
		mcp.WithDestructiveHintAnnotation(true)(t)
	}
}

func idParam(name, desc string) mcp.ToolOption {
	return mcp.WithString(name, mcp.Required(), mcp.Description(desc+" (Discord ID, as a string)"))
}

func optIDParam(name, desc string) mcp.ToolOption {
	return mcp.WithString(name, mcp.Description(desc+" (Discord ID, as a string)"))
}

func withFormat() mcp.ToolOption {
	return mcp.WithString("format", mcp.Enum("text", "json"),
		mcp.Description("text (default): compact summary. json: Discord's object exactly as returned."))
}

func withReason() mcp.ToolOption {
	return mcp.WithString("reason", mcp.Description("Recorded in the server's Discord audit log"))
}

func withAllowMentions() mcp.ToolOption {
	return mcp.WithBoolean("allow_mentions", mcp.Description(
		"Default true: @everyone, @here, role and user mentions ping as usual. false: nothing pings; the text is unchanged."))
}

func withAttachments() mcp.ToolOption {
	return mcp.WithArray("attachments",
		mcp.Description("Files to upload (max 10). Content is base64; URLs are not fetched."),
		mcp.Items(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filename":       map[string]any{"type": "string"},
				"content_base64": map[string]any{"type": "string"},
				"description":    map[string]any{"type": "string"},
			},
			"required": []string{"filename", "content_base64"},
		}))
}

func withEmbeds() mcp.ToolOption {
	return mcp.WithArray("embeds", mcp.Description("Discord embed objects (max 10)"), mcp.Items(map[string]any{"type": "object"}))
}

func formatArg(a Args) (string, error) {
	f, err := a.String("format")
	if err != nil {
		return "", err
	}
	switch f {
	case "", "text":
		return "text", nil
	case "json":
		return "json", nil
	}
	return "", argErr("format must be text or json")
}

// messagePayload builds the JSON body for sending or editing a message.
// needBody requires at least one of content, embeds or attachments;
// allowFiles rejects attachments on tools that do not upload.
func messagePayload(a Args, needBody, allowFiles bool) (map[string]any, []discord.File, error) {
	body := map[string]any{}
	content, err := a.String("content")
	if err != nil {
		return nil, nil, err
	}
	if content != "" {
		body["content"] = content
	}
	embeds, err := a.List("embeds")
	if err != nil {
		return nil, nil, err
	}
	if len(embeds) > maxEmbeds {
		return nil, nil, argErr("at most %d embeds", maxEmbeds)
	}
	if embeds != nil {
		body["embeds"] = embeds
	}
	allow, err := a.Bool("allow_mentions", true)
	if err != nil {
		return nil, nil, err
	}
	if !allow {
		body["allowed_mentions"] = map[string]any{"parse": []string{}}
	}
	if _, ok := a.present("attachments"); ok && !allowFiles {
		return nil, nil, argErr("this tool does not accept attachments")
	}
	files, meta, err := attachments(a)
	if err != nil {
		return nil, nil, err
	}
	if len(files) > 0 {
		body["attachments"] = meta
	}
	if needBody && content == "" && len(embeds) == 0 && len(files) == 0 {
		return nil, nil, argErr("provide content, embeds or attachments")
	}
	return body, files, nil
}

func attachments(a Args) ([]discord.File, []map[string]any, error) {
	list, err := a.List("attachments")
	if err != nil {
		return nil, nil, err
	}
	if len(list) > maxAttachments {
		return nil, nil, argErr("at most %d attachments", maxAttachments)
	}
	var files []discord.File
	var meta []map[string]any
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, nil, argErr("attachments[%d] must be an object", i)
		}
		name, _ := m["filename"].(string)
		if name == "" || strings.ContainsAny(name, `/\`) {
			return nil, nil, argErr("attachments[%d].filename must be a plain file name", i)
		}
		b64, _ := m["content_base64"].(string)
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, nil, argErr("attachments[%d].content_base64 is not valid base64", i)
		}
		files = append(files, discord.File{Name: name, ContentType: mime.TypeByExtension(filepath.Ext(name)), Data: data})
		entry := map[string]any{"id": i, "filename": name}
		if d, ok := m["description"].(string); ok && d != "" {
			entry["description"] = d
		}
		meta = append(meta, entry)
	}
	return files, meta, nil
}

// output renders a Discord response body as text, or returns it as-is for
// format json.
func output[T any](body json.RawMessage, format string, render func(T) string) (string, error) {
	if format == "json" {
		return string(body), nil
	}
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("decode Discord response: %w", err)
	}
	return render(v), nil
}
```

- [ ] **Step 7: Implement shared renderers and the (empty) tool sets**

`internal/tools/render.go`:

```go
package tools

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type userJSON struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
	Bot        bool   `json:"bot"`
}

type attachmentJSON struct {
	Filename string `json:"filename"`
	URL      string `json:"url"`
}

type messageJSON struct {
	ID               string           `json:"id"`
	ChannelID        string           `json:"channel_id"`
	Author           userJSON         `json:"author"`
	Content          string           `json:"content"`
	Timestamp        time.Time        `json:"timestamp"`
	EditedTimestamp  *time.Time       `json:"edited_timestamp"`
	Attachments      []attachmentJSON `json:"attachments"`
	Embeds           []map[string]any `json:"embeds"`
	Pinned           bool             `json:"pinned"`
	MessageReference *struct {
		MessageID string `json:"message_id"`
	} `json:"message_reference"`
	Reactions []struct {
		Count int `json:"count"`
		Emoji struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"emoji"`
	} `json:"reactions"`
	Thread *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"thread"`
}

type channelJSON struct {
	ID       string `json:"id"`
	Type     int    `json:"type"`
	GuildID  string `json:"guild_id"`
	Name     string `json:"name"`
	Topic    string `json:"topic"`
	ParentID string `json:"parent_id"`
	Position int    `json:"position"`
	NSFW     bool   `json:"nsfw"`
	ThreadMetadata *struct {
		Archived bool `json:"archived"`
		Locked   bool `json:"locked"`
	} `json:"thread_metadata"`
}

const timeLayout = "2006-01-02 15:04"

func displayName(u userJSON) string {
	name := u.Username
	if u.GlobalName != "" {
		name = u.GlobalName
	}
	if u.Bot {
		name += " [bot]"
	}
	return name
}

func renderMessage(m messageJSON) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[%s] %s (%s) #%s: %s", m.Timestamp.UTC().Format(timeLayout), displayName(m.Author), m.Author.ID, m.ID, m.Content)
	if m.EditedTimestamp != nil {
		sb.WriteString(" (edited)")
	}
	if m.Pinned {
		sb.WriteString(" (pinned)")
	}
	if m.MessageReference != nil && m.MessageReference.MessageID != "" {
		fmt.Fprintf(&sb, "\n  reply to #%s", m.MessageReference.MessageID)
	}
	for _, a := range m.Attachments {
		fmt.Fprintf(&sb, "\n  attachment: %s %s", a.Filename, a.URL)
	}
	if len(m.Embeds) > 0 {
		fmt.Fprintf(&sb, "\n  %d embed(s)", len(m.Embeds))
	}
	if len(m.Reactions) > 0 {
		parts := make([]string, len(m.Reactions))
		for i, r := range m.Reactions {
			e := r.Emoji.Name
			if r.Emoji.ID != "" {
				e = r.Emoji.Name + ":" + r.Emoji.ID
			}
			parts[i] = fmt.Sprintf("%s x%d", e, r.Count)
		}
		fmt.Fprintf(&sb, "\n  reactions: %s", strings.Join(parts, ", "))
	}
	if m.Thread != nil {
		fmt.Fprintf(&sb, "\n  thread: %s (%s)", m.Thread.Name, m.Thread.ID)
	}
	return sb.String()
}

// renderMessages prints oldest first; Discord lists newest first.
func renderMessages(msgs []messageJSON) string {
	if len(msgs) == 0 {
		return "No messages."
	}
	sorted := slices.Clone(msgs)
	slices.SortStableFunc(sorted, func(a, b messageJSON) int { return a.Timestamp.Compare(b.Timestamp) })
	lines := make([]string, len(sorted))
	for i, m := range sorted {
		lines[i] = renderMessage(m)
	}
	return strings.Join(lines, "\n")
}

var channelTypes = map[int]string{
	0: "text", 1: "dm", 2: "voice", 3: "group-dm", 4: "category", 5: "announcement",
	10: "announcement-thread", 11: "public-thread", 12: "private-thread", 13: "stage", 14: "directory", 15: "forum", 16: "media",
}

func channelTypeName(t int) string {
	if n, ok := channelTypes[t]; ok {
		return n
	}
	return fmt.Sprintf("type-%d", t)
}

func renderChannel(c channelJSON) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "#%s (%s) %s", c.Name, c.ID, channelTypeName(c.Type))
	if c.ParentID != "" {
		fmt.Fprintf(&sb, " parent=%s", c.ParentID)
	}
	if c.NSFW {
		sb.WriteString(" nsfw")
	}
	if tm := c.ThreadMetadata; tm != nil {
		if tm.Archived {
			sb.WriteString(" archived")
		}
		if tm.Locked {
			sb.WriteString(" locked")
		}
	}
	if c.Topic != "" {
		fmt.Fprintf(&sb, " — %s", c.Topic)
	}
	return sb.String()
}
```

`internal/tools/sets.go`:

```go
package tools

// BotTools are the tools for principals with a bot credential.
func BotTools() []Tool {
	var out []Tool
	return out
}

// EventTools are added for configured bots with events.
func EventTools() []Tool {
	var out []Tool
	return out
}

// WebhookTools are the only tools a webhook principal gets.
func WebhookTools() []Tool {
	var out []Tool
	return out
}
```

- [ ] **Step 8: Run the tests**

Run: `go test ./internal/tools/`
Expected: PASS. If `TestMessagePayload` fails only on `ContentType`, check `mime.TypeByExtension(".txt")` on the machine (Go's built-in table returns `text/plain; charset=utf-8`).

- [ ] **Step 9: Commit**

```bash
go mod tidy
git add go.mod go.sum internal/tools
git commit -m "feat(tools): add the audited tool wrapper and shared helpers" -m "Every MCP tool will be a Handler receiving the request's principal and
typed arguments. One wrapper takes the principal from the request
context and refuses without one, records the Discord calls made, maps
errors to audit outcomes and tool errors the LLM can act on, evicts a
direct credential Discord answers 401 for, and writes exactly one audit
line per call.

Argument accessors reject IDs passed as JSON numbers, which lose
precision above 2^53. messagePayload implements allow_mentions (false
sends allowed_mentions with an empty parse list) and base64
attachments, and shared renderers print messages oldest first."
```

---
### Task 10: Discovery tools

**Files:**
- Create: `internal/tools/discovery.go`
- Modify: `internal/tools/sets.go` (`BotTools`)
- Test: `internal/tools/discovery_test.go`

**Interfaces:**
- Consumes: everything listed as produced by Task 9.
- Produces: `discoveryTools() []Tool` containing `discord_get_me`, `discord_list_guilds`, `discord_get_guild`, `discord_list_channels`, `discord_get_channel`; constructors `getMeTool()`, `listGuildsTool()`, `getGuildTool()`, `listChannelsTool()`, `getChannelTool()`.

- [ ] **Step 1: Write the failing tests**

`internal/tools/discovery_test.go`:

```go
package tools

import (
	"strings"
	"testing"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
)

func TestGetMe(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/users/@me", discordtest.JSON(200, map[string]any{"id": "42", "username": "helper", "bot": true}))
	text, isErr := h.call(getMeTool(), nil)
	if isErr || text != "helper [bot] (42)" {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
	raw, _ := h.call(getMeTool(), map[string]any{"format": "json"})
	if !strings.Contains(raw, `"username":"helper"`) {
		t.Fatalf("json = %s", raw)
	}
}

func TestListGuilds(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/users/@me/guilds", discordtest.JSON(200, []map[string]any{
		{"id": "1", "name": "Home", "owner": true, "approximate_member_count": 12},
		{"id": "2", "name": "Work", "approximate_member_count": 300},
	}))
	text, isErr := h.call(listGuildsTool(), map[string]any{"after": "10", "limit": float64(50)})
	if isErr {
		t.Fatal(text)
	}
	if want := "Home (1) members~12 [owner]\nWork (2) members~300"; text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", text, want)
	}
	q := h.last().Query
	if q.Get("after") != "10" || q.Get("limit") != "50" || q.Get("with_counts") != "true" {
		t.Fatalf("query = %v", q)
	}
}

func TestGetGuild(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{id}", discordtest.JSON(200, map[string]any{
		"id": "1", "name": "Home", "owner_id": "7", "description": "cozy",
		"approximate_member_count": 12, "approximate_presence_count": 3, "premium_tier": 1,
	}))
	text, isErr := h.call(getGuildTool(), map[string]any{"guild_id": "1"})
	if isErr {
		t.Fatal(text)
	}
	for _, want := range []string{"Home (1)", "owner: 7", "members: ~12 (~3 online)", "boost tier: 1", "cozy"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if h.last().Query.Get("with_counts") != "true" {
		t.Fatal("with_counts not requested")
	}
}

func TestGetGuildRejectsBadIDWithoutCalling(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	text, isErr := h.call(getGuildTool(), map[string]any{"guild_id": 1.0})
	if !isErr || !strings.Contains(text, "as a string") || h.requests() != 0 {
		t.Fatalf("text=%q isErr=%v requests=%d", text, isErr, h.requests())
	}
	if h.lastAudit()["outcome"] != "invalid_args" {
		t.Fatalf("audit = %v", h.lastAudit())
	}
}

func TestListChannelsGroupsByCategory(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{id}/channels", discordtest.JSON(200, []map[string]any{
		{"id": "20", "type": 0, "name": "general", "parent_id": "10", "position": 1},
		{"id": "10", "type": 4, "name": "Text", "position": 0},
		{"id": "30", "type": 0, "name": "rules", "position": 0},
		{"id": "21", "type": 2, "name": "Lounge", "parent_id": "10", "position": 0, "topic": ""},
	}))
	text, isErr := h.call(listChannelsTool(), map[string]any{"guild_id": "1"})
	if isErr {
		t.Fatal(text)
	}
	want := "#rules (30) text\n" +
		"Text (10) category\n" +
		"  #Lounge (21) voice parent=10\n" +
		"  #general (20) text parent=10"
	if text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", text, want)
	}
}

func TestGetChannel(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/channels/{id}", discordtest.JSON(200, map[string]any{"id": "5", "type": 11, "name": "bug", "parent_id": "4",
		"thread_metadata": map[string]any{"archived": true}}))
	text, isErr := h.call(getChannelTool(), map[string]any{"channel_id": "5"})
	if isErr || text != "#bug (5) public-thread parent=4 archived" {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tools/ -run 'GetMe|Guild|Channel'`
Expected: FAIL — `undefined: getMeTool`.

- [ ] **Step 3: Implement the discovery tools**

`internal/tools/discovery.go`:

```go
package tools

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

func discoveryTools() []Tool {
	return []Tool{getMeTool(), listGuildsTool(), getGuildTool(), listChannelsTool(), getChannelTool()}
}

func getMeTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_get_me",
			mcp.WithDescription("Show the bot user this server acts as."),
			readOnly(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/users/@me"})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(u userJSON) string {
				return fmt.Sprintf("%s (%s)", displayName(u), u.ID)
			})
		},
	}
}

type partialGuildJSON struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Owner                  bool   `json:"owner"`
	ApproximateMemberCount int    `json:"approximate_member_count"`
}

func listGuildsTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_list_guilds",
			mcp.WithDescription("List the servers (guilds) the bot is in."),
			readOnly(),
			optIDParam("after", "Only guilds with an ID after this one, for paging"),
			mcp.WithNumber("limit", mcp.Description("Max guilds, 1-200 (default 200)")),
			withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			after, err := a.OptionalID("after")
			if err != nil {
				return "", err
			}
			limit, err := a.Int("limit", 200, 1, 200)
			if err != nil {
				return "", err
			}
			q := url.Values{"limit": {strconv.Itoa(limit)}, "with_counts": {"true"}}
			if after != "" {
				q.Set("after", after)
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/users/@me/guilds", Query: q})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(gs []partialGuildJSON) string {
				if len(gs) == 0 {
					return "The bot is in no guilds."
				}
				lines := make([]string, len(gs))
				for i, g := range gs {
					lines[i] = fmt.Sprintf("%s (%s) members~%d", g.Name, g.ID, g.ApproximateMemberCount)
					if g.Owner {
						lines[i] += " [owner]"
					}
				}
				return strings.Join(lines, "\n")
			})
		},
	}
}

type guildJSON struct {
	ID                       string   `json:"id"`
	Name                     string   `json:"name"`
	OwnerID                  string   `json:"owner_id"`
	Description              string   `json:"description"`
	ApproximateMemberCount   int      `json:"approximate_member_count"`
	ApproximatePresenceCount int      `json:"approximate_presence_count"`
	PremiumTier              int      `json:"premium_tier"`
	Features                 []string `json:"features"`
}

func getGuildTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_get_guild",
			mcp.WithDescription("Show a guild: name, owner, approximate member counts, boost tier, features."),
			readOnly(), idParam("guild_id", "Guild"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			id, err := a.ID("guild_id")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{
				Method: "GET", Route: "/guilds/{guild_id}",
				Params: map[string]string{"guild_id": id},
				Query:  url.Values{"with_counts": {"true"}},
			})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(g guildJSON) string {
				var sb strings.Builder
				fmt.Fprintf(&sb, "%s (%s)\nowner: %s\nmembers: ~%d (~%d online)\nboost tier: %d",
					g.Name, g.ID, g.OwnerID, g.ApproximateMemberCount, g.ApproximatePresenceCount, g.PremiumTier)
				if g.Description != "" {
					fmt.Fprintf(&sb, "\ndescription: %s", g.Description)
				}
				if len(g.Features) > 0 {
					fmt.Fprintf(&sb, "\nfeatures: %s", strings.Join(g.Features, ", "))
				}
				return sb.String()
			})
		},
	}
}

func listChannelsTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_list_channels",
			mcp.WithDescription("List a guild's channels, grouped under their categories. Active threads are listed by discord_list_active_threads."),
			readOnly(), idParam("guild_id", "Guild"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			id, err := a.ID("guild_id")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/guilds/{guild_id}/channels", Params: map[string]string{"guild_id": id}})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderChannelTree)
		},
	}
}

// renderChannelTree prints uncategorised channels first, then each category
// with its children indented, everything in Discord's position order.
func renderChannelTree(chs []channelJSON) string {
	if len(chs) == 0 {
		return "No channels."
	}
	byPos := func(a, b channelJSON) int {
		if a.Position != b.Position {
			return a.Position - b.Position
		}
		return strings.Compare(a.ID, b.ID)
	}
	children := map[string][]channelJSON{}
	var top, categories []channelJSON
	for _, c := range chs {
		switch {
		case c.Type == 4:
			categories = append(categories, c)
		case c.ParentID != "":
			children[c.ParentID] = append(children[c.ParentID], c)
		default:
			top = append(top, c)
		}
	}
	slices.SortFunc(top, byPos)
	slices.SortFunc(categories, byPos)
	var lines []string
	for _, c := range top {
		lines = append(lines, renderChannel(c))
	}
	for _, cat := range categories {
		lines = append(lines, fmt.Sprintf("%s (%s) category", cat.Name, cat.ID))
		kids := children[cat.ID]
		slices.SortFunc(kids, byPos)
		for _, k := range kids {
			lines = append(lines, "  "+renderChannel(k))
		}
		delete(children, cat.ID)
	}
	// Children whose category was not returned.
	for _, kids := range children {
		for _, k := range kids {
			lines = append(lines, renderChannel(k))
		}
	}
	return strings.Join(lines, "\n")
}

func getChannelTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_get_channel",
			mcp.WithDescription("Show a channel or thread."),
			readOnly(), idParam("channel_id", "Channel or thread"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			id, err := a.ID("channel_id")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/channels/{channel_id}", Params: map[string]string{"channel_id": id}})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderChannel)
		},
	}
}
```

In `internal/tools/sets.go`, change `BotTools` to:

```go
func BotTools() []Tool {
	var out []Tool
	out = append(out, discoveryTools()...)
	return out
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tools/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools
git commit -m "feat(tools): add discovery tools" -m "Add discord_get_me, discord_list_guilds, discord_get_guild,
discord_list_channels and discord_get_channel. Text output is compact for
the LLM (channels are grouped under their categories in position order)
and format json returns Discord's object as-is. Guild counts are
requested with with_counts."
```

---
### Task 11: Message tools (messages, reactions, pins, search, DMs)

**Files:**
- Create: `internal/tools/messages.go`
- Modify: `internal/tools/sets.go` (`BotTools`)
- Test: `internal/tools/messages_test.go`

**Interfaces:**
- Consumes: Task 9 helpers (`Args`, `messagePayload`, `output`, `renderMessage`, `renderMessages`, schema helpers), `discord.Call`, `discord.Response`.
- Produces: `messageTools() []Tool` with constructors `readMessagesTool()`, `getMessageTool()`, `sendMessageTool()`, `editMessageTool()`, `deleteMessageTool()`, `pinMessageTool()`, `addReactionTool()`, `removeReactionTool()`, `searchMessagesTool()`, `sendDMTool()`; helper `normalizeEmoji(s string) string`; `sendResult(resp discord.Response, format string) (string, error)` (reused by Task 16).

- [ ] **Step 1: Write the failing tests**

`internal/tools/messages_test.go`:

```go
package tools

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
)

var sentMessage = map[string]any{"id": "900", "channel_id": "5", "content": "hi", "author": map[string]any{"id": "42", "username": "helper"}, "timestamp": "2026-09-14T11:02:00Z"}

func TestReadMessages(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/channels/{id}/messages", discordtest.JSON(200, []map[string]any{
		{"id": "2", "content": "later", "author": map[string]any{"id": "8", "username": "bob"}, "timestamp": "2026-09-14T11:03:00Z"},
		{"id": "1", "content": "earlier", "author": map[string]any{"id": "7", "username": "al"}, "timestamp": "2026-09-14T11:02:00Z"},
	}))
	text, isErr := h.call(readMessagesTool(), map[string]any{"channel_id": "5", "around": "1", "limit": float64(10)})
	if isErr {
		t.Fatal(text)
	}
	if !strings.HasPrefix(text, "[2026-09-14 11:02] al (7) #1: earlier\n[2026-09-14 11:03] bob (8) #2: later") {
		t.Fatalf("text:\n%s", text)
	}
	q := h.last().Query
	if q.Get("around") != "1" || q.Get("limit") != "10" || q.Has("before") {
		t.Fatalf("query = %v", q)
	}
}

func TestReadMessagesRejectsTwoAnchors(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	_, isErr := h.call(readMessagesTool(), map[string]any{"channel_id": "5", "before": "1", "after": "2"})
	if !isErr || h.requests() != 0 {
		t.Fatalf("isErr=%v requests=%d", isErr, h.requests())
	}
}

func TestGetMessage(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/channels/{c}/messages/{m}", discordtest.JSON(200, sentMessage))
	text, isErr := h.call(getMessageTool(), map[string]any{"channel_id": "5", "message_id": "900"})
	if isErr || text != "[2026-09-14 11:02] helper (42) #900: hi" {
		t.Fatalf("text=%q", text)
	}
}

func TestSendMessage(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/channels/{id}/messages", discordtest.JSON(200, sentMessage))
	text, isErr := h.call(sendMessageTool(), map[string]any{
		"channel_id": "5", "content": "hi @everyone", "reply_to": "899", "allow_mentions": false,
	})
	if isErr || text != "Sent message #900 in channel 5." {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
	body := h.lastBody()
	if body["content"] != "hi @everyone" {
		t.Fatalf("body = %v", body)
	}
	if !reflect.DeepEqual(body["message_reference"], map[string]any{"message_id": "899", "fail_if_not_exists": false}) {
		t.Fatalf("message_reference = %v", body["message_reference"])
	}
	if !reflect.DeepEqual(body["allowed_mentions"], map[string]any{"parse": []any{}}) {
		t.Fatalf("allowed_mentions = %v", body["allowed_mentions"])
	}
}

func TestSendMessageDefaultsSendNoAllowedMentions(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/channels/{id}/messages", discordtest.JSON(200, sentMessage))
	h.call(sendMessageTool(), map[string]any{"channel_id": "5", "content": "@here"})
	if _, ok := h.lastBody()["allowed_mentions"]; ok {
		t.Fatal("allow_mentions default must not send allowed_mentions")
	}
}

func TestSendMessageWithAttachmentIsMultipart(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/channels/{id}/messages", discordtest.JSON(200, sentMessage))
	_, isErr := h.call(sendMessageTool(), map[string]any{
		"channel_id":  "5",
		"attachments": []any{map[string]any{"filename": "a.txt", "content_base64": "aGVsbG8="}},
	})
	if isErr {
		t.Fatal("send failed")
	}
	if ct := h.last().Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/form-data") {
		t.Fatalf("Content-Type = %s", ct)
	}
	args := h.lastAudit()["args"].(map[string]any)
	att := args["attachments"].([]any)[0].(map[string]any)
	if _, leaked := att["content_base64"]; leaked || att["size"] != float64(5) {
		t.Fatalf("audit attachment = %v", att)
	}
}

func TestSendMessageNeedsBody(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	_, isErr := h.call(sendMessageTool(), map[string]any{"channel_id": "5"})
	if !isErr || h.requests() != 0 {
		t.Fatalf("isErr=%v requests=%d", isErr, h.requests())
	}
}

func TestEditAndDeleteMessage(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("PATCH /api/v10/channels/{c}/messages/{m}", discordtest.JSON(200, sentMessage))
	h.handle("DELETE /api/v10/channels/{c}/messages/{m}", discordtest.JSON(204, nil))

	text, isErr := h.call(editMessageTool(), map[string]any{"channel_id": "5", "message_id": "900", "content": "fixed"})
	if isErr || text != "Edited message #900." || h.lastBody()["content"] != "fixed" {
		t.Fatalf("edit: %q %v %v", text, isErr, h.lastBody())
	}
	text, isErr = h.call(deleteMessageTool(), map[string]any{"channel_id": "5", "message_id": "900", "reason": "spam"})
	if isErr || text != "Deleted message #900." || h.last().Header.Get("X-Audit-Log-Reason") != "spam" {
		t.Fatalf("delete: %q %v", text, isErr)
	}
}

func TestPinMessage(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("PUT /api/v10/channels/{c}/messages/pins/{m}", discordtest.JSON(204, nil))
	h.handle("DELETE /api/v10/channels/{c}/messages/pins/{m}", discordtest.JSON(204, nil))
	if text, _ := h.call(pinMessageTool(), map[string]any{"channel_id": "5", "message_id": "900", "pinned": true}); text != "Pinned message #900." || h.last().Method != "PUT" {
		t.Fatalf("pin: %q %s", text, h.last().Method)
	}
	if text, _ := h.call(pinMessageTool(), map[string]any{"channel_id": "5", "message_id": "900", "pinned": false}); text != "Unpinned message #900." || h.last().Method != "DELETE" {
		t.Fatalf("unpin: %q %s", text, h.last().Method)
	}
	if _, isErr := h.call(pinMessageTool(), map[string]any{"channel_id": "5", "message_id": "900"}); !isErr {
		t.Fatal("pinned is required")
	}
}

func TestReactions(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("PUT /api/v10/channels/{c}/messages/{m}/reactions/{e}/@me", discordtest.JSON(204, nil))
	h.handle("DELETE /api/v10/channels/{c}/messages/{m}/reactions/{e}/{u}", discordtest.JSON(204, nil))

	h.call(addReactionTool(), map[string]any{"channel_id": "5", "message_id": "900", "emoji": "<:party:123>"})
	if raw := h.last().RawPath; !strings.HasSuffix(raw, "/reactions/party:123/@me") {
		t.Fatalf("custom emoji path = %s", raw)
	}
	h.call(removeReactionTool(), map[string]any{"channel_id": "5", "message_id": "900", "emoji": "👍", "user_id": "7"})
	if raw := h.last().RawPath; !strings.HasSuffix(raw, "/reactions/%F0%9F%91%8D/7") || h.last().Method != "DELETE" {
		t.Fatalf("remove path = %s", raw)
	}
	h.call(removeReactionTool(), map[string]any{"channel_id": "5", "message_id": "900", "emoji": "👍"})
	if raw := h.last().RawPath; !strings.HasSuffix(raw, "/@me") {
		t.Fatalf("remove own path = %s", raw)
	}
}

func TestNormalizeEmoji(t *testing.T) {
	for in, want := range map[string]string{
		"<:party:123>":  "party:123",
		"<a:dance:456>": "dance:456",
		"party:123":     "party:123",
		"👍":             "👍",
	} {
		if got := normalizeEmoji(in); got != want {
			t.Errorf("normalizeEmoji(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchMessages(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{g}/messages/search", discordtest.JSON(200, map[string]any{
		"total_results": 1,
		"messages":      []any{[]any{sentMessage}},
	}))
	text, isErr := h.call(searchMessagesTool(), map[string]any{
		"guild_id": "1", "content": "hi", "channel_id": "5", "author_id": "42", "limit": float64(5), "offset": float64(10),
	})
	if isErr || text != "1 result(s), showing 1:\n[2026-09-14 11:02] helper (42) #900: hi" {
		t.Fatalf("text=%q", text)
	}
	q := h.last().Query
	if q.Get("content") != "hi" || q.Get("channel_id") != "5" || q.Get("author_id") != "42" || q.Get("limit") != "5" || q.Get("offset") != "10" {
		t.Fatalf("query = %v", q)
	}
}

func TestSearchMessagesIndexing(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{g}/messages/search", discordtest.JSON(http.StatusAccepted, map[string]any{
		"message": "Index not yet available. Try again later", "code": 110000, "retry_after": 2,
	}))
	text, isErr := h.call(searchMessagesTool(), map[string]any{"guild_id": "1", "content": "hi"})
	if !isErr || !strings.Contains(text, "retry after 2s") {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
}

func TestSendDM(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/users/@me/channels", discordtest.JSON(200, map[string]any{"id": "77", "type": 1}))
	h.handle("POST /api/v10/channels/{id}/messages", discordtest.JSON(200, map[string]any{"id": "901", "channel_id": "77"}))
	text, isErr := h.call(sendDMTool(), map[string]any{"user_id": "7", "content": "hello"})
	if isErr || text != "Sent message #901 in channel 77." {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
	reqs := h.fake.Requests()
	if len(reqs) != 2 || !strings.Contains(string(reqs[0].Body), `"recipient_id":"7"`) || reqs[1].Path != "/api/v10/channels/77/messages" {
		t.Fatalf("requests = %+v", reqs)
	}
	if calls := h.lastAudit()["discord"].([]any); len(calls) != 2 {
		t.Fatalf("audit must list both calls: %v", calls)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tools/ -run 'Message|Reaction|Emoji|DM'`
Expected: FAIL — `undefined: readMessagesTool`.

- [ ] **Step 3: Implement the message tools**

`internal/tools/messages.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

func messageTools() []Tool {
	return []Tool{
		readMessagesTool(), getMessageTool(), sendMessageTool(), editMessageTool(), deleteMessageTool(),
		pinMessageTool(), addReactionTool(), removeReactionTool(), searchMessagesTool(), sendDMTool(),
	}
}

func chanMsg(channelID, messageID string) map[string]string {
	return map[string]string{"channel_id": channelID, "message_id": messageID}
}

// channelAndMessage reads the two IDs most message tools take.
func channelAndMessage(a Args) (string, string, error) {
	c, err := a.ID("channel_id")
	if err != nil {
		return "", "", err
	}
	m, err := a.ID("message_id")
	if err != nil {
		return "", "", err
	}
	return c, m, nil
}

func readMessagesTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_read_messages",
			mcp.WithDescription("Read message history from a channel or thread, printed oldest first. Use at most one of before, after, around."),
			readOnly(),
			idParam("channel_id", "Channel or thread"),
			mcp.WithNumber("limit", mcp.Description("Messages to return, 1-100 (default 50)")),
			optIDParam("before", "Messages before this message"),
			optIDParam("after", "Messages after this message"),
			optIDParam("around", "Messages around this message"),
			withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			channel, err := a.ID("channel_id")
			if err != nil {
				return "", err
			}
			limit, err := a.Int("limit", 50, 1, 100)
			if err != nil {
				return "", err
			}
			anchor, err := a.OneOf("before", "after", "around")
			if err != nil {
				return "", err
			}
			q := url.Values{"limit": {strconv.Itoa(limit)}}
			if anchor != "" {
				id, err := a.ID(anchor)
				if err != nil {
					return "", err
				}
				q.Set(anchor, id)
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/channels/{channel_id}/messages", Params: map[string]string{"channel_id": channel}, Query: q})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderMessages)
		},
	}
}

func getMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_get_message",
			mcp.WithDescription("Read one message."),
			readOnly(), idParam("channel_id", "Channel or thread"), idParam("message_id", "Message"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			c, m, err := channelAndMessage(a)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/channels/{channel_id}/messages/{message_id}", Params: chanMsg(c, m)})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderMessage)
		},
	}
}

// sendResult reports a created message.
func sendResult(resp discord.Response, format string) (string, error) {
	return output(resp.Body, format, func(m messageJSON) string {
		return fmt.Sprintf("Sent message #%s in channel %s.", m.ID, m.ChannelID)
	})
}

func sendMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_send_message",
			mcp.WithDescription("Send a message to a channel or thread. Provide content, embeds or attachments."),
			mutating(),
			idParam("channel_id", "Channel or thread"),
			mcp.WithString("content", mcp.Description("Message text (up to 2000 characters)")),
			withEmbeds(), withAttachments(),
			optIDParam("reply_to", "Reply to this message in the same channel"),
			withAllowMentions(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			channel, err := a.ID("channel_id")
			if err != nil {
				return "", err
			}
			body, files, err := messagePayload(a, true, true)
			if err != nil {
				return "", err
			}
			replyTo, err := a.OptionalID("reply_to")
			if err != nil {
				return "", err
			}
			if replyTo != "" {
				body["message_reference"] = map[string]any{"message_id": replyTo, "fail_if_not_exists": false}
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "POST", Route: "/channels/{channel_id}/messages", Params: map[string]string{"channel_id": channel}, Body: body, Files: files})
			if err != nil {
				return "", err
			}
			return sendResult(resp, format)
		},
	}
}

func editMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_edit_message",
			mcp.WithDescription("Edit a message the bot sent: replace its content and/or embeds."),
			mutating(),
			idParam("channel_id", "Channel or thread"), idParam("message_id", "Message"),
			mcp.WithString("content", mcp.Description("New text")),
			withEmbeds(), withAllowMentions(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			c, m, err := channelAndMessage(a)
			if err != nil {
				return "", err
			}
			body, _, err := messagePayload(a, true, false)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "PATCH", Route: "/channels/{channel_id}/messages/{message_id}", Params: chanMsg(c, m), Body: body})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(msg messageJSON) string { return fmt.Sprintf("Edited message #%s.", msg.ID) })
		},
	}
}

func deleteMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_delete_message",
			mcp.WithDescription("Delete a message."),
			destructive(), idParam("channel_id", "Channel or thread"), idParam("message_id", "Message"), withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			c, m, err := channelAndMessage(a)
			if err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			if _, err := p.Client.Do(ctx, discord.Call{Method: "DELETE", Route: "/channels/{channel_id}/messages/{message_id}", Params: chanMsg(c, m), Reason: reason}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Deleted message #%s.", m), nil
		},
	}
}

func pinMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_pin_message",
			mcp.WithDescription("Pin (pinned=true) or unpin (pinned=false) a message."),
			mutating(), idParam("channel_id", "Channel or thread"), idParam("message_id", "Message"),
			mcp.WithBoolean("pinned", mcp.Required(), mcp.Description("true to pin, false to unpin")),
			withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			c, m, err := channelAndMessage(a)
			if err != nil {
				return "", err
			}
			if _, ok := a.present("pinned"); !ok {
				return "", argErr("pinned is required")
			}
			pinned, err := a.Bool("pinned", false)
			if err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			method, verb := http.MethodPut, "Pinned"
			if !pinned {
				method, verb = http.MethodDelete, "Unpinned"
			}
			if _, err := p.Client.Do(ctx, discord.Call{Method: method, Route: "/channels/{channel_id}/messages/pins/{message_id}", Params: chanMsg(c, m), Reason: reason}); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s message #%s.", verb, m), nil
		},
	}
}

var customEmojiRe = regexp.MustCompile(`^<a?:([A-Za-z0-9_]+):([0-9]+)>$`)

// normalizeEmoji accepts the <:name:id> form as it appears in message text
// and turns it into the name:id form the reactions API expects.
func normalizeEmoji(s string) string {
	if m := customEmojiRe.FindStringSubmatch(s); m != nil {
		return m[1] + ":" + m[2]
	}
	return s
}

const emojiDesc = "Unicode emoji (👍) or custom emoji as name:id or <:name:id>"

func addReactionTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_add_reaction",
			mcp.WithDescription("React to a message as the bot."),
			mutating(), idParam("channel_id", "Channel or thread"), idParam("message_id", "Message"),
			mcp.WithString("emoji", mcp.Required(), mcp.Description(emojiDesc))),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			c, m, err := channelAndMessage(a)
			if err != nil {
				return "", err
			}
			emoji, err := a.RequiredString("emoji")
			if err != nil {
				return "", err
			}
			params := chanMsg(c, m)
			params["emoji"] = normalizeEmoji(emoji)
			if _, err := p.Client.Do(ctx, discord.Call{Method: "PUT", Route: "/channels/{channel_id}/messages/{message_id}/reactions/{emoji}/@me", Params: params}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Reacted %s to message #%s.", emoji, m), nil
		},
	}
}

func removeReactionTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_remove_reaction",
			mcp.WithDescription("Remove the bot's reaction, or another user's reaction when user_id is given."),
			destructive(), idParam("channel_id", "Channel or thread"), idParam("message_id", "Message"),
			mcp.WithString("emoji", mcp.Required(), mcp.Description(emojiDesc)),
			optIDParam("user_id", "Remove this user's reaction instead of the bot's")),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			c, m, err := channelAndMessage(a)
			if err != nil {
				return "", err
			}
			emoji, err := a.RequiredString("emoji")
			if err != nil {
				return "", err
			}
			user, err := a.OptionalID("user_id")
			if err != nil {
				return "", err
			}
			params := chanMsg(c, m)
			params["emoji"] = normalizeEmoji(emoji)
			route := "/channels/{channel_id}/messages/{message_id}/reactions/{emoji}/@me"
			if user != "" {
				route = "/channels/{channel_id}/messages/{message_id}/reactions/{emoji}/{user_id}"
				params["user_id"] = user
			}
			if _, err := p.Client.Do(ctx, discord.Call{Method: "DELETE", Route: route, Params: params}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Removed reaction %s from message #%s.", emoji, m), nil
		},
	}
}

func searchMessagesTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_search_messages",
			mcp.WithDescription("Search a guild's messages. Needs Read Message History; results depend on the Message Content intent."),
			readOnly(),
			idParam("guild_id", "Guild to search"),
			mcp.WithString("content", mcp.Description("Text to search for")),
			optIDParam("channel_id", "Only this channel"),
			optIDParam("author_id", "Only messages by this user"),
			mcp.WithNumber("limit", mcp.Description("Results, 1-25 (default 25)")),
			mcp.WithNumber("offset", mcp.Description("Skip this many results, 0-9975")),
			withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			guild, err := a.ID("guild_id")
			if err != nil {
				return "", err
			}
			q := url.Values{}
			content, err := a.String("content")
			if err != nil {
				return "", err
			}
			if content != "" {
				q.Set("content", content)
			}
			for _, key := range []string{"channel_id", "author_id"} {
				id, err := a.OptionalID(key)
				if err != nil {
					return "", err
				}
				if id != "" {
					q.Set(key, id)
				}
			}
			limit, err := a.Int("limit", 25, 1, 25)
			if err != nil {
				return "", err
			}
			offset, err := a.Int("offset", 0, 0, 9975)
			if err != nil {
				return "", err
			}
			q.Set("limit", strconv.Itoa(limit))
			if offset > 0 {
				q.Set("offset", strconv.Itoa(offset))
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/guilds/{guild_id}/messages/search", Params: map[string]string{"guild_id": guild}, Query: q})
			if err != nil {
				return "", err
			}
			if resp.Status == http.StatusAccepted {
				var pending struct {
					RetryAfter float64 `json:"retry_after"`
				}
				_ = json.Unmarshal(resp.Body, &pending)
				return "", fmt.Errorf("Discord is still indexing this guild for search: retry after %gs", pending.RetryAfter)
			}
			return output(resp.Body, format, func(r struct {
				TotalResults int             `json:"total_results"`
				Messages     [][]messageJSON `json:"messages"`
			}) string {
				var hits []string
				for _, group := range r.Messages {
					if len(group) > 0 {
						hits = append(hits, renderMessage(group[0]))
					}
				}
				if len(hits) == 0 {
					return "No results."
				}
				return fmt.Sprintf("%d result(s), showing %d:\n%s", r.TotalResults, len(hits), strings.Join(hits, "\n"))
			})
		},
	}
}

func sendDMTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_send_dm",
			mcp.WithDescription("Send a direct message to a user. The user must share a server with the bot and accept DMs."),
			mutating(),
			idParam("user_id", "Recipient"),
			mcp.WithString("content", mcp.Description("Message text")),
			withEmbeds(), withAttachments(), withAllowMentions(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			user, err := a.ID("user_id")
			if err != nil {
				return "", err
			}
			body, files, err := messagePayload(a, true, true)
			if err != nil {
				return "", err
			}
			dm, err := p.Client.Do(ctx, discord.Call{Method: "POST", Route: "/users/@me/channels", Body: map[string]any{"recipient_id": user}})
			if err != nil {
				return "", err
			}
			var ch channelJSON
			if err := json.Unmarshal(dm.Body, &ch); err != nil || ch.ID == "" {
				return "", fmt.Errorf("decode DM channel: unexpected response")
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "POST", Route: "/channels/{channel_id}/messages", Params: map[string]string{"channel_id": ch.ID}, Body: body, Files: files})
			if err != nil {
				return "", err
			}
			return sendResult(resp, format)
		},
	}
}
```

In `internal/tools/sets.go`, add to `BotTools` after the discovery line:

```go
	out = append(out, messageTools()...)
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tools/`
Expected: PASS. `go vet` flags `fmt.Errorf("Discord is still indexing…")` only if staticcheck is used (capitalised error string); `go vet` itself accepts it, and the capital is Discord's name.

- [ ] **Step 5: Commit**

```bash
git add internal/tools
git commit -m "feat(tools): add message, reaction, pin, search and dm tools" -m "Add discord_read_messages (history oldest first, one of before, after or
around), discord_get_message, discord_send_message (reply_to, embeds,
base64 attachments), discord_edit_message, discord_delete_message,
discord_pin_message on the current pins routes, discord_add_reaction and
discord_remove_reaction (accepting <:name:id> as it appears in text),
discord_search_messages and discord_send_dm.

Search answers 202 while Discord indexes a guild; the tool reports the
retry_after instead of an empty result. send_dm opens the DM channel
first, and both Discord calls appear in the audit line."
```

---
### Task 12: Thread and channel tools

**Files:**
- Create: `internal/tools/channels.go`
- Modify: `internal/tools/sets.go` (`BotTools`)
- Test: `internal/tools/channels_test.go`

**Interfaces:**
- Consumes: Task 9 helpers; `renderChannel`, `channelJSON`, `channelTypeName`.
- Produces: `channelTools() []Tool` with constructors `createThreadTool()`, `listActiveThreadsTool()`, `createChannelTool()`, `editChannelTool()`, `deleteChannelTool()`.

- [ ] **Step 1: Write the failing tests**

`internal/tools/channels_test.go`:

```go
package tools

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
)

func TestCreateThreadFromMessage(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/channels/{c}/messages/{m}/threads", discordtest.JSON(201, map[string]any{"id": "60", "type": 11, "name": "bug", "parent_id": "5"}))
	text, isErr := h.call(createThreadTool(), map[string]any{"channel_id": "5", "message_id": "900", "name": "bug", "reason": "triage"})
	if isErr || text != "Created #bug (60) public-thread parent=5" {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
	if !reflect.DeepEqual(h.lastBody(), map[string]any{"name": "bug", "auto_archive_duration": float64(1440)}) {
		t.Fatalf("body = %v", h.lastBody())
	}
	if h.last().Header.Get("X-Audit-Log-Reason") != "triage" {
		t.Fatal("reason not sent")
	}
}

func TestCreateStandalonePrivateThread(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/channels/{c}/threads", discordtest.JSON(201, map[string]any{"id": "61", "type": 12, "name": "secret", "parent_id": "5"}))
	_, isErr := h.call(createThreadTool(), map[string]any{"channel_id": "5", "name": "secret", "private": true, "auto_archive_minutes": float64(60)})
	if isErr {
		t.Fatal("create failed")
	}
	if !reflect.DeepEqual(h.lastBody(), map[string]any{"name": "secret", "auto_archive_duration": float64(60), "type": float64(12)}) {
		t.Fatalf("body = %v", h.lastBody())
	}
}

func TestCreateThreadValidation(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	for name, args := range map[string]map[string]any{
		"bad archive":         {"channel_id": "5", "name": "x", "auto_archive_minutes": float64(30)},
		"private on message":  {"channel_id": "5", "message_id": "1", "name": "x", "private": true},
		"missing name":        {"channel_id": "5"},
	} {
		if _, isErr := h.call(createThreadTool(), args); !isErr {
			t.Errorf("%s: want error", name)
		}
	}
	if h.requests() != 0 {
		t.Fatalf("%d request(s) sent", h.requests())
	}
}

func TestListActiveThreads(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{g}/threads/active", discordtest.JSON(200, map[string]any{
		"threads": []map[string]any{{"id": "60", "type": 11, "name": "bug", "parent_id": "5"}},
		"members": []any{},
	}))
	text, isErr := h.call(listActiveThreadsTool(), map[string]any{"guild_id": "1"})
	if isErr || text != "#bug (60) public-thread parent=5" {
		t.Fatalf("text=%q", text)
	}
}

func TestCreateChannel(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/guilds/{g}/channels", discordtest.JSON(201, map[string]any{"id": "70", "type": 2, "name": "Lounge", "parent_id": "10"}))
	text, isErr := h.call(createChannelTool(), map[string]any{"guild_id": "1", "name": "Lounge", "type": "voice", "parent_id": "10"})
	if isErr || !strings.HasPrefix(text, "Created #Lounge (70) voice") {
		t.Fatalf("text=%q", text)
	}
	if !reflect.DeepEqual(h.lastBody(), map[string]any{"name": "Lounge", "type": float64(2), "parent_id": "10"}) {
		t.Fatalf("body = %v", h.lastBody())
	}
}

func TestEditChannel(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("PATCH /api/v10/channels/{c}", discordtest.JSON(200, map[string]any{"id": "60", "type": 11, "name": "bug", "thread_metadata": map[string]any{"archived": true, "locked": true}}))
	text, isErr := h.call(editChannelTool(), map[string]any{"channel_id": "60", "archived": true, "locked": true, "topic": ""})
	if isErr || text != "Updated #bug (60) public-thread archived locked" {
		t.Fatalf("text=%q", text)
	}
	if !reflect.DeepEqual(h.lastBody(), map[string]any{"archived": true, "locked": true, "topic": ""}) {
		t.Fatalf("body = %v", h.lastBody())
	}
	if _, isErr := h.call(editChannelTool(), map[string]any{"channel_id": "60"}); !isErr {
		t.Fatal("edit with no fields must fail")
	}
}

func TestDeleteChannel(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("DELETE /api/v10/channels/{c}", discordtest.JSON(200, map[string]any{"id": "70", "type": 0, "name": "old"}))
	text, isErr := h.call(deleteChannelTool(), map[string]any{"channel_id": "70", "reason": "cleanup"})
	if isErr || text != "Deleted #old (70) text" {
		t.Fatalf("text=%q", text)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tools/ -run 'Thread|Channel'`
Expected: FAIL — `undefined: createThreadTool`.

- [ ] **Step 3: Implement the channel tools**

`internal/tools/channels.go`:

```go
package tools

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

func channelTools() []Tool {
	return []Tool{createThreadTool(), listActiveThreadsTool(), createChannelTool(), editChannelTool(), deleteChannelTool()}
}

var autoArchiveMinutes = map[int]bool{60: true, 1440: true, 4320: true, 10080: true}

func createThreadTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_create_thread",
			mcp.WithDescription("Create a thread from a message (message_id) or a standalone thread in a channel."),
			mutating(),
			idParam("channel_id", "Parent channel"),
			mcp.WithString("name", mcp.Required(), mcp.Description("Thread name (1-100 characters)")),
			optIDParam("message_id", "Start the thread from this message"),
			mcp.WithBoolean("private", mcp.Description("Standalone threads only: create a private thread")),
			mcp.WithNumber("auto_archive_minutes", mcp.Description("60, 1440 (default), 4320 or 10080")),
			withReason(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			channel, err := a.ID("channel_id")
			if err != nil {
				return "", err
			}
			name, err := a.RequiredString("name")
			if err != nil {
				return "", err
			}
			message, err := a.OptionalID("message_id")
			if err != nil {
				return "", err
			}
			private, err := a.Bool("private", false)
			if err != nil {
				return "", err
			}
			archive, err := a.Int("auto_archive_minutes", 1440, 60, 10080)
			if err != nil || !autoArchiveMinutes[archive] {
				return "", argErr("auto_archive_minutes must be 60, 1440, 4320 or 10080")
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			body := map[string]any{"name": name, "auto_archive_duration": archive}
			call := discord.Call{Method: "POST", Body: body, Reason: reason}
			if message != "" {
				if private {
					return "", argErr("private applies to standalone threads only; omit message_id")
				}
				call.Route = "/channels/{channel_id}/messages/{message_id}/threads"
				call.Params = chanMsg(channel, message)
			} else {
				body["type"] = 11
				if private {
					body["type"] = 12
				}
				call.Route = "/channels/{channel_id}/threads"
				call.Params = map[string]string{"channel_id": channel}
			}
			resp, err := p.Client.Do(ctx, call)
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(c channelJSON) string { return "Created " + renderChannel(c) })
		},
	}
}

func listActiveThreadsTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_list_active_threads",
			mcp.WithDescription("List a guild's active (non-archived) threads."),
			readOnly(), idParam("guild_id", "Guild"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			guild, err := a.ID("guild_id")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/guilds/{guild_id}/threads/active", Params: map[string]string{"guild_id": guild}})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(r struct {
				Threads []channelJSON `json:"threads"`
			}) string {
				if len(r.Threads) == 0 {
					return "No active threads."
				}
				lines := make([]string, len(r.Threads))
				for i, c := range r.Threads {
					lines[i] = renderChannel(c)
				}
				return strings.Join(lines, "\n")
			})
		},
	}
}

var createChannelTypes = map[string]int{"text": 0, "voice": 2, "category": 4, "announcement": 5, "stage": 13, "forum": 15}

func createChannelTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_create_channel",
			mcp.WithDescription("Create a channel in a guild."),
			mutating(),
			idParam("guild_id", "Guild"),
			mcp.WithString("name", mcp.Required(), mcp.Description("Channel name")),
			mcp.WithString("type", mcp.Enum("text", "voice", "category", "announcement", "stage", "forum"), mcp.Description("Default text")),
			mcp.WithString("topic", mcp.Description("Channel topic")),
			optIDParam("parent_id", "Category to put the channel in"),
			mcp.WithBoolean("nsfw", mcp.Description("Age-restricted channel")),
			withReason(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			guild, err := a.ID("guild_id")
			if err != nil {
				return "", err
			}
			name, err := a.RequiredString("name")
			if err != nil {
				return "", err
			}
			typeName, err := a.String("type")
			if err != nil {
				return "", err
			}
			if typeName == "" {
				typeName = "text"
			}
			typ, ok := createChannelTypes[typeName]
			if !ok {
				return "", argErr("type must be one of text, voice, category, announcement, stage, forum")
			}
			body := map[string]any{"name": name, "type": typ}
			if err := copyOptional(a, body, "topic", "parent_id", "nsfw"); err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "POST", Route: "/guilds/{guild_id}/channels", Params: map[string]string{"guild_id": guild}, Body: body, Reason: reason})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(c channelJSON) string { return "Created " + renderChannel(c) })
		},
	}
}

// copyOptional copies the given arguments into body when present, validating
// *_id keys as IDs, known booleans as booleans and position as an integer.
func copyOptional(a Args, body map[string]any, keys ...string) error {
	for _, k := range keys {
		if _, ok := a.present(k); !ok {
			continue
		}
		switch {
		case strings.HasSuffix(k, "_id"):
			id, err := a.OptionalID(k)
			if err != nil {
				return err
			}
			body[k] = id
		case k == "nsfw" || k == "archived" || k == "locked":
			b, err := a.Bool(k, false)
			if err != nil {
				return err
			}
			body[k] = b
		case k == "position":
			n, err := a.Int(k, 0, 0, 10000)
			if err != nil {
				return err
			}
			body[k] = n
		default:
			s, err := a.String(k)
			if err != nil {
				return err
			}
			body[k] = s
		}
	}
	return nil
}

func editChannelTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_edit_channel",
			mcp.WithDescription("Change a channel or thread. Only the fields given are changed; archived and locked apply to threads."),
			mutating(),
			idParam("channel_id", "Channel or thread"),
			mcp.WithString("name", mcp.Description("New name")),
			mcp.WithString("topic", mcp.Description("New topic (empty string clears it)")),
			optIDParam("parent_id", "Move under this category"),
			mcp.WithBoolean("nsfw", mcp.Description("Age-restricted")),
			mcp.WithNumber("position", mcp.Description("Sort position")),
			mcp.WithBoolean("archived", mcp.Description("Threads: archive or unarchive")),
			mcp.WithBoolean("locked", mcp.Description("Threads: lock or unlock")),
			withReason(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			channel, err := a.ID("channel_id")
			if err != nil {
				return "", err
			}
			body := map[string]any{}
			if err := copyOptional(a, body, "name", "topic", "parent_id", "nsfw", "position", "archived", "locked"); err != nil {
				return "", err
			}
			if len(body) == 0 {
				return "", argErr("give at least one field to change")
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "PATCH", Route: "/channels/{channel_id}", Params: map[string]string{"channel_id": channel}, Body: body, Reason: reason})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(c channelJSON) string { return "Updated " + renderChannel(c) })
		},
	}
}

func deleteChannelTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_delete_channel",
			mcp.WithDescription("Delete a channel or thread. This cannot be undone."),
			destructive(), idParam("channel_id", "Channel or thread"), withReason(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			channel, err := a.ID("channel_id")
			if err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "DELETE", Route: "/channels/{channel_id}", Params: map[string]string{"channel_id": channel}, Reason: reason})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(c channelJSON) string { return "Deleted " + renderChannel(c) })
		},
	}
}
```

In `internal/tools/sets.go`, add to `BotTools`:

```go
	out = append(out, channelTools()...)
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tools/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools
git commit -m "feat(tools): add thread and channel tools" -m "Add discord_create_thread (from a message, or standalone public or
private), discord_list_active_threads, discord_create_channel,
discord_edit_channel and discord_delete_channel. Edits send only the
fields given, so an omitted topic is left alone while an empty one
clears it; archived and locked cover thread archiving and locking.
auto_archive_minutes is checked against the four durations Discord
accepts before anything is sent."
```

---
### Task 13: Member, role and moderation tools

**Files:**
- Create: `internal/tools/members.go`
- Modify: `internal/tools/sets.go` (`BotTools`)
- Test: `internal/tools/members_test.go`

**Interfaces:**
- Consumes: Task 9 helpers; `userJSON`, `displayName`.
- Produces: `memberTools() []Tool` with constructors `listMembersTool()`, `getMemberTool()`, `searchMembersTool()`, `listRolesTool()`, `addRoleTool()`, `removeRoleTool()`, `timeoutMemberTool()`, `kickMemberTool()`, `banMemberTool()`, `unbanMemberTool()`; package variable `timeNow = time.Now` (overridden in tests).

- [ ] **Step 1: Write the failing tests**

`internal/tools/members_test.go`:

```go
package tools

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
)

var member = map[string]any{
	"user": map[string]any{"id": "7", "username": "alice"}, "nick": "Al", "roles": []string{"1", "2"},
	"joined_at": "2026-01-02T03:04:05Z",
}

func TestListAndSearchMembers(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{g}/members", discordtest.JSON(200, []any{member}))
	h.handle("GET /api/v10/guilds/{g}/members/search", discordtest.JSON(200, []any{member}))

	text, isErr := h.call(listMembersTool(), map[string]any{"guild_id": "1", "after": "5", "limit": float64(10)})
	if isErr || text != "alice (7) nick=Al roles=1,2 joined 2026-01-02" {
		t.Fatalf("text=%q", text)
	}
	if q := h.last().Query; q.Get("after") != "5" || q.Get("limit") != "10" {
		t.Fatalf("query = %v", q)
	}
	if _, isErr := h.call(searchMembersTool(), map[string]any{"guild_id": "1", "query": "ali"}); isErr {
		t.Fatal("search failed")
	}
	if q := h.last().Query; q.Get("query") != "ali" || q.Get("limit") != "25" {
		t.Fatalf("query = %v", q)
	}
	if _, isErr := h.call(searchMembersTool(), map[string]any{"guild_id": "1"}); !isErr {
		t.Fatal("query is required")
	}
}

func TestGetMemberTimedOut(t *testing.T) {
	timeNow = func() time.Time { return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { timeNow = time.Now })
	h := newHarness(t, auth.CapBot)
	m := map[string]any{"user": map[string]any{"id": "7", "username": "alice"}, "roles": []string{},
		"joined_at": "2026-01-02T03:04:05Z", "communication_disabled_until": "2026-09-15T00:00:00Z"}
	h.handle("GET /api/v10/guilds/{g}/members/{u}", discordtest.JSON(200, m))
	text, isErr := h.call(getMemberTool(), map[string]any{"guild_id": "1", "user_id": "7"})
	if isErr || text != "alice (7) joined 2026-01-02 timed out until 2026-09-15 00:00" {
		t.Fatalf("text=%q", text)
	}
}

func TestListRoles(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{g}/roles", discordtest.JSON(200, []map[string]any{
		{"id": "1", "name": "@everyone", "position": 0, "permissions": "1024"},
		{"id": "2", "name": "Admin", "position": 3, "permissions": "8", "color": 16711680, "managed": false},
		{"id": "3", "name": "Bot", "position": 2, "permissions": "0", "managed": true},
	}))
	text, isErr := h.call(listRolesTool(), map[string]any{"guild_id": "1"})
	want := "Admin (2) position=3 permissions=8 color=#ff0000\n" +
		"Bot (3) position=2 permissions=0 [managed]\n" +
		"@everyone (1) position=0 permissions=1024"
	if isErr || text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", text, want)
	}
}

func TestRoleAssignment(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("PUT /api/v10/guilds/{g}/members/{u}/roles/{r}", discordtest.JSON(204, nil))
	h.handle("DELETE /api/v10/guilds/{g}/members/{u}/roles/{r}", discordtest.JSON(204, nil))
	args := map[string]any{"guild_id": "1", "user_id": "7", "role_id": "2", "reason": "promo"}
	if text, _ := h.call(addRoleTool(), args); text != "Added role 2 to user 7." || h.last().Path != "/api/v10/guilds/1/members/7/roles/2" {
		t.Fatalf("add: %q %s", text, h.last().Path)
	}
	if text, _ := h.call(removeRoleTool(), args); text != "Removed role 2 from user 7." || h.last().Method != "DELETE" {
		t.Fatalf("remove: %q", text)
	}
}

func TestTimeoutMember(t *testing.T) {
	timeNow = func() time.Time { return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { timeNow = time.Now })
	h := newHarness(t, auth.CapBot)
	h.handle("PATCH /api/v10/guilds/{g}/members/{u}", discordtest.JSON(200, member))

	text, isErr := h.call(timeoutMemberTool(), map[string]any{"guild_id": "1", "user_id": "7", "minutes": float64(90)})
	if isErr || text != "Timed out user 7 until 2026-09-14 13:30 UTC." {
		t.Fatalf("text=%q", text)
	}
	if !reflect.DeepEqual(h.lastBody(), map[string]any{"communication_disabled_until": "2026-09-14T13:30:00Z"}) {
		t.Fatalf("body = %v", h.lastBody())
	}
	text, _ = h.call(timeoutMemberTool(), map[string]any{"guild_id": "1", "user_id": "7", "minutes": float64(0)})
	if text != "Cleared the timeout of user 7." || !strings.Contains(string(h.last().Body), `"communication_disabled_until":null`) {
		t.Fatalf("clear: %q %s", text, h.last().Body)
	}
	if _, isErr := h.call(timeoutMemberTool(), map[string]any{"guild_id": "1", "user_id": "7", "minutes": float64(40321)}); !isErr {
		t.Fatal("more than 28 days must fail")
	}
}

func TestKickBanUnban(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("DELETE /api/v10/guilds/{g}/members/{u}", discordtest.JSON(204, nil))
	h.handle("PUT /api/v10/guilds/{g}/bans/{u}", discordtest.JSON(204, nil))
	h.handle("DELETE /api/v10/guilds/{g}/bans/{u}", discordtest.JSON(204, nil))

	if text, _ := h.call(kickMemberTool(), map[string]any{"guild_id": "1", "user_id": "7", "reason": "rules"}); text != "Kicked user 7." {
		t.Fatalf("kick: %q", text)
	}
	if h.last().Header.Get("X-Audit-Log-Reason") != "rules" {
		t.Fatal("kick reason not sent")
	}
	if text, _ := h.call(banMemberTool(), map[string]any{"guild_id": "1", "user_id": "7", "delete_message_hours": float64(24)}); text != "Banned user 7." {
		t.Fatalf("ban: %q", text)
	}
	if !reflect.DeepEqual(h.lastBody(), map[string]any{"delete_message_seconds": float64(86400)}) {
		t.Fatalf("ban body = %v", h.lastBody())
	}
	if text, _ := h.call(unbanMemberTool(), map[string]any{"guild_id": "1", "user_id": "7"}); text != "Unbanned user 7." {
		t.Fatalf("unban: %q", text)
	}
}

func TestModerationToolsAreDestructive(t *testing.T) {
	for _, tool := range []Tool{kickMemberTool(), banMemberTool(), timeoutMemberTool(), removeRoleTool()} {
		if a := tool.Def.Annotations; a.DestructiveHint == nil || !*a.DestructiveHint {
			t.Errorf("%s must be annotated destructive", tool.Def.Name)
		}
	}
	for _, tool := range []Tool{listMembersTool(), listRolesTool()} {
		if a := tool.Def.Annotations; a.ReadOnlyHint == nil || !*a.ReadOnlyHint {
			t.Errorf("%s must be annotated read-only", tool.Def.Name)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tools/ -run 'Member|Role|Kick'`
Expected: FAIL — `undefined: listMembersTool`.

- [ ] **Step 3: Implement the member tools**

`internal/tools/members.go`:

```go
package tools

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

// timeNow is replaced in tests.
var timeNow = time.Now

const maxTimeoutMinutes = 28 * 24 * 60

func memberTools() []Tool {
	return []Tool{
		listMembersTool(), getMemberTool(), searchMembersTool(), listRolesTool(), addRoleTool(), removeRoleTool(),
		timeoutMemberTool(), kickMemberTool(), banMemberTool(), unbanMemberTool(),
	}
}

type memberJSON struct {
	User                       userJSON   `json:"user"`
	Nick                       string     `json:"nick"`
	Roles                      []string   `json:"roles"`
	JoinedAt                   time.Time  `json:"joined_at"`
	CommunicationDisabledUntil *time.Time `json:"communication_disabled_until"`
}

func renderMember(m memberJSON) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s (%s)", displayName(m.User), m.User.ID)
	if m.Nick != "" {
		fmt.Fprintf(&sb, " nick=%s", m.Nick)
	}
	if len(m.Roles) > 0 {
		fmt.Fprintf(&sb, " roles=%s", strings.Join(m.Roles, ","))
	}
	fmt.Fprintf(&sb, " joined %s", m.JoinedAt.UTC().Format("2006-01-02"))
	if u := m.CommunicationDisabledUntil; u != nil && u.After(timeNow()) {
		fmt.Fprintf(&sb, " timed out until %s", u.UTC().Format(timeLayout))
	}
	return sb.String()
}

func renderMembers(ms []memberJSON) string {
	if len(ms) == 0 {
		return "No members."
	}
	lines := make([]string, len(ms))
	for i, m := range ms {
		lines[i] = renderMember(m)
	}
	return strings.Join(lines, "\n")
}

func guildParam(a Args) (map[string]string, error) {
	g, err := a.ID("guild_id")
	if err != nil {
		return nil, err
	}
	return map[string]string{"guild_id": g}, nil
}

func guildUserParams(a Args) (map[string]string, error) {
	params, err := guildParam(a)
	if err != nil {
		return nil, err
	}
	u, err := a.ID("user_id")
	if err != nil {
		return nil, err
	}
	params["user_id"] = u
	return params, nil
}

func listMembersTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_list_members",
			mcp.WithDescription("List guild members in user ID order. Requires the Server Members intent enabled in the Developer Portal."),
			readOnly(), idParam("guild_id", "Guild"),
			mcp.WithNumber("limit", mcp.Description("Members, 1-1000 (default 100)")),
			optIDParam("after", "Only members with a user ID after this one, for paging"),
			withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			params, err := guildParam(a)
			if err != nil {
				return "", err
			}
			limit, err := a.Int("limit", 100, 1, 1000)
			if err != nil {
				return "", err
			}
			after, err := a.OptionalID("after")
			if err != nil {
				return "", err
			}
			q := url.Values{"limit": {strconv.Itoa(limit)}}
			if after != "" {
				q.Set("after", after)
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/guilds/{guild_id}/members", Params: params, Query: q})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderMembers)
		},
	}
}

func getMemberTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_get_member",
			mcp.WithDescription("Show one guild member: nickname, roles, join date, timeout."),
			readOnly(), idParam("guild_id", "Guild"), idParam("user_id", "User"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			params, err := guildUserParams(a)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/guilds/{guild_id}/members/{user_id}", Params: params})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderMember)
		},
	}
}

func searchMembersTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_search_members",
			mcp.WithDescription("Find guild members whose username or nickname starts with query."),
			readOnly(), idParam("guild_id", "Guild"),
			mcp.WithString("query", mcp.Required(), mcp.Description("Name prefix")),
			mcp.WithNumber("limit", mcp.Description("Members, 1-1000 (default 25)")),
			withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			params, err := guildParam(a)
			if err != nil {
				return "", err
			}
			query, err := a.RequiredString("query")
			if err != nil {
				return "", err
			}
			limit, err := a.Int("limit", 25, 1, 1000)
			if err != nil {
				return "", err
			}
			q := url.Values{"query": {query}, "limit": {strconv.Itoa(limit)}}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/guilds/{guild_id}/members/search", Params: params, Query: q})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderMembers)
		},
	}
}

type roleJSON struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       int    `json:"color"`
	Position    int    `json:"position"`
	Permissions string `json:"permissions"`
	Managed     bool   `json:"managed"`
}

func listRolesTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_list_roles",
			mcp.WithDescription("List a guild's roles, highest first, with their permission bitsets."),
			readOnly(), idParam("guild_id", "Guild"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			params, err := guildParam(a)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/guilds/{guild_id}/roles", Params: params})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(rs []roleJSON) string {
				slices.SortFunc(rs, func(a, b roleJSON) int { return cmp.Compare(b.Position, a.Position) })
				lines := make([]string, len(rs))
				for i, r := range rs {
					lines[i] = fmt.Sprintf("%s (%s) position=%d permissions=%s", r.Name, r.ID, r.Position, r.Permissions)
					if r.Color != 0 {
						lines[i] += fmt.Sprintf(" color=#%06x", r.Color)
					}
					if r.Managed {
						lines[i] += " [managed]"
					}
				}
				return strings.Join(lines, "\n")
			})
		},
	}
}

func roleTool(name, desc, method, verb, prep string, ann mcp.ToolOption) Tool {
	return Tool{
		Def: mcp.NewTool(name, mcp.WithDescription(desc), ann,
			idParam("guild_id", "Guild"), idParam("user_id", "Member"), idParam("role_id", "Role"), withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			params, err := guildUserParams(a)
			if err != nil {
				return "", err
			}
			role, err := a.ID("role_id")
			if err != nil {
				return "", err
			}
			params["role_id"] = role
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			if _, err := p.Client.Do(ctx, discord.Call{Method: method, Route: "/guilds/{guild_id}/members/{user_id}/roles/{role_id}", Params: params, Reason: reason}); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s role %s %s user %s.", verb, role, prep, params["user_id"]), nil
		},
	}
}

func addRoleTool() Tool {
	return roleTool("discord_add_role", "Give a member a role.", "PUT", "Added", "to", mutating())
}

func removeRoleTool() Tool {
	return roleTool("discord_remove_role", "Take a role from a member.", "DELETE", "Removed", "from", destructive())
}

func timeoutMemberTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_timeout_member",
			mcp.WithDescription("Time a member out for a number of minutes (max 40320 = 28 days); 0 clears an existing timeout."),
			destructive(), idParam("guild_id", "Guild"), idParam("user_id", "Member"),
			mcp.WithNumber("minutes", mcp.Required(), mcp.Description("Duration in minutes, 0-40320")),
			withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			params, err := guildUserParams(a)
			if err != nil {
				return "", err
			}
			if _, ok := a.present("minutes"); !ok {
				return "", argErr("minutes is required")
			}
			minutes, err := a.Int("minutes", 0, 0, maxTimeoutMinutes)
			if err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			var until *string
			var untilTime time.Time
			if minutes > 0 {
				untilTime = timeNow().UTC().Add(time.Duration(minutes) * time.Minute)
				s := untilTime.Format(time.RFC3339)
				until = &s
			}
			body := map[string]any{"communication_disabled_until": until}
			if _, err := p.Client.Do(ctx, discord.Call{Method: "PATCH", Route: "/guilds/{guild_id}/members/{user_id}", Params: params, Body: body, Reason: reason}); err != nil {
				return "", err
			}
			if minutes == 0 {
				return fmt.Sprintf("Cleared the timeout of user %s.", params["user_id"]), nil
			}
			return fmt.Sprintf("Timed out user %s until %s UTC.", params["user_id"], untilTime.Format(timeLayout)), nil
		},
	}
}

func kickMemberTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_kick_member",
			mcp.WithDescription("Remove a member from the guild. They can rejoin with an invite."),
			destructive(), idParam("guild_id", "Guild"), idParam("user_id", "Member"), withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			params, err := guildUserParams(a)
			if err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			if _, err := p.Client.Do(ctx, discord.Call{Method: "DELETE", Route: "/guilds/{guild_id}/members/{user_id}", Params: params, Reason: reason}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Kicked user %s.", params["user_id"]), nil
		},
	}
}

func banMemberTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_ban_member",
			mcp.WithDescription("Ban a user from the guild, optionally deleting their recent messages."),
			destructive(), idParam("guild_id", "Guild"), idParam("user_id", "User"),
			mcp.WithNumber("delete_message_hours", mcp.Description("Delete the user's messages from the last 0-168 hours (default 0)")),
			withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			params, err := guildUserParams(a)
			if err != nil {
				return "", err
			}
			hours, err := a.Int("delete_message_hours", 0, 0, 168)
			if err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			body := map[string]any{"delete_message_seconds": hours * 3600}
			if _, err := p.Client.Do(ctx, discord.Call{Method: "PUT", Route: "/guilds/{guild_id}/bans/{user_id}", Params: params, Body: body, Reason: reason}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Banned user %s.", params["user_id"]), nil
		},
	}
}

func unbanMemberTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_unban_member",
			mcp.WithDescription("Lift a user's ban."),
			mutating(), idParam("guild_id", "Guild"), idParam("user_id", "User"), withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			params, err := guildUserParams(a)
			if err != nil {
				return "", err
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			if _, err := p.Client.Do(ctx, discord.Call{Method: "DELETE", Route: "/guilds/{guild_id}/bans/{user_id}", Params: params, Reason: reason}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Unbanned user %s.", params["user_id"]), nil
		},
	}
}
```

In `internal/tools/sets.go`, add to `BotTools`:

```go
	out = append(out, memberTools()...)
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tools/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools
git commit -m "feat(tools): add member, role and moderation tools" -m "Add discord_list_members, discord_get_member, discord_search_members,
discord_list_roles, discord_add_role, discord_remove_role,
discord_timeout_member, discord_kick_member, discord_ban_member and
discord_unban_member.

Timeouts take minutes up to Discord's 28-day limit and 0 clears one;
bans take delete_message_hours up to 7 days. Removing roles, timeouts,
kicks and bans are annotated destructive so clients can confirm them,
and every moderation tool forwards reason to Discord's audit log."
```

---
### Task 14: `discord_request` (long-tail API access)

**Files:**
- Create: `internal/tools/request.go`
- Modify: `internal/tools/sets.go` (`BotTools`)
- Test: `internal/tools/request_test.go`

**Interfaces:**
- Consumes: `(*discord.Client).DoRaw`, `discord.APIError` (Task 4); Task 9 helpers.
- Produces: `requestTool() Tool` (`discord_request`); constant `maxRawResponse = 64 << 10`.

- [ ] **Step 1: Write the failing tests**

`internal/tools/request_test.go`:

```go
package tools

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
)

func TestRequestGetWithQuery(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/guilds/{g}/audit-logs", discordtest.JSON(200, map[string]any{"audit_log_entries": []any{}}))
	text, isErr := h.call(requestTool(), map[string]any{
		"method": "GET", "route": "/guilds/1/audit-logs",
		"query": map[string]any{"limit": float64(5), "action_type": "22", "with": []any{"a", "b"}, "flag": true},
	})
	if isErr {
		t.Fatal(text)
	}
	var out struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil || out.Status != 200 || !strings.Contains(string(out.Body), "audit_log_entries") {
		t.Fatalf("text = %s", text)
	}
	q := h.last().Query
	if q.Get("limit") != "5" || q.Get("action_type") != "22" || strings.Join(q["with"], ",") != "a,b" || q.Get("flag") != "true" {
		t.Fatalf("query = %v", q)
	}
}

func TestRequestPostBodyAndReason(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/guilds/{g}/emojis", discordtest.JSON(201, map[string]any{"id": "3"}))
	text, isErr := h.call(requestTool(), map[string]any{
		"method": "POST", "route": "/guilds/1/emojis", "body": map[string]any{"name": "party"}, "reason": "new emoji",
	})
	if isErr || !strings.HasPrefix(text, `{"status":201`) {
		t.Fatalf("text=%s", text)
	}
	if h.lastBody()["name"] != "party" || h.last().Header.Get("X-Audit-Log-Reason") != "new%20emoji" {
		t.Fatalf("body=%v reason=%q", h.lastBody(), h.last().Header.Get("X-Audit-Log-Reason"))
	}
}

func TestRequestRejectedBeforeSending(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"host in route":  {"method": "GET", "route": "//evil.example/x"},
		"dot segments":   {"method": "GET", "route": "/channels/%2e%2e/oauth2"},
		"bad method":     {"method": "TRACE", "route": "/users/@me"},
		"missing route":  {"method": "GET"},
		"body on GET":    {"method": "GET", "route": "/users/@me", "body": map[string]any{"a": 1.0}},
		"object in query": {"method": "GET", "route": "/users/@me", "query": map[string]any{"x": map[string]any{}}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, auth.CapBot)
			if _, isErr := h.call(requestTool(), args); !isErr || h.requests() != 0 {
				t.Fatalf("isErr=%v requests=%d", isErr, h.requests())
			}
			if h.lastAudit()["outcome"] != "invalid_args" {
				t.Fatalf("audit = %v", h.lastAudit())
			}
		})
	}
}

func TestRequestTruncatesLargeResponse(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	big := strings.Repeat("x", maxRawResponse+10)
	h.handle("GET /api/v10/users/@me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"blob":"` + big + `"}`))
	})
	text, isErr := h.call(requestTool(), map[string]any{"method": "GET", "route": "/users/@me"})
	if isErr {
		t.Fatal("request failed")
	}
	var out struct {
		Status     int    `json:"status"`
		Truncated  bool   `json:"truncated"`
		Bytes      int    `json:"bytes"`
		BodyPrefix string `json:"body_prefix"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil || !out.Truncated || out.Bytes != len(big)+11 || len(out.BodyPrefix) != maxRawResponse {
		t.Fatalf("truncated=%v bytes=%d prefix=%d err=%v", out.Truncated, out.Bytes, len(out.BodyPrefix), err)
	}
}

func TestRequestErrorIncludesDiscordDetails(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("POST /api/v10/guilds/{g}/roles", discordtest.JSON(400, map[string]any{
		"message": "Invalid Form Body", "code": 50035,
		"errors": map[string]any{"name": map[string]any{"_errors": []any{map[string]any{"message": "Must be 100 or fewer in length."}}}},
	}))
	text, isErr := h.call(requestTool(), map[string]any{"method": "POST", "route": "/guilds/1/roles", "body": map[string]any{"name": "x"}})
	if !isErr || !strings.Contains(text, "Discord 400: Invalid Form Body (50035)") || !strings.Contains(text, "Must be 100 or fewer") {
		t.Fatalf("text = %s", text)
	}
	if h.lastAudit()["outcome"] != "discord_error" {
		t.Fatalf("audit = %v", h.lastAudit())
	}
}

func TestRequestAuditRedactsWebhookToken(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	h.handle("GET /api/v10/webhooks/{id}/{token}", discordtest.JSON(200, map[string]any{"id": "1"}))
	h.call(requestTool(), map[string]any{"method": "GET", "route": "/webhooks/1/SECRETWEBHOOKTOKEN"})
	line := h.logs.String()
	if strings.Contains(line, "SECRETWEBHOOKTOKEN") {
		t.Fatalf("audit leaks webhook token: %s", line)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tools/ -run Request`
Expected: FAIL — `undefined: requestTool`.

- [ ] **Step 3: Implement `discord_request`**

`internal/tools/request.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

const (
	maxRawResponse   = 64 << 10
	maxRawErrorBody  = 4 << 10
)

func requestTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_request",
			mcp.WithDescription("Call any Discord REST API endpoint not covered by another tool, as the bot. "+
				"route is relative to https://discord.com/api/v10, e.g. /guilds/123/emojis. "+
				"Returns {status, body}; bodies over 64 KB are truncated."),
			destructive(),
			mcp.WithString("method", mcp.Required(), mcp.Enum("GET", "POST", "PUT", "PATCH", "DELETE")),
			mcp.WithString("route", mcp.Required(), mcp.Description("Path relative to /api/v10, starting with /; no query string")),
			mcp.WithObject("query", mcp.Description("Query parameters; array values repeat the parameter")),
			mcp.WithAny("body", mcp.Description("JSON body (not allowed with GET)")),
			withReason()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			method, err := a.RequiredString("method")
			if err != nil {
				return "", err
			}
			route, err := a.RequiredString("route")
			if err != nil {
				return "", err
			}
			rawQuery, err := a.Object("query")
			if err != nil {
				return "", err
			}
			query, err := toQuery(rawQuery)
			if err != nil {
				return "", err
			}
			body, hasBody := a.present("body")
			if hasBody && method == "GET" {
				return "", argErr("body is not allowed with GET")
			}
			reason, err := a.String("reason")
			if err != nil {
				return "", err
			}
			resp, err := p.Client.DoRaw(ctx, method, route, query, body, reason)
			if err != nil {
				var ae *discord.APIError
				if errors.As(err, &ae) && len(resp.Body) > 0 {
					return "", fmt.Errorf("%w\n%s", err, truncateString(string(resp.Body), maxRawErrorBody))
				}
				return "", err
			}
			return rawResult(resp), nil
		},
	}
}

func toQuery(m map[string]any) (url.Values, error) {
	if len(m) == 0 {
		return nil, nil
	}
	q := url.Values{}
	for k, v := range m {
		values := []any{v}
		if list, ok := v.([]any); ok {
			values = list
		}
		for _, item := range values {
			s, err := queryValue(k, item)
			if err != nil {
				return nil, err
			}
			q.Add(k, s)
		}
	}
	return q, nil
}

func queryValue(key string, v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case float64:
		if x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10), nil
		}
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	}
	return "", argErr("query.%s must be a string, number, boolean or an array of those", key)
}

func rawResult(resp discord.Response) string {
	if len(resp.Body) > maxRawResponse {
		out, _ := json.Marshal(map[string]any{
			"status":      resp.Status,
			"truncated":   true,
			"bytes":       len(resp.Body),
			"body_prefix": strings.ToValidUTF8(string(resp.Body[:maxRawResponse]), ""),
		})
		return string(out)
	}
	body := json.RawMessage("null")
	if len(resp.Body) > 0 && json.Valid(resp.Body) {
		body = resp.Body
	}
	out, _ := json.Marshal(struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}{resp.Status, body})
	return string(out)
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "…"
}
```

In `internal/tools/sets.go`, add to `BotTools`:

```go
	out = append(out, requestTool())
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tools/`
Expected: PASS. In `TestRequestTruncatesLargeResponse`, the fake writes `{"blob":"` (9 bytes) + big + `"}` (2 bytes), so `bytes` is `len(big)+11`.

- [ ] **Step 5: Commit**

```bash
git add internal/tools
git commit -m "feat(tools): add discord_request for the rest of the api" -m "discord_request reaches any Discord REST endpoint without a typed tool:
method, a route relative to /api/v10, query parameters, a JSON body and
an audit log reason. The route guard runs before anything is sent, so
no argument can direct the bot token to another host.

The result is {status, body}; bodies over 64 KB come back as a
truncated prefix. Discord errors keep their field-level details so the
LLM can correct the request. The audit line redacts webhook tokens a
route may contain and truncates large bodies."
```

---

### Task 15: Event tools

**Files:**
- Create: `internal/tools/events.go`
- Modify: `internal/tools/sets.go` (`EventTools`)
- Test: `internal/tools/events_test.go`

**Interfaces:**
- Consumes: `events.Buffer.Since`, `events.Buffer.Wait`, `events.Filter`, `events.Event`, `events.ErrBadCursor`, `events.ErrClosed`, `events.TypeGatewayReconnected` (Tasks 6–7); `auth.Principal.Events`, `auth.Principal.BotUserID`; Task 9 helpers, `renderMessage`, `messageJSON`.
- Produces: `pollEventsTool()`, `waitForMessageTool()`; `EventTools()` returns both; constants `defaultWaitSeconds = 60`, `maxWaitSeconds = 300`.

- [ ] **Step 1: Write the failing tests**

`internal/tools/events_test.go`:

```go
package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Hellhium/discord-mcp/internal/auth"
)

func appendMsg(h *harness, channel, author, content string) {
	h.p.Events.Append("MESSAGE_CREATE", json.RawMessage(fmt.Sprintf(
		`{"id":"1%s","channel_id":%q,"guild_id":"9","content":%q,"author":{"id":%q,"username":"u%s"},"timestamp":"2026-09-14T11:02:00Z"}`,
		author, channel, content, author, author)))
}

func TestPollEvents(t *testing.T) {
	h := newHarness(t, auth.CapBotEvents)
	appendMsg(h, "5", "7", "hello")
	h.p.Events.Append("MESSAGE_REACTION_ADD", json.RawMessage(`{"channel_id":"5","user_id":"8","guild_id":"9","emoji":{"name":"👍"}}`))

	text, isErr := h.call(pollEventsTool(), map[string]any{})
	if isErr {
		t.Fatal(text)
	}
	cursor := h.p.Events.Cursor()
	for _, want := range []string{"next_cursor: " + cursor, "#1 MESSAGE_CREATE [2026-09-14 11:02] u7 (7) #17: hello", "#2 MESSAGE_REACTION_ADD guild=9 channel=5 user=8"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}

	appendMsg(h, "6", "7", "elsewhere")
	text, _ = h.call(pollEventsTool(), map[string]any{"cursor": cursor, "channel_id": "5"})
	if strings.Contains(text, "elsewhere") || !strings.Contains(text, "No new events.") {
		t.Fatalf("filtered poll:\n%s", text)
	}

	raw, _ := h.call(pollEventsTool(), map[string]any{"cursor": cursor, "format": "json"})
	var page struct {
		Events     []map[string]any `json:"events"`
		NextCursor string           `json:"next_cursor"`
		Gap        bool             `json:"gap"`
	}
	if err := json.Unmarshal([]byte(raw), &page); err != nil || len(page.Events) != 1 || page.NextCursor == "" {
		t.Fatalf("json page = %s (%v)", raw, err)
	}
}

func TestPollEventsReportsGapAndBadCursor(t *testing.T) {
	h := newHarness(t, auth.CapBotEvents)
	appendMsg(h, "5", "7", "hi")
	text, isErr := h.call(pollEventsTool(), map[string]any{"cursor": "oldboot:3"})
	if isErr || !strings.Contains(text, "gap: some events were missed") {
		t.Fatalf("text:\n%s", text)
	}
	if _, isErr := h.call(pollEventsTool(), map[string]any{"cursor": "garbage"}); !isErr || h.lastAudit()["outcome"] != "invalid_args" {
		t.Fatalf("bad cursor: isErr=%v audit=%v", isErr, h.lastAudit())
	}
}

func TestWaitForMessageIgnoresSelfByDefault(t *testing.T) {
	h := newHarness(t, auth.CapBotEvents)
	go func() {
		time.Sleep(30 * time.Millisecond)
		appendMsg(h, "5", hBotUserID, "from the bot")
		appendMsg(h, "5", "7", "from a user")
	}()
	text, isErr := h.call(waitForMessageTool(), map[string]any{"channel_id": "5", "timeout_seconds": float64(5)})
	if isErr || !strings.Contains(text, "from a user") || strings.Contains(text, "from the bot") || !strings.Contains(text, "next_cursor: ") {
		t.Fatalf("text:\n%s", text)
	}
}

func TestWaitForMessageIncludeSelf(t *testing.T) {
	h := newHarness(t, auth.CapBotEvents)
	cursor := h.p.Events.Cursor()
	appendMsg(h, "5", hBotUserID, "from the bot")
	text, _ := h.call(waitForMessageTool(), map[string]any{"cursor": cursor, "include_self": true, "timeout_seconds": float64(1)})
	if !strings.Contains(text, "from the bot") {
		t.Fatalf("text:\n%s", text)
	}
}

func TestWaitForMessageTimeoutIsNotAnError(t *testing.T) {
	h := newHarness(t, auth.CapBotEvents)
	text, isErr := h.call(waitForMessageTool(), map[string]any{"channel_id": "5", "timeout_seconds": float64(1)})
	if isErr || !strings.HasPrefix(text, "No matching message within 1s.") {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
	if h.lastAudit()["outcome"] != "ok" {
		t.Fatalf("audit = %v", h.lastAudit())
	}
	raw, _ := h.call(waitForMessageTool(), map[string]any{"timeout_seconds": float64(1), "format": "json"})
	if !strings.Contains(raw, `"message":null`) || !strings.Contains(raw, `"next_cursor"`) {
		t.Fatalf("json = %s", raw)
	}
}

func TestWaitForMessageTimeoutBounds(t *testing.T) {
	h := newHarness(t, auth.CapBotEvents)
	if _, isErr := h.call(waitForMessageTool(), map[string]any{"timeout_seconds": float64(301)}); !isErr {
		t.Fatal("timeout above 300s must fail")
	}
}

func TestEventToolsWithoutBuffer(t *testing.T) {
	h := newHarness(t, auth.CapBot)
	if text, isErr := h.call(pollEventsTool(), nil); !isErr || !strings.Contains(text, "not enabled") {
		t.Fatalf("text=%q", text)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tools/ -run 'Poll|Wait|EventTools'`
Expected: FAIL — `undefined: pollEventsTool`.

- [ ] **Step 3: Implement the event tools**

`internal/tools/events.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/events"
)

const (
	defaultWaitSeconds = 60
	maxWaitSeconds     = 300
)

func buffer(p *auth.Principal) (*events.Buffer, error) {
	if p.Events == nil {
		return nil, argErr("events are not enabled for this credential")
	}
	return p.Events, nil
}

// cursorErr turns buffer cursor errors into argument errors.
func cursorErr(err error) error {
	if errors.Is(err, events.ErrBadCursor) {
		return argErr("%s", err.Error())
	}
	return err
}

func renderEvent(e events.Event) string {
	if e.Type == "MESSAGE_CREATE" || e.Type == "MESSAGE_UPDATE" {
		var m messageJSON
		if json.Unmarshal(e.Data, &m) == nil && m.ID != "" {
			return fmt.Sprintf("#%d %s %s", e.Seq, e.Type, renderMessage(m))
		}
	}
	if e.Type == events.TypeGatewayReconnected {
		return fmt.Sprintf("#%d %s: the Gateway reconnected; events before this may be missing", e.Seq, e.Type)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "#%d %s", e.Seq, e.Type)
	for _, kv := range [][2]string{{"guild", e.GuildID}, {"channel", e.ChannelID}, {"user", e.AuthorID}} {
		if kv[1] != "" {
			fmt.Fprintf(&sb, " %s=%s", kv[0], kv[1])
		}
	}
	return sb.String()
}

const gapNote = "gap: some events were missed (cursor older than the buffer, or the server restarted)"

func pollEventsTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_poll_events",
			mcp.WithDescription("Read buffered Discord Gateway events (new messages, reactions, member joins…) after a cursor. "+
				"Without a cursor, returns the latest events. Pass next_cursor back to continue."),
			readOnly(),
			mcp.WithString("cursor", mcp.Description("next_cursor from a previous call")),
			mcp.WithArray("types", mcp.Description("Only these event types, e.g. MESSAGE_CREATE"), mcp.Items(map[string]any{"type": "string"})),
			optIDParam("guild_id", "Only events in this guild"),
			optIDParam("channel_id", "Only events in this channel"),
			mcp.WithNumber("limit", mcp.Description("Events, 1-100 (default 50)")),
			withFormat()),
		Handle: func(_ context.Context, p *auth.Principal, a Args) (string, error) {
			buf, err := buffer(p)
			if err != nil {
				return "", err
			}
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			cursor, err := a.String("cursor")
			if err != nil {
				return "", err
			}
			types, err := a.StringList("types")
			if err != nil {
				return "", err
			}
			guild, err := a.OptionalID("guild_id")
			if err != nil {
				return "", err
			}
			channel, err := a.OptionalID("channel_id")
			if err != nil {
				return "", err
			}
			limit, err := a.Int("limit", 50, 1, 100)
			if err != nil {
				return "", err
			}
			page, err := buf.Since(cursor, events.Filter{Types: types, GuildID: guild, ChannelID: channel}, limit)
			if err != nil {
				return "", cursorErr(err)
			}
			if format == "json" {
				evs := page.Events
				if evs == nil {
					evs = []events.Event{}
				}
				out, _ := json.Marshal(map[string]any{"events": evs, "next_cursor": page.NextCursor, "gap": page.Gap})
				return string(out), nil
			}
			lines := []string{"next_cursor: " + page.NextCursor}
			if page.Gap {
				lines = append(lines, gapNote)
			}
			if len(page.Events) == 0 {
				lines = append(lines, "No new events.")
			}
			for _, e := range page.Events {
				lines = append(lines, renderEvent(e))
			}
			return strings.Join(lines, "\n"), nil
		},
	}
}

func waitForMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_wait_for_message",
			mcp.WithDescription("Wait for the next new message, optionally in one channel or from one user. "+
				"Ignores the bot's own messages unless include_self. A timeout is a normal result that returns a cursor to keep waiting from."),
			readOnly(),
			optIDParam("channel_id", "Only messages in this channel"),
			optIDParam("author_id", "Only messages from this user"),
			mcp.WithBoolean("include_self", mcp.Description("Also match the bot's own messages (default false)")),
			mcp.WithString("cursor", mcp.Description("Wait for messages after this cursor (default: from now)")),
			mcp.WithNumber("timeout_seconds", mcp.Description("1-300 (default 60)")),
			withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			buf, err := buffer(p)
			if err != nil {
				return "", err
			}
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			channel, err := a.OptionalID("channel_id")
			if err != nil {
				return "", err
			}
			author, err := a.OptionalID("author_id")
			if err != nil {
				return "", err
			}
			includeSelf, err := a.Bool("include_self", false)
			if err != nil {
				return "", err
			}
			cursor, err := a.String("cursor")
			if err != nil {
				return "", err
			}
			seconds, err := a.Int("timeout_seconds", defaultWaitSeconds, 1, maxWaitSeconds)
			if err != nil {
				return "", err
			}
			f := events.Filter{Types: []string{"MESSAGE_CREATE"}, ChannelID: channel, AuthorID: author}
			if !includeSelf {
				f.ExcludeAuthorID = p.BotUserID
			}

			waitCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
			defer cancel()
			ev, next, gap, err := buf.Wait(waitCtx, cursor, f)
			switch {
			case err == nil:
			case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
				ev = nil
			case errors.Is(err, events.ErrClosed):
				return "", errors.New("the server is shutting down")
			case errors.Is(err, events.ErrBadCursor):
				return "", cursorErr(err)
			default:
				return "", fmt.Errorf("wait cancelled: %w", err)
			}

			if format == "json" {
				var msg json.RawMessage = json.RawMessage("null")
				if ev != nil {
					msg = ev.Data
				}
				out, _ := json.Marshal(map[string]any{"message": msg, "next_cursor": next, "gap": gap})
				return string(out), nil
			}
			var lines []string
			if ev == nil {
				lines = append(lines, fmt.Sprintf("No matching message within %ds.", seconds))
			} else {
				lines = append(lines, renderEvent(*ev))
			}
			if gap {
				lines = append(lines, gapNote)
			}
			lines = append(lines, "next_cursor: "+next)
			return strings.Join(lines, "\n"), nil
		},
	}
}
```

In `internal/tools/sets.go`, change `EventTools` to:

```go
func EventTools() []Tool {
	return []Tool{pollEventsTool(), waitForMessageTool()}
}
```

- [ ] **Step 4: Run the tests with the race detector**

Run: `go test -race ./internal/tools/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools
git commit -m "feat(tools): add poll_events and wait_for_message" -m "Configured bots with events get two tools over the event buffer.
discord_poll_events pages through events after a cursor, filtered by
type, guild or channel, and reports a gap when events were missed.
discord_wait_for_message blocks up to 300 seconds for the next matching
message, ignoring the bot's own messages unless include_self; a timeout
is a normal result carrying the cursor to keep waiting from, and a
shutdown or a client disconnect ends the wait."
```

---
### Task 16: Webhook tools

**Files:**
- Create: `internal/tools/webhook.go`
- Modify: `internal/tools/sets.go` (`WebhookTools`)
- Test: `internal/tools/webhook_test.go`

**Interfaces:**
- Consumes: `auth.Principal.Webhook` (Task 8); Task 9 helpers; `sendResult`, `renderMessage` (Tasks 9, 11).
- Produces: `webhookGetTool()`, `webhookSendTool()`, `webhookGetMessageTool()`, `webhookEditMessageTool()`, `webhookDeleteMessageTool()`; `WebhookTools()` returns all five.

- [ ] **Step 1: Write the failing tests**

`internal/tools/webhook_test.go`:

```go
package tools

import (
	"strings"
	"testing"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discordtest"
)

const hookPath = "/api/v10/webhooks/" + hWebhookID + "/" + hWebhookToken

func TestWebhookGet(t *testing.T) {
	h := newHarness(t, auth.CapWebhook)
	h.handle("GET /api/v10/webhooks/{id}/{token}", discordtest.JSON(200, map[string]any{"id": hWebhookID, "name": "alerts", "channel_id": "5", "guild_id": "9"}))
	text, isErr := h.call(webhookGetTool(), nil)
	if isErr || text != "Webhook alerts (555) posts to channel 5 in guild 9." {
		t.Fatalf("text=%q", text)
	}
	if h.last().Path != hookPath || h.last().Header.Get("Authorization") != "" {
		t.Fatalf("path=%s auth=%q", h.last().Path, h.last().Header.Get("Authorization"))
	}
}

func TestWebhookGetJSONHidesToken(t *testing.T) {
	h := newHarness(t, auth.CapWebhook)
	h.handle("GET /api/v10/webhooks/{id}/{token}", discordtest.JSON(200, map[string]any{
		"id": hWebhookID, "name": "alerts", "token": hWebhookToken, "url": "https://discord.com/api/webhooks/555/" + hWebhookToken,
	}))
	text, isErr := h.call(webhookGetTool(), map[string]any{"format": "json"})
	if isErr || strings.Contains(text, hWebhookToken) || !strings.Contains(text, `"name":"alerts"`) {
		t.Fatalf("text = %s", text)
	}
}

func TestWebhookSend(t *testing.T) {
	h := newHarness(t, auth.CapWebhook)
	h.handle("POST /api/v10/webhooks/{id}/{token}", discordtest.JSON(200, map[string]any{"id": "900", "channel_id": "5"}))
	text, isErr := h.call(webhookSendTool(), map[string]any{
		"content": "deploy done", "username": "CI", "avatar_url": "https://example.com/a.png", "thread_id": "66", "allow_mentions": false,
	})
	if isErr || text != "Sent message #900 in channel 5." {
		t.Fatalf("text=%q isErr=%v", text, isErr)
	}
	req := h.last()
	if req.Query.Get("wait") != "true" || req.Query.Get("thread_id") != "66" {
		t.Fatalf("query = %v", req.Query)
	}
	body := h.lastBody()
	if body["username"] != "CI" || body["avatar_url"] != "https://example.com/a.png" || body["content"] != "deploy done" || body["allowed_mentions"] == nil {
		t.Fatalf("body = %v", body)
	}
	logs := h.logs.String()
	if strings.Contains(logs, hWebhookToken) {
		t.Fatalf("audit leaks the webhook token: %s", logs)
	}
	if calls := h.lastAudit()["discord"].([]any); calls[0].(map[string]any)["route"] != "/webhooks/{webhook_id}/{webhook_token}" {
		t.Fatalf("audit route = %v", calls)
	}
	if p := h.lastAudit()["principal"].(map[string]any); p["kind"] != "instance" {
		t.Fatalf("principal = %v", p)
	}
}

func TestWebhookSendNeedsBody(t *testing.T) {
	h := newHarness(t, auth.CapWebhook)
	if _, isErr := h.call(webhookSendTool(), map[string]any{"username": "CI"}); !isErr || h.requests() != 0 {
		t.Fatalf("isErr=%v requests=%d", isErr, h.requests())
	}
}

func TestWebhookMessageLifecycle(t *testing.T) {
	h := newHarness(t, auth.CapWebhook)
	msg := map[string]any{"id": "900", "channel_id": "5", "content": "v1", "author": map[string]any{"id": hWebhookID, "username": "CI", "bot": true}, "timestamp": "2026-09-14T11:02:00Z"}
	h.handle("GET /api/v10/webhooks/{id}/{token}/messages/{m}", discordtest.JSON(200, msg))
	h.handle("PATCH /api/v10/webhooks/{id}/{token}/messages/{m}", discordtest.JSON(200, msg))
	h.handle("DELETE /api/v10/webhooks/{id}/{token}/messages/{m}", discordtest.JSON(204, nil))

	text, isErr := h.call(webhookGetMessageTool(), map[string]any{"message_id": "900", "thread_id": "66"})
	if isErr || text != "[2026-09-14 11:02] CI [bot] (555) #900: v1" || h.last().Query.Get("thread_id") != "66" {
		t.Fatalf("get: %q", text)
	}
	if text, isErr = h.call(webhookEditMessageTool(), map[string]any{"message_id": "900", "content": "v2"}); isErr || text != "Edited message #900." || h.lastBody()["content"] != "v2" {
		t.Fatalf("edit: %q", text)
	}
	if text, isErr = h.call(webhookDeleteMessageTool(), map[string]any{"message_id": "900"}); isErr || text != "Deleted message #900." || h.last().Path != hookPath+"/messages/900" {
		t.Fatalf("delete: %q %s", text, h.last().Path)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tools/ -run Webhook`
Expected: FAIL — `undefined: webhookGetTool`.

- [ ] **Step 3: Implement the webhook tools**

`internal/tools/webhook.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Hellhium/discord-mcp/internal/auth"
	"github.com/Hellhium/discord-mcp/internal/discord"
)

// Webhook tools take no webhook ID or token argument: the webhook is the
// principal's, so a caller can only ever act as the webhook it authenticated
// with.

func webhookParams(p *auth.Principal) map[string]string {
	return map[string]string{"webhook_id": p.Webhook.ID, "webhook_token": p.Webhook.Token}
}

func threadQuery(a Args) (url.Values, error) {
	thread, err := a.OptionalID("thread_id")
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if thread != "" {
		q.Set("thread_id", thread)
	}
	return q, nil
}

func webhookGetTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_webhook_get",
			mcp.WithDescription("Show this webhook: its name and the channel and guild it posts to."),
			readOnly(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: "/webhooks/{webhook_id}/{webhook_token}", Params: webhookParams(p)})
			if err != nil {
				return "", err
			}
			if format == "json" {
				// Discord's webhook object includes the token and URL; never
				// echo them back.
				var obj map[string]any
				if err := json.Unmarshal(resp.Body, &obj); err != nil {
					return "", fmt.Errorf("decode Discord response: %w", err)
				}
				delete(obj, "token")
				delete(obj, "url")
				out, _ := json.Marshal(obj)
				return string(out), nil
			}
			return output(resp.Body, format, func(w struct {
				ID        string `json:"id"`
				Name      string `json:"name"`
				ChannelID string `json:"channel_id"`
				GuildID   string `json:"guild_id"`
			}) string {
				return fmt.Sprintf("Webhook %s (%s) posts to channel %s in guild %s.", w.Name, w.ID, w.ChannelID, w.GuildID)
			})
		},
	}
}

func webhookSendTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_webhook_send",
			mcp.WithDescription("Post a message as this webhook. Provide content, embeds or attachments."),
			mutating(),
			mcp.WithString("content", mcp.Description("Message text (up to 2000 characters)")),
			mcp.WithString("username", mcp.Description("Override the webhook's display name for this message")),
			mcp.WithString("avatar_url", mcp.Description("Override the webhook's avatar for this message (Discord fetches it, not this server)")),
			withEmbeds(), withAttachments(),
			optIDParam("thread_id", "Post in this thread of the webhook's channel"),
			withAllowMentions(), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			body, files, err := messagePayload(a, true, true)
			if err != nil {
				return "", err
			}
			for _, k := range []string{"username", "avatar_url"} {
				v, err := a.String(k)
				if err != nil {
					return "", err
				}
				if v != "" {
					body[k] = v
				}
			}
			q, err := threadQuery(a)
			if err != nil {
				return "", err
			}
			q.Set("wait", "true")
			resp, err := p.Client.Do(ctx, discord.Call{Method: "POST", Route: "/webhooks/{webhook_id}/{webhook_token}", Params: webhookParams(p), Query: q, Body: body, Files: files})
			if err != nil {
				return "", err
			}
			return sendResult(resp, format)
		},
	}
}

func webhookMessageParams(p *auth.Principal, a Args) (map[string]string, url.Values, error) {
	m, err := a.ID("message_id")
	if err != nil {
		return nil, nil, err
	}
	q, err := threadQuery(a)
	if err != nil {
		return nil, nil, err
	}
	params := webhookParams(p)
	params["message_id"] = m
	return params, q, nil
}

const webhookMessageRoute = "/webhooks/{webhook_id}/{webhook_token}/messages/{message_id}"

func webhookGetMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_webhook_get_message",
			mcp.WithDescription("Read a message this webhook sent."),
			readOnly(), idParam("message_id", "Message"), optIDParam("thread_id", "Thread the message is in"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			params, q, err := webhookMessageParams(p, a)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "GET", Route: webhookMessageRoute, Params: params, Query: q})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, renderMessage)
		},
	}
}

func webhookEditMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_webhook_edit_message",
			mcp.WithDescription("Edit a message this webhook sent."),
			mutating(), idParam("message_id", "Message"),
			mcp.WithString("content", mcp.Description("New text")),
			withEmbeds(), withAllowMentions(), optIDParam("thread_id", "Thread the message is in"), withFormat()),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			format, err := formatArg(a)
			if err != nil {
				return "", err
			}
			params, q, err := webhookMessageParams(p, a)
			if err != nil {
				return "", err
			}
			body, _, err := messagePayload(a, true, false)
			if err != nil {
				return "", err
			}
			resp, err := p.Client.Do(ctx, discord.Call{Method: "PATCH", Route: webhookMessageRoute, Params: params, Query: q, Body: body})
			if err != nil {
				return "", err
			}
			return output(resp.Body, format, func(m messageJSON) string { return fmt.Sprintf("Edited message #%s.", m.ID) })
		},
	}
}

func webhookDeleteMessageTool() Tool {
	return Tool{
		Def: mcp.NewTool("discord_webhook_delete_message",
			mcp.WithDescription("Delete a message this webhook sent."),
			destructive(), idParam("message_id", "Message"), optIDParam("thread_id", "Thread the message is in")),
		Handle: func(ctx context.Context, p *auth.Principal, a Args) (string, error) {
			params, q, err := webhookMessageParams(p, a)
			if err != nil {
				return "", err
			}
			if _, err := p.Client.Do(ctx, discord.Call{Method: "DELETE", Route: webhookMessageRoute, Params: params, Query: q}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Deleted message #%s.", params["message_id"]), nil
		},
	}
}
```

In `internal/tools/sets.go`, change `WebhookTools` to:

```go
func WebhookTools() []Tool {
	return []Tool{webhookGetTool(), webhookSendTool(), webhookGetMessageTool(), webhookEditMessageTool(), webhookDeleteMessageTool()}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tools/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools
git commit -m "feat(tools): add webhook tools" -m "Webhook principals get discord_webhook_get, discord_webhook_send,
discord_webhook_get_message, discord_webhook_edit_message and
discord_webhook_delete_message, and nothing else. None takes a webhook ID
or token: the webhook is the one the caller authenticated with.

Sends use wait=true so the created message comes back, with optional
username, avatar_url and thread_id. Webhook requests carry no
Authorization header, the audit log shows only the route template, and
the json view of the webhook drops its token and URL."
```

---
### Task 17: Router, application wiring and end-to-end tests

**Files:**
- Create: `internal/router/router.go`, `internal/router/router_test.go`
- Create: `internal/app/app.go`
- Modify: `internal/tools/sets.go` (add `Instructions`)
- Test: `internal/e2e/e2e_test.go`

**Interfaces:**
- Consumes: `auth.Resolver`, `auth.Credential`, `auth.WithPrincipal`, `auth.FromContext`, `auth.InstanceEntry`, `auth.DirectOptions`, capabilities (Task 8); `audit.Logger` (Task 5); `config.Config` (Task 2); `credential.ParseWebhook`, `intents.Parse` (Task 1); `discord.NewBot`, `discord.NewWebhook`, `VerifyBot`, `VerifyWebhook`, `CheckPrivilegedIntents`, `Session` (Tasks 3–4); `events.NewBuffer`, `events.NewGateway` (Tasks 6–7); `tools.BotTools`, `tools.EventTools`, `tools.WebhookTools`, `tools.Register` (Tasks 9–16).
- Produces:
  - `router.HealthPath = "/healthz"`, `router.MCPPath = "/mcp"`
  - `router.Servers` (`map[auth.Capability]http.Handler`), `router.New(res *auth.Resolver, servers Servers, log *audit.Logger) *Router`
  - `tools.Instructions(c auth.Capability) string`
  - `app.Options{Version string; VerifyCredentials bool; Log *audit.Logger; HTTPClient *http.Client; GatewayReadyTimeout time.Duration}`
  - `app.Build(ctx context.Context, cfg *config.Config, opts Options) (*App, error)`; `(*App).Handler http.Handler`; `(*App).Close()`; `(*App).Summary() []string` (one startup log line per instance, no secrets)

- [ ] **Step 1: Write the failing router tests**

`internal/router/router_test.go`:

```go
package router

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/auth"
)

const tokBot, tokHook = "bot-token-bot-token-bot-token-00", "hook-token-hook-token-hook-toke"

func setup(t *testing.T) (http.Handler, *bytes.Buffer) {
	t.Helper()
	bot := &auth.Principal{Kind: auth.KindInstance, Capability: auth.CapBot, Instance: "bot"}
	hook := &auth.Principal{Kind: auth.KindInstance, Capability: auth.CapWebhook, Instance: "hook"}
	res, err := auth.NewResolver([]auth.InstanceEntry{
		{Tokens: []string{tokBot}, Principal: bot},
		{Tokens: []string{tokHook}, Principal: hook},
	}, auth.DirectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stub := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := auth.FromContext(r.Context())
			if !ok {
				t.Error("no principal in context")
				return
			}
			_, _ = io.WriteString(w, name+":"+p.Instance)
		})
	}
	var logs bytes.Buffer
	return New(res, Servers{auth.CapBot: stub("bot-server"), auth.CapWebhook: stub("webhook-server")}, audit.New(&logs)), &logs
}

func do(h http.Handler, method, path string, headers map[string]string) (int, string) {
	r := httptest.NewRequest(method, path, strings.NewReader("{}"))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

func TestHealthz(t *testing.T) {
	h, _ := setup(t)
	if code, body := do(h, "GET", HealthPath, nil); code != 200 || body != "ok\n" {
		t.Fatalf("GET healthz = %d %q", code, body)
	}
	if code, _ := do(h, "POST", HealthPath, nil); code != 405 {
		t.Fatalf("POST healthz = %d", code)
	}
}

func TestRejectionsAreIdentical(t *testing.T) {
	h, logs := setup(t)
	codeA, bodyA := do(h, "POST", MCPPath, nil)
	codeB, bodyB := do(h, "POST", MCPPath, map[string]string{"Authorization": "Bearer wrong"})
	codeC, bodyC := do(h, "POST", "/elsewhere", map[string]string{"X-API-Key": "wrong"})
	if codeA != 401 || codeB != 401 || codeC != 401 || bodyA != bodyB || bodyB != bodyC {
		t.Fatalf("rejections differ: %d %q / %d %q / %d %q", codeA, bodyA, codeB, bodyB, codeC, bodyC)
	}
	for _, want := range []string{`"reason":"missing_credential"`, `"reason":"unknown_token","header":"Authorization"`, `"header":"X-API-Key"`} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("logs missing %s:\n%s", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), "wrong") {
		t.Fatal("rejected credential value was logged")
	}
}

func TestRoutesByCapability(t *testing.T) {
	h, _ := setup(t)
	if code, body := do(h, "POST", MCPPath, map[string]string{"Authorization": "Bearer " + tokBot}); code != 200 || body != "bot-server:bot" {
		t.Fatalf("bot = %d %q", code, body)
	}
	if code, body := do(h, "POST", MCPPath, map[string]string{"X-API-Key": tokHook}); code != 200 || body != "webhook-server:hook" {
		t.Fatalf("hook = %d %q", code, body)
	}
	if code, _ := do(h, "POST", "/other", map[string]string{"Authorization": "Bearer " + tokBot}); code != 404 {
		t.Fatalf("unknown path = %d", code)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/router/`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Implement the router**

`internal/router/router.go`:

```go
// Package router is the front door. A request is authenticated before it is
// routed: the reply to a missing or unknown credential is the same 401
// whatever path was asked for, so it reveals nothing about the server. An
// authenticated request reaches the MCP server for its principal's capability
// set with the principal in its context.
package router

import (
	"encoding/json"
	"net/http"

	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/auth"
)

// Paths served. Everything else is 404 once authenticated.
const (
	HealthPath = "/healthz"
	MCPPath    = "/mcp"
)

// Servers maps each capability set to its MCP handler.
type Servers map[auth.Capability]http.Handler

// Router serves /healthz and /mcp.
type Router struct {
	res     *auth.Resolver
	servers Servers
	log     *audit.Logger
}

// New builds the router.
func New(res *auth.Resolver, servers Servers, log *audit.Logger) *Router {
	return &Router{res: res, servers: servers, log: log}
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == HealthPath {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeJSON(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
		return
	}

	cred, header, ok := auth.Credential(r)
	if !ok {
		rt.log.AuthRejected(string(auth.RejectMissing), "")
		writeJSON(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	p, rej := rt.res.Resolve(r.Context(), cred)
	if rej != "" {
		rt.log.AuthRejected(string(rej), header)
		writeJSON(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	h, ok := rt.servers[p.Capability]
	if r.URL.Path != MCPPath || !ok {
		writeJSON(w, http.StatusNotFound, "unknown endpoint")
		return
	}
	h.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
}

func writeJSON(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": message})
}
```

- [ ] **Step 4: Run the router tests**

Run: `go test ./internal/router/`
Expected: PASS.

- [ ] **Step 5: Add MCP instructions per capability**

Append to `internal/tools/sets.go` (and add the import `"github.com/Hellhium/discord-mcp/internal/auth"`):

```go
const baseInstructions = "You act as a Discord bot through this server. Discord's own permissions decide what succeeds, " +
	"and every call is recorded in an audit log. Pass Discord IDs as strings. " +
	"Start with discord_list_guilds and discord_list_channels. " +
	"Use discord_request for Discord API endpoints that have no dedicated tool."

// Instructions returns the MCP handshake instructions for a capability set.
// They are fixed text: no instance name, token, webhook or other config value.
func Instructions(c auth.Capability) string {
	switch c {
	case auth.CapBotEvents:
		return baseInstructions + " Live Discord events are buffered: use discord_wait_for_message to wait for replies " +
			"and discord_poll_events to catch up, passing back the next_cursor each call returns."
	case auth.CapWebhook:
		return "You act as one Discord webhook through this server: you can post messages as it and read, edit or delete " +
			"the messages it sent. Every call is recorded in an audit log. Pass Discord IDs as strings."
	default:
		return baseInstructions
	}
}
```

- [ ] **Step 6: Implement the application wiring**

`internal/app/app.go`:

```go
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
```

- [ ] **Step 7: Write the end-to-end tests**

`internal/e2e/e2e_test.go`:

```go
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
		name    string
		token   string
		count   int
		has     []string
		hasNot  []string
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
```

- [ ] **Step 8: Run everything**

Run: `go mod tidy && go vet ./... && go test -race ./...`
Expected: PASS for every package. If the tool counts differ, list the names printed by `TestToolListsPerCapability` and compare with Tasks 10–16: discovery 5 + messages 10 + channels 5 + members 10 + `discord_request` = 31; plus 2 event tools = 33; webhook 5.

- [ ] **Step 9: Commit**

```bash
git add go.mod go.sum internal/router internal/app internal/tools/sets.go internal/e2e
git commit -m "feat(app): wire router, resolver and mcp servers per capability" -m "The router serves /healthz without authentication and authenticates
every other request first, answering an identical 401 for any missing,
unknown or Discord-rejected credential and logging the reason. An
authenticated request reaches one of three stateless MCP servers built
at startup (bot, bot with events, webhook) with its principal in the
request context, so tools/list shows exactly what the caller can use.

app.Build creates a principal per instance, verifies config credentials
against Discord and opens Gateways unless verification is turned off,
and closes whatever it started on failure. Handshake instructions are
fixed text per capability set.

End-to-end tests run the whole stack against the fake Discord: tool
lists per capability, each Discord request carrying the caller's own
credential, direct bot tokens, and no credential in logs, errors or
instructions."
```

---
### Task 18: Entrypoint, example config and README

**Files:**
- Create: `cmd/server/main.go`
- Create: `config.example.yaml`
- Create: `internal/config/example_test.go`
- Create: `README.md`

**Interfaces:**
- Consumes: `config.Load` (Task 2), `audit.New` (Task 5), `app.Build`, `app.Options`, `(*App).Handler`, `(*App).Summary`, `(*App).Close` (Task 17).
- Produces: binary flags `-config <path>` (required) and `-verify-credentials` (default `true`); `main.version` set with `-ldflags "-X main.version=..."`.

- [ ] **Step 1: Write the example config and its test**

`config.example.yaml`:

```yaml
# discord-mcp example configuration.
# Treat the real file as a secret (chmod 600) and keep it out of git and images.

server:
  listen: ":8080"

# Accept a Discord bot token or webhook directly in place of a token below.
direct_auth:
  enabled: false
  cache_ttl: 10m

discord:
  timeout: 15s

instances:
  # A bot with live events. Generate client tokens with:
  #   openssl rand -base64 32 | tr -d '=+/'
  - name: assistant
    auth:
      tokens: ["REPLACE-WITH-A-GENERATED-TOKEN-1"]
    bot:
      token: "REPLACE.WITH.YOUR-DISCORD-BOT-TOKEN"
      events:
        # message_content is privileged: enable it in the Developer Portal.
        intents: [guilds, guild_messages, guild_message_reactions, direct_messages, message_content]
        buffer_size: 1000

  # A webhook: its callers only get webhook tools.
  - name: alerts
    auth:
      tokens: ["REPLACE-WITH-A-GENERATED-TOKEN-2"]
    webhook:
      url: "https://discord.com/api/webhooks/123456789012345678/REPLACE_WITH_WEBHOOK_TOKEN"
```

`internal/config/example_test.go`:

```go
package config

import "testing"

// The shipped example must stay valid: the container smoke test starts the
// server with it.
func TestExampleConfigIsValid(t *testing.T) {
	c, err := Load("../../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Instances) != 2 || c.Instances[0].Bot == nil || c.Instances[1].Webhook == nil {
		t.Fatalf("unexpected example: %+v", c.Instances)
	}
}
```

Run: `go test ./internal/config/ -run Example`
Expected: PASS.

- [ ] **Step 2: Implement main**

`cmd/server/main.go`:

```go
// Command server is the discord-mcp entrypoint: it loads the YAML config,
// builds the application and serves /healthz and /mcp on one listener until
// SIGINT or SIGTERM.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Hellhium/discord-mcp/internal/app"
	"github.com/Hellhium/discord-mcp/internal/audit"
	"github.com/Hellhium/discord-mcp/internal/config"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	configPath := flag.String("config", "", "path to the YAML config file (required)")
	verify := flag.Bool("verify-credentials", true, "check config credentials against Discord and open Gateways at startup")
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "missing required -config flag")
		os.Exit(2)
	}
	if err := run(*configPath, *verify); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string, verify bool) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	log := audit.New(os.Stdout)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a, err := app.Build(ctx, cfg, app.Options{Version: version, VerifyCredentials: verify, Log: log})
	if err != nil {
		return err
	}
	defer a.Close()

	srv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           a.Handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Info("discord-mcp listening", "version", version, "listen", cfg.Server.Listen, "verify_credentials", verify)
	for _, line := range a.Summary() {
		log.Info(line)
	}

	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		// Wake wait_for_message calls first so Shutdown can drain them.
		a.Close()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	}
}
```

- [ ] **Step 3: Build and smoke-test locally**

```bash
make build
./discord-mcp; echo "exit=$?"            # expect: missing required -config flag, exit=2
./discord-mcp -config config.example.yaml -verify-credentials=false &
sleep 1
curl -s http://localhost:8080/healthz    # expect: ok
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:8080/mcp   # expect: 401
curl -s -X POST http://localhost:8080/mcp \
  -H 'Authorization: Bearer REPLACE-WITH-A-GENERATED-TOKEN-2' \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"curl","version":"1"}}}' | head -c 400; echo
kill %1
```

Expected: `ok`, `401`, and a JSON-RPC `initialize` result naming server `discord-mcp` whose instructions describe the webhook mode. The stdout of the server shows JSON lines: `discord-mcp listening`, one line per instance, `direct auth: false`, and an `auth_rejected` line for the 401.

- [ ] **Step 4: Write the README**

`README.md`:

````markdown
# discord-mcp

An [MCP](https://modelcontextprotocol.io) server that lets an LLM drive Discord
through a bot account or a webhook.

- **Discord is the permission system.** The server adds no filtering of its own:
  whatever the bot's roles (or the webhook) allow, the LLM can do.
- **Every action is audited.** Each tool call writes one JSON line to stdout —
  who, which tool, which IDs, the arguments, the Discord calls made and the
  outcome. Credentials never appear in it.
- **Full API reach.** About thirty typed tools cover assistant work (messages,
  threads, channels, members, roles, moderation, search, DMs), and
  `discord_request` reaches every other REST endpoint.
- **Two modes.** Bot callers get the bot tools (plus live events for bots
  configured with `events`); webhook callers only get webhook tools.

## Getting started

You need a Discord bot token (Developer Portal → your application → Bot) or a
webhook URL (channel settings → Integrations → Webhooks), and either Go 1.26+
or Docker.

### 1. Write a config

Start from [`config.example.yaml`](config.example.yaml). The minimum:

```yaml
instances:
  - name: assistant
    auth:
      tokens: ["PASTE-A-GENERATED-TOKEN-HERE"]
    bot:
      token: "YOUR-DISCORD-BOT-TOKEN"
```

Each instance maps the tokens your MCP clients present to exactly one `bot` or
one `webhook`. Generate client tokens yourself:

```bash
openssl rand -base64 32 | tr -d '=+/'
```

Treat the config as a secret (`chmod 600`) and keep it out of git and images.

To receive live events, add `events` to a bot:

```yaml
    bot:
      token: "YOUR-DISCORD-BOT-TOKEN"
      events:
        intents: [guilds, guild_messages, direct_messages, message_content]
```

Privileged intents (`message_content`, `guild_members`, `guild_presences`) must
be enabled in the Developer Portal; startup fails naming any that are not.

### 2. Run it

```bash
make build
./discord-mcp -config config.yaml
```

or with Docker:

```bash
docker run --rm -p 8080:8080 \
  -v "$PWD/config.yaml:/etc/discord-mcp/config.yaml:ro" \
  ghcr.io/hellhium/discord-mcp:latest
```

At startup every bot token and webhook in the config is checked against
Discord, and bots with events connect to the Gateway; any failure stops the
server with a message naming the instance. `-verify-credentials=false` skips
this (event buffers then stay empty).

### 3. Check it is up

```bash
curl -s http://localhost:8080/healthz     # ok
```

### 4. Connect an MCP client

MCP is served over Streamable HTTP at `POST /mcp`. Send the token in
`Authorization: Bearer …`, or in `X-API-Key: …` for clients that cannot set an
Authorization header.

**Claude Code:**

```bash
claude mcp add --transport http discord http://localhost:8080/mcp \
  --header "Authorization: Bearer $TOKEN"
```

**JSON-configured clients** (`.mcp.json`, Cursor, VS Code …):

```json
{
  "mcpServers": {
    "discord": {
      "type": "http",
      "url": "http://localhost:8080/mcp",
      "headers": { "Authorization": "Bearer PASTE-A-GENERATED-TOKEN-HERE" }
    }
  }
}
```

**stdio-only clients** need a bridge such as
`npx -y mcp-remote http://localhost:8080/mcp --header "Authorization: Bearer …"`.

## Direct authentication

With `direct_auth.enabled: true`, a client may send a Discord credential in
place of a config token, in the same headers:

- a bot token → bot tools (no events);
- a webhook URL, or `<webhook id>/<webhook token>` → webhook tools.

Config tokens are always matched first. Anything else that is not shaped like a
Discord credential is refused without contacting Discord. A credential Discord
accepts is cached for `cache_ttl` since last use; one Discord rejects is refused
for a minute without asking again, so a retrying client cannot get the server's
IP banned by Discord.

## Tools

| Group | Tools |
|---|---|
| Discovery | `discord_get_me`, `discord_list_guilds`, `discord_get_guild`, `discord_list_channels`, `discord_get_channel` |
| Messages | `discord_read_messages`, `discord_get_message`, `discord_send_message`, `discord_edit_message`, `discord_delete_message`, `discord_pin_message`, `discord_add_reaction`, `discord_remove_reaction`, `discord_search_messages`, `discord_send_dm` |
| Threads & channels | `discord_create_thread`, `discord_list_active_threads`, `discord_create_channel`, `discord_edit_channel`, `discord_delete_channel` |
| Members & roles | `discord_list_members`, `discord_get_member`, `discord_search_members`, `discord_list_roles`, `discord_add_role`, `discord_remove_role`, `discord_timeout_member`, `discord_kick_member`, `discord_ban_member`, `discord_unban_member` |
| Everything else | `discord_request` — any REST endpoint, relative to `https://discord.com/api/v10` |
| Events (bots with `events`) | `discord_poll_events`, `discord_wait_for_message` |
| Webhook callers | `discord_webhook_get`, `discord_webhook_send`, `discord_webhook_get_message`, `discord_webhook_edit_message`, `discord_webhook_delete_message` |

Read tools accept `format: json` to get Discord's objects as-is. Tools that
change something accept `reason`, recorded in the server's Discord audit log.
Message tools accept `allow_mentions: false` to stop a message from pinging
anyone. Attachments are base64; the server never fetches URLs.

## Audit log

One JSON line per tool call on stdout:

```json
{"time":"2026-09-14T11:02:00Z","level":"INFO","msg":"action",
 "principal":{"kind":"instance","instance":"assistant"},
 "tool":"discord_send_message","outcome":"ok","duration_ms":142,
 "targets":{"channel_id":"123"},"args":{"channel_id":"123","content":"hello"},
 "discord":[{"method":"POST","route":"/channels/{channel_id}/messages","status":200,"rate_limited_ms":0}]}
```

`outcome` is `ok`, `invalid_args`, `discord_error`, `rate_limited` or
`credential_rejected`. Direct credentials appear as `"credential":"sha256:…"`.
Rejected requests log `auth_rejected` with the reason; Gateway connects,
resumes and reconnects are logged too.

## Operating notes

- **Events live in memory.** A restart empties the buffer; a cursor from before
  it is reported as a gap.
- **Run one replica per instance with events.** Two replicas would each hold
  their own Gateway and buffer. Instances without events are stateless.
- **Rate limits** are handled per credential; a call that would wait longer
  than `discord.timeout` fails with `retry_after`.
````

- [ ] **Step 5: Run the whole suite**

Run: `make all`
Expected: `fmt-check`, `vet`, `test` and `build` all succeed.

- [ ] **Step 6: (Optional, manual) try a real bot**

With a real bot token in `config.yaml` and `message_content` enabled, run `./discord-mcp -config config.yaml`, connect Claude Code as in step 4, and ask it to list guilds, send a message and wait for a reply. Check stdout shows `gateway_connected` and one `action` line per tool call. This is the only check of `events.Gateway.Start` against Discord itself.

- [ ] **Step 7: Commit**

```bash
git add cmd config.example.yaml internal/config/example_test.go README.md
git commit -m "feat: add server entrypoint, example config and readme" -m "cmd/server loads the config, builds the application and serves /healthz
and /mcp until SIGINT or SIGTERM. Shutdown closes event buffers first so
pending wait_for_message calls return and the HTTP server can drain.
-verify-credentials=false starts without contacting Discord, for offline
smoke tests.

config.example.yaml shows a bot with events and a webhook and is kept
valid by a test. The README covers configuration, running, connecting MCP
clients with Bearer or X-API-Key, direct authentication, the tool list,
the audit log format and operating limits."
```

---

### Task 19: Docker image and CI

**Files:**
- Create: `Dockerfile`, `.dockerignore`, `.github/workflows/docker.yml`

**Interfaces:**
- Consumes: the binary and flags from Task 18; `config.example.yaml`.
- Produces: image `ghcr.io/<owner>/discord-mcp`, entrypoint `/usr/local/bin/discord-mcp`, default command `-config /etc/discord-mcp/config.yaml`.

- [ ] **Step 1: Write the Dockerfile**

`Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1

# Build: a static binary cross-compiled on the native builder platform.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /src

# Dependencies first: this layer survives every source-only change.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/discord-mcp ./cmd/server

# Runtime: alpine, for a shell to exec into, wget for the HEALTHCHECK and
# ca-certificates for https://discord.com.
FROM alpine:3.24

RUN apk add --no-cache ca-certificates \
    && adduser -D -H -u 65532 nonroot

COPY --from=build /out/discord-mcp /usr/local/bin/discord-mcp

# Numeric, never the name: Kubernetes runAsNonRoot cannot verify a named user.
USER 65532:65532

# Mount the config read-only:
#   docker run -v ./config.yaml:/etc/discord-mcp/config.yaml:ro ...
EXPOSE 8080

# Assumes the default listen address; drop it if server.listen moves off :8080.
HEALTHCHECK --interval=30s --timeout=3s --start-period=40s \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/discord-mcp"]
CMD ["-config", "/etc/discord-mcp/config.yaml"]
```

(`--start-period=40s` leaves room for the 30s Gateway ready timeout.)

`.dockerignore`:

```
# Everything the build does not need. Keeping this tight also keeps secrets
# out of the build context: a real config.yaml never belongs in an image.
.git
.github
.claude
docs
references
*.yaml
!config.example.yaml
discord-mcp
Dockerfile
.dockerignore
README.md
CLAUDE.md
Makefile
```

- [ ] **Step 2: Build and smoke-test the image locally**

```bash
docker build -t discord-mcp:dev .
docker run --rm discord-mcp:dev -config /nope.yaml; echo "exit=$?"     # expect non-zero
docker run -d --name dm -p 8080:8080 \
  -v "$PWD/config.example.yaml:/etc/discord-mcp/config.yaml:ro" \
  discord-mcp:dev -config /etc/discord-mcp/config.yaml -verify-credentials=false
sleep 2 && curl -fsS http://localhost:8080/healthz
docker logs dm && docker rm -f dm
```

Expected: `exit=1`, then `ok`, and JSON startup lines in the logs. (Skip this step if Docker is not available locally; CI runs the same check.)

- [ ] **Step 3: Write the workflow**

`.github/workflows/docker.yml`:

```yaml
name: docker

on:
  push:
    branches: [main]
    tags: ["v*"]
  pull_request:
  workflow_dispatch:

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ${{ github.repository }}

concurrency:
  group: docker-${{ github.ref }}
  cancel-in-progress: true

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: gofmt
        run: test -z "$(gofmt -l .)"
      - name: vet
        run: go vet ./...
      - name: test
        run: go test -race ./...

  build:
    needs: test
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
      id-token: write
      attestations: write
    steps:
      - name: Checkout
        uses: actions/checkout@v5

      # Pull requests build amd64 only and are never pushed.
      - name: Set build parameters
        id: params
        run: |
          if [ "${{ github.event_name }}" = "pull_request" ]; then
            echo "platforms=linux/amd64" >> "$GITHUB_OUTPUT"
            echo "push=false" >> "$GITHUB_OUTPUT"
          else
            echo "platforms=linux/amd64,linux/arm64" >> "$GITHUB_OUTPUT"
            echo "push=true" >> "$GITHUB_OUTPUT"
          fi

      - name: Set up QEMU
        if: steps.params.outputs.push == 'true'
        uses: docker/setup-qemu-action@v3

      - name: Set up Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to ${{ env.REGISTRY }}
        if: steps.params.outputs.push == 'true'
        uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Derive tags and labels
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          tags: |
            type=ref,event=branch
            type=ref,event=pr
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
            type=semver,pattern={{major}},enable=${{ !startsWith(github.ref, 'refs/tags/v0.') }}
            type=raw,value=latest,enable={{is_default_branch}}
            type=sha,format=long
          labels: |
            org.opencontainers.image.title=discord-mcp
            org.opencontainers.image.description=MCP server that drives Discord through a bot or webhook and audits every action

      - name: Build and push
        id: build
        uses: docker/build-push-action@v6
        with:
          context: .
          platforms: ${{ steps.params.outputs.platforms }}
          push: ${{ steps.params.outputs.push }}
          load: ${{ steps.params.outputs.push == 'false' }}
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          build-args: |
            VERSION=${{ steps.meta.outputs.version }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
          provenance: ${{ steps.params.outputs.push }}
          sbom: ${{ steps.params.outputs.push }}

      # A bad config must fail loudly; the example must start offline and
      # answer /healthz.
      - name: Smoke test the image
        if: steps.params.outputs.push == 'false'
        run: |
          set -euo pipefail
          image="$(echo '${{ steps.meta.outputs.tags }}' | head -n1)"

          if docker run --rm "$image" -config /nope.yaml; then
            echo "expected a failure with a missing config"; exit 1
          fi

          docker run -d --name dm -p 8080:8080 \
            -v "$PWD/config.example.yaml:/etc/discord-mcp/config.yaml:ro" \
            "$image" -config /etc/discord-mcp/config.yaml -verify-credentials=false
          for _ in $(seq 1 30); do
            if curl -fsS http://localhost:8080/healthz; then ok=1; break; fi
            sleep 1
          done
          docker logs dm
          docker rm -f dm
          test "${ok:-0}" = 1

      - name: Attest build provenance
        if: steps.params.outputs.push == 'true'
        uses: actions/attest-build-provenance@v2
        with:
          subject-name: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          subject-digest: ${{ steps.build.outputs.digest }}
          push-to-registry: true
```

- [ ] **Step 4: Validate the workflow syntax**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/docker.yml'))" && echo ok`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add Dockerfile .dockerignore .github/workflows/docker.yml
git commit -m "ci: build and publish the container image" -m "Add a multi-stage Dockerfile producing a static binary on Alpine, run as
UID 65532 with a /healthz HEALTHCHECK and the config mounted read-only.
The workflow runs gofmt, vet and race tests, then builds multi-arch
images pushed to GHCR from main and tags. Pull requests build amd64 only
and smoke-test the image: a missing config must fail and the example
config must start offline and answer /healthz."
```

---

## Spec coverage

| Spec section | Implemented in |
|---|---|
| §1 Goal: Discord permissions only, audit every action, two modes, two auth paths | Tasks 8, 9, 17 |
| §2 Architecture: packages, request flow, three capability MCP servers, principal from context | Tasks 8, 9, 17 |
| §3 Configuration, defaults, validation, `-verify-credentials` | Tasks 1, 2, 17, 18 |
| §3 Startup Discord checks, privileged intents named, Gateway failure | Tasks 4, 7, 17 |
| §4 Credential extraction order and shape detection | Tasks 1, 8 |
| §4 Success cache, failure cache (1 min), revocation eviction | Tasks 8, 9 |
| §4 Identical 401, `auth_rejected` line, no credential forwarded | Tasks 5, 17 |
| §5 Shared behaviour: format, reason, annotations, allow_mentions, base64 attachments, embeds | Task 9 |
| §5 Bot tools | Tasks 10–13 (+ `discord_search_messages`, see amendment) |
| §5 `discord_request` guard, fixed host, JSON only, 64 KB truncation | Tasks 4, 14 |
| §5 Event tools | Task 15 |
| §5 Webhook tools | Task 16 |
| §5 Instructions without config values | Task 17 |
| §6 Event buffer: lifecycle filtering, ring, boot-ID cursors, reconnect marker, waiters | Tasks 6, 7 |
| §7 Audit log fields, outcomes, levels, sanitising, Gateway lifecycle lines | Tasks 3, 5, 7, 9 |
| §8 Errors and rate limits; graceful shutdown | Tasks 3, 9, 18 |
| §9 Testing | Every task; end to end in Task 17 |
| §10 Packaging: Makefile, Dockerfile, GHCR workflow, example config, README | Tasks 1, 18, 19 |
