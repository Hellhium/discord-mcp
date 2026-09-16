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
		name   string
		status int
		body   any
		check  func(t *testing.T, resp Response, err error)
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
